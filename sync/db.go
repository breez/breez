package sync

import (
	"encoding/binary"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	syncInfoBucket        = "syncInfo"
	lastCFilterHeightKey  = "lastCFilterHeight"
	lastSuccessRunDateKey = "lastSuccessRunDate"
)

type jobDB struct {
	db *bolt.DB
}

func openJobDB(path string) (*jobDB, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	return &jobDB{db: db}, nil
}

func (j *jobDB) close() error {
	return j.db.Close()
}

func (j *jobDB) setCFilterSyncHeight(height uint64) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(syncInfoBucket))
		if err != nil {
			return err
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, height)
		return bucket.Put([]byte(lastCFilterHeightKey), b)
	})
}

func (j *jobDB) fetchCFilterSyncHeight() (uint64, error) {
	var startSyncHeight uint64
	err := j.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(syncInfoBucket))
		if bucket == nil {
			return nil
		}
		heightBytes := bucket.Get([]byte(lastCFilterHeightKey))
		if heightBytes == nil {
			return nil
		}
		startSyncHeight = binary.BigEndian.Uint64(heightBytes)
		return nil
	})
	return startSyncHeight, err
}

func (j *jobDB) setLastSuccessRunDate(completeDate time.Time) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(syncInfoBucket))
		if err != nil {
			return err
		}
		t := make([]byte, 8)
		binary.BigEndian.PutUint64(t, uint64(completeDate.Unix()))
		return bucket.Put([]byte(lastSuccessRunDateKey), t)
	})
}

func (j *jobDB) lastSuccessRunDate() (time.Time, error) {
	var lastRunDate uint64
	err := j.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(syncInfoBucket))
		if bucket == nil {
			return nil
		}
		dateBytes := bucket.Get([]byte(lastSuccessRunDateKey))
		if dateBytes == nil {
			return nil
		}
		lastRunDate = binary.BigEndian.Uint64(dateBytes)
		return nil
	})
	return time.Unix(int64(lastRunDate), 0), err
}
