package db

import (
	"encoding/binary"
	"os"
	"path"
	"path/filepath"

	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/refcount"
	"github.com/btcsuite/btclog"
	bolt "go.etcd.io/bbolt"
)

const (
	versionBucket        = "version"
	incomingPayReqBucket = "paymentRequests"

	//add funds
	addressesBucket           = "subswap_addresses"
	swapAddressesByHashBucket = "subswap_addresses_by_hash"

	//remove funds
	redeemableHashesBucket = "redeemableHashes"

	//payments and account
	paymentsBucket         = "payments"
	paymentsHashBucket     = "paymentsByHash"
	paymentsSyncInfoBucket = "paymentsSyncInfo"
	accountBucket          = "account"

	//encrypted sessions
	encryptedSessionsBucket = "encrypted_sessions"

	//Network configuration
	networkBucket = "network"

	//syncstatus
	syncstatus = "syncstatus"
)

var (
	serviceRefCounter refcount.ReferenceCountable
)

// DB is the structure for breez database
type DB struct {
	*bolt.DB
	dbPath string
	log    btclog.Logger
}

// Get returns a Ch
func Get(workingDir string) (db *DB, cleanupFn func() error, err error) {
	service, release, err := serviceRefCounter.Get(
		func() (interface{}, refcount.ReleaseFunc, error) {
			return newDB(workingDir)
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return service.(*DB), release, err
}

func newDB(workingDir string) (*DB, refcount.ReleaseFunc, error) {
	logBackend, err := breezlog.GetLogBackend(workingDir)
	if err != nil {
		return nil, nil, err
	}
	log := logBackend.Logger("BRDB")

	db, err := openDB(path.Join(workingDir, "breez.db"), log)
	if err != nil {
		return nil, nil, err
	}

	return db, db.closeDB, err
}

// OpenDB opens the database and makes it ready to work
func openDB(dbPath string, log btclog.Logger) (*DB, error) {
	var err error
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Criticalf("Failed to open database %v", err)
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		var err error
		_, err = tx.CreateBucketIfNotExists([]byte(incomingPayReqBucket))
		if err != nil {
			return err
		}
		paymenetBucket, err := tx.CreateBucketIfNotExists([]byte(paymentsBucket))
		if err != nil {
			return err
		}
		_, err = paymenetBucket.CreateBucketIfNotExists([]byte(paymentsSyncInfoBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(accountBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(addressesBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(swapAddressesByHashBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(versionBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(redeemableHashesBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(paymentsHashBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(encryptedSessionsBucket))
		if err != nil {
			return err
		}

		_, err = tx.CreateBucketIfNotExists([]byte(networkBucket))
		if err != nil {
			return err
		}

		_, err = tx.CreateBucketIfNotExists([]byte(syncstatus))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	breezDB := &DB{
		DB:     db,
		dbPath: dbPath,
		log:    log,
	}

	// remove invalidated btcd on upgrade.
	peers, _, err := breezDB.GetPeers(nil)
	if err != nil {
		return nil, err
	}
	if len(peers) == 1 && peers[0] == "bb1.breez.technology" {
		if err = breezDB.SetPeers(nil); err != nil {
			return nil, err
		}
	}
	return breezDB, nil
}

// CloseDB closed the db
func (db *DB) closeDB() error {
	return db.Close()
}

// DeleteDB deletes the database, mainly for testing
func (db *DB) DeleteDB() error {
	return os.Remove(db.Path())
}

func (db *DB) saveItem(bucket []byte, key []byte, value []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Put(key, value)
	})
}

func (db *DB) deleteItem(bucket []byte, key []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Delete(key)
	})
}

func (db *DB) fetchItem(bucket []byte, key []byte) ([]byte, error) {
	var value []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		value = b.Get(key)
		return nil
	})
	return value, err
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func btoi(bytes []byte) uint64 {
	return binary.BigEndian.Uint64(bytes)
}

func (db *DB) BackupDb(dir string) (string, error) {
	dbCopy := filepath.Join(dir, path.Base(db.Path()))
	f1, err := os.Create(dbCopy)
	if err != nil {
		return "", err
	}
	defer f1.Close()
	err = db.View(func(tx *bolt.Tx) error {
		_, err := tx.WriteTo(f1)
		return err
	})
	if err != nil {
		return "", err
	}
	return dbCopy, nil
}
