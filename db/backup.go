package db

import (
	bolt "go.etcd.io/bbolt"
)

var (
	markIDKey = []byte("lastBackupMarkID")
)

// AddBackupRequest is used to mark a need for a backup before actually executing it.
// This will allow to recover in case of backup failure.
func (db *DB) AddBackupRequest() (err error) {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(backupBucket))
		backupSeq, err := b.NextSequence()
		if err != nil {
			return err
		}
		return b.Put(markIDKey, itob(backupSeq))
	})
}

// LastBackupRequest returns the pending backup id if exists, zero if doesn't.
func (db *DB) LastBackupRequest() (uint64, error) {
	pendingRequest, err := db.fetchItem([]byte(backupBucket), markIDKey)
	if err != nil || pendingRequest == nil {
		return 0, err
	}
	return btoi(pendingRequest), nil
}

// MarkBackupRequestCompleted is used to signal that the backup has completed and that the
// corresponding requestID can be removed.
func (db *DB) MarkBackupRequestCompleted(requestID uint64) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(backupBucket))
		pendingRequest := b.Get(markIDKey)
		if pendingRequest == nil || requestID != btoi(pendingRequest) {
			return nil
		}
		return b.Delete(markIDKey)
	})
}
