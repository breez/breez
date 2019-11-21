package chainservice

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path"
	"sync"
	"time"

	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino/headerfs"
)

var (
	bootstrapMu       sync.Mutex
	waddrmgrNamespace = []byte("waddrmgr")
	syncBucketName    = []byte("sync")
	birthdayBlockName = []byte("birthday")
)

// ResetChainService deletes neutrino headers/cfheaders and the db so
// the process of synching will start from the beginning.
// It allows the node to recover in case some filters/headers were
// skipped due to unexpected error.
func ResetChainService(workingDir string) error {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return err
	}
	neutrinoDataDir := neutrinoDataDir(workingDir, config.Network)
	if err = os.Remove(path.Join(neutrinoDataDir, "neutrino.db")); err != nil {
		return err
	}
	if err = os.Remove(path.Join(neutrinoDataDir, "reg_filter_headers.bin")); err != nil {
		return err
	}
	if err = os.Remove(path.Join(neutrinoDataDir, "block_headers.bin")); err != nil {
		return err
	}

	return nil
}

// Bootstrapped returns true if bootstrap was done, fals otherwise.
func Bootstrapped(workingDir string) (bool, error) {
	tipHeight, err := chainTipHeight(workingDir)
	if err != nil {
		return false, err
	}
	birthday, err := walletBirthday(workingDir)
	if err != nil {
		return false, err
	}
	logBackend, err := log.GetLogBackend(workingDir)
	if err != nil {
		return false, err
	}
	logger = logBackend.Logger("CHAIN")
	logger.Infof("using birthday %v for bootstrap", birthday)
	lastCheckpiont := getLatestCheckpoint(*birthday)
	return tipHeight >= lastCheckpiont.Height, nil
}

// Bootstrap is populating neutrino data (flat files and db) with predefined
// checkpoints to make the sync process a lot faster.
// Instad of synching from the genesis block, it is now done down to a checkpoint
// determined by the wallet birthday.
func Bootstrap(workingDir string) error {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()

	logBackend, err := log.GetLogBackend(workingDir)
	if err != nil {
		return err
	}
	logger = logBackend.Logger("CHAIN")

	if service != nil {
		return errors.New("Chain service already created, can't bootstrap")
	}

	bootstrapped, err := Bootstrapped(workingDir)
	if err != nil {
		logger.Errorf("Bootstrapped returned error: %v", err)
		return err
	}
	if bootstrapped {
		return nil
	}

	logger.Info("staring bootstrap flow")
	//create temporary neturino db.
	neutrinoDataDir, db, err := getNeutrinoDB(workingDir)
	if err != nil {
		return err
	}
	defer db.Close()

	birthday, err := walletBirthday(workingDir)
	if err != nil {
		return err
	}
	logger.Infof("bootstrapping using birthday: %v", birthday)

	tipCheckpoint := getLatestCheckpoint(*birthday)
	logger.Infof("bootstrapping using checkpoint height: %v", tipCheckpoint.Height)

	// Now that we have the latest checkpoint that was mined before the wallet
	// birthday we need to ensure neutrino is pupulated with that.
	if err = ensureMinimumTip(db, neutrinoDataDir,
		&headerfs.BlockHeader{
			BlockHeader: tipCheckpoint.BlockHeader,
			Height:      tipCheckpoint.Height},
		tipCheckpoint.FilterHeader, logger); err != nil {
		return err
	}

	return nil
}

func getNeutrinoDB(workingDir string) (string, walletdb.DB, error) {
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return "", nil, err
	}
	neutrinoDataDir := neutrinoDataDir(workingDir, config.Network)
	neutrinoDB := path.Join(neutrinoDataDir, "neutrino.db")
	if err := os.MkdirAll(neutrinoDataDir, 0700); err != nil {
		return "", nil, err
	}

	db, err := walletdb.Create("bdb", neutrinoDB, false)
	return neutrinoDataDir, db, err
}

// getLatestCheckpoint returns the latest checkpoint that is mined before the
// walletBirthday date.
func getLatestCheckpoint(walletBirthday time.Time) Checkpoint {
	var latestCheckpoint Checkpoint
	for _, ck := range checkpoints {
		if ck.BlockHeader.Timestamp.After(walletBirthday) {
			break
		}
		latestCheckpoint = ck
	}

	return latestCheckpoint
}

// ensureMinimumTip ensures neutrino is initialized with (at least) 'startHeader' as the tip.
// using this function allows us to use a pre-defined checkpoint for neutrino as earliest point
// for syncing, making the bootstrap and sync a lot faster.
func ensureMinimumTip(db walletdb.DB, bootstrapDir string, startHeader *headerfs.BlockHeader,
	filterHash *chainhash.Hash, logger btclog.Logger) error {

	headersPath := path.Join(bootstrapDir, "block_headers.bin")
	headersFile, err := os.OpenFile(headersPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer headersFile.Close()

	filterHeadersPath := path.Join(bootstrapDir, "reg_filter_headers.bin")
	filterHeadersFile, err := os.OpenFile(filterHeadersPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer headersFile.Close()

	if err := headersFile.Truncate(int64(startHeader.Height * headerfs.BlockHeaderSize)); err != nil {
		return err
	}
	if err := filterHeadersFile.Truncate(int64(startHeader.Height * headerfs.RegularFilterHeaderSize)); err != nil {
		return err
	}

	// populating all the predefined checpoints
	for i, ck := range checkpoints {
		height := uint32(i * wire.CFCheckptInterval)
		if _, err := headersFile.Seek(int64(height*headerfs.BlockHeaderSize), 0); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err = ck.BlockHeader.Serialize(&buf); err != nil {
			return err
		}
		if _, err := headersFile.Write(buf.Bytes()); err != nil {
			return err
		}

		if _, err := filterHeadersFile.Seek(int64(height*headerfs.RegularFilterHeaderSize), 0); err != nil {
			return err
		}

		if _, err := filterHeadersFile.Write(ck.FilterHeader[:]); err != nil {
			return err
		}
	}

	return updateDBTip(db, startHeader.Height, startHeader.BlockHash())
}

func updateDBTip(db walletdb.DB, height uint32, hash chainhash.Hash) error {
	return walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		rootBucket := tx.ReadWriteBucket([]byte("header-index"))
		var heightBytes [4]byte
		binary.BigEndian.PutUint32(heightBytes[:], height)
		err := rootBucket.Put(hash[:], heightBytes[:])
		if err != nil {
			return err
		}

		if err = rootBucket.Put([]byte("bitcoin"), hash[:]); err != nil {
			return err
		}
		return rootBucket.Put([]byte("regular"), hash[:])
	})
}

// chainTipHeight returns the current headers tip.
func chainTipHeight(workingDir string) (uint32, error) {
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return 0, err
	}
	params, err := chainParams(config.Network)
	if err != nil {
		return 0, err
	}
	neutrinoDataDir, db, err := getNeutrinoDB(workingDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	headersStore, err := headerfs.NewBlockHeaderStore(neutrinoDataDir, db, params)
	if err != nil {
		return 0, err
	}
	_, height, err := headersStore.ChainTip()
	if err != nil {
		return 0, err
	}
	return height, nil
}

// walletBirthday finds the wallet birthday date. If the wallet already exists
// it query it, otherwise it just return a date two days ago.
func walletBirthday(workingDir string) (*time.Time, error) {
	noWalletBirthday := time.Now().Add(time.Hour * 48 * -1)
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}
	walletDBPath := path.Join(workingDir, "data/chain/bitcoin/", config.Network, "wallet.db")
	_, err = os.Stat(walletDBPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &noWalletBirthday, nil
		}
		return nil, err
	}

	db, err := walletdb.Open("bdb", walletDBPath, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	birthday := noWalletBirthday
	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrNamespace)
		if ns != nil {
			syncBucket := ns.NestedReadBucket(syncBucketName)
			if syncBucket != nil {
				birthdayTimestamp := syncBucket.Get(birthdayBlockName)
				if len(birthdayTimestamp) == 8 {
					birthday = time.Unix(int64(binary.BigEndian.Uint64(birthdayTimestamp)), 0)
				}
			}
		}
		return nil
	})
	return &birthday, err
}
