package doubleratchet

import (
	"encoding/binary"
	"fmt"
	"os"

	bolt "go.etcd.io/bbolt"
)

const (
	encryptedSessionsBucket = "encryptedSessions"
	sessionKeysBucket       = "sessionKeys"
)

var db *bolt.DB

func openDB(dbPath string) error {
	var err error
	db, err = bolt.Open(dbPath, 0600, nil)
	if err != nil {
		fmt.Printf("Failed to open database %v", err)
		return err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		var err error
		_, err = tx.CreateBucketIfNotExists([]byte(sessionKeysBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(encryptedSessionsBucket))
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

func closeDB() error {
	return db.Close()
}

func destroyDB() error {
	return os.Remove(db.Path())
}

func saveEncryptedSession(sessionID []byte, sessionData []byte) error {
	return saveItem([]byte(encryptedSessionsBucket), sessionID, sessionData)
}

func fetchEncryptedSession(sessionID []byte) ([]byte, error) {
	return fetchItem([]byte(encryptedSessionsBucket), []byte(sessionID))
}

func saveEncryptedMessageKey(sessionID []byte, pubKey []byte, msgNum uint, msgKey []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		keysBucket, err := tx.CreateBucketIfNotExists([]byte("sessionKeys"))
		if err != nil {
			return err
		}
		pubKeyBucket, err := keysBucket.CreateBucketIfNotExists(pubKey)
		return pubKeyBucket.Put(itob(uint64(msgNum)), msgKey)
	})
}

func fetchEncryptedMessageKey(pubKey []byte, msgNum uint) ([]byte, error) {
	var msgKey []byte
	err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("sessionKeys"))
		if err != nil {
			return err
		}
		pubKeyBucket := bucket.Bucket(pubKey)
		if pubKeyBucket == nil {
			return nil
		}
		msgKey = pubKeyBucket.Get(itob(uint64(msgNum)))
		return nil
	})
	return msgKey, err
}

func countMessageKeys(pubKey []byte) (uint, error) {
	var count uint
	err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("sessionKeys"))
		if err != nil {
			return err
		}
		pubKeyBucket := bucket.Bucket(pubKey)
		if pubKeyBucket == nil {
			return nil
		}
		count = uint(pubKeyBucket.Stats().KeyN)
		return nil
	})
	return count, err
}

func allMessageKeys() (map[[32]byte]map[uint][32]byte, error) {
	var all map[[32]byte]map[uint][32]byte
	err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("sessionKeys"))
		if err != nil {
			return err
		}
		return bucket.ForEach(func(k, v []byte) error {
			if v == nil {
				var key [32]byte
				copy(key[:], k)
				all[key] = make(map[uint][32]byte)
				pubKeyBucket := bucket.Bucket(k)
				pubKeyBucket.ForEach(func(mk, mv []byte) error {
					var msgKey [32]byte
					copy(msgKey[:], k)
					all[key][uint(btoi(mk))] = msgKey
					return nil
				})
			}
			return nil
		})
	})
	return all, err
}

func saveItem(bucket []byte, key []byte, value []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Put(key, value)
	})
}

func fetchItem(bucket []byte, key []byte) ([]byte, error) {
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
