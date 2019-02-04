package db

import (
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	markIDKey = []byte("lastBackupMarkID")
)

// SetPendingBackup is used to mark a need for a backup before actually executing it.
// This will allow to recover in case of backup failure.
func (db *DB) SetPendingBackup() (pendingID uint64, err error) {
	now := uint64(time.Now().Unix())
	return now, db.saveItem([]byte(backupBucket), markIDKey, itob(now))
}

// PendingBackup returns the pending backup id if exists, zero if doesn't.
func (db *DB) PendingBackup() (uint64, error) {
	pending, err := db.fetchItem([]byte(backupBucket), markIDKey)
	if err != nil || pending == nil {
		return 0, err
	}
	return btoi(pending), nil
}

// ComitPendingBackup is used to signal that the backup has completed and that the
// corresponding pendingID can be removed.
func (db *DB) ComitPendingBackup(pendingID uint64) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(backupBucket))
		pending := b.Get(markIDKey)
		if pending == nil || pendingID != btoi(pending) {
			return nil
		}
		return b.Delete(markIDKey)
	})
}
