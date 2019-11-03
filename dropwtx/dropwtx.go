package dropwtx

import (
	"path"

	"github.com/breez/breez/config"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	"github.com/btcsuite/btcwallet/wtxmgr"
)

var (
	waddrmgrNamespace = []byte("waddrmgr")
	wtxmgrNamespace   = []byte("wtxmgr")
)

func Drop(workingDir string) error {
	cfg, err := config.GetConfig(workingDir)
	if err != nil {
		return err
	}
	db, err := walletdb.Open("bdb", path.Join(workingDir, "data/chain/bitcoin/", cfg.Network, "wallet.db"), false)
	if err != nil {
		return err
	}
	defer db.Close()

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		err := tx.DeleteTopLevelBucket(wtxmgrNamespace)
		if err != nil && err != walletdb.ErrBucketNotFound {
			return err
		}
		ns, err := tx.CreateTopLevelBucket(wtxmgrNamespace)
		if err != nil {
			return err
		}
		err = wtxmgr.Create(ns)
		if err != nil {
			return err
		}

		ns = tx.ReadWriteBucket(waddrmgrNamespace)
		birthdayBlock, err := waddrmgr.FetchBirthdayBlock(ns)
		if err != nil {
			startBlock, err := waddrmgr.FetchStartBlock(ns)
			if err != nil {
				return err
			}
			return waddrmgr.PutSyncedTo(ns, startBlock)
		}

		// We'll need to remove our birthday block first because it
		// serves as a barrier when updating our state to detect reorgs
		// due to the wallet not storing all block hashes of the chain.
		if err := waddrmgr.DeleteBirthdayBlock(ns); err != nil {
			return err
		}

		if err := waddrmgr.PutSyncedTo(ns, &birthdayBlock); err != nil {
			return err
		}
		return waddrmgr.PutBirthdayBlock(ns, birthdayBlock)
	})
	if err != nil {
		return err
	}
	return nil
}
