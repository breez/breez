package bootstrap

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/log"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb/bdb"
	"github.com/lightningnetwork/lnd/channeldb"
	"go.etcd.io/bbolt"
)

var (
	logger                btclog.Logger
	ErrMissingPolicyError = errors.New("missing channel policy")
)

// SyncGraphDB syncs the channeldb from another db.
func SyncGraphDB(workingDir, sourceDBPath string) error {
	var err error
	if logger == nil {
		logger, err = log.GetLogger(workingDir, "BOOTSTRAP")
		if err != nil {
			return err
		}
		logger.SetLevel(btclog.LevelDebug)
	}
	logger.Infof("SyncGraphDB workingDir=%v file=%v", workingDir, sourceDBPath)
	dir, fileName := filepath.Split(sourceDBPath)
	if fileName != "channel.db" {
		return errors.New("file name must be channel.db")
	}

	logger.Info("opening channel db...")

	// open the source database.
	channelDBSource, err := channeldb.Open(dir)
	if err != nil {
		return err
	}
	logger.Info("opening channel db was succesfull")
	defer channelDBSource.Close()

	// open the destination database.
	channelDBDest, cleanup, err := channeldbservice.Get(workingDir)
	if err != nil {
		return fmt.Errorf("failed to open channeldb %v", err)
	}

	logger.Info("channel db destination service opened succesfully")
	defer cleanup()
	sourceDB, err := bdb.UnderlineDB(channelDBSource.Backend)
	if err != nil {
		return err
	}
	logger.Info("bdb.UnderlineDB opened succesfully")

	// buckets we want to copy from source to destination.
	bucketsToCopy := map[string]struct{}{
		"graph-edge": {},
		"graph-meta": {},
		"graph-node": {},
	}

	// utility function to convert bolts key to a string path.
	extractPathElements := func(bytesPath [][]byte, key []byte) []string {
		var path []string
		for _, b := range bytesPath {
			path = append(path, string(b))
		}
		return append(path, string(key))
	}

	ourNode, err := ourNode(channelDBDest)
	if err != nil {
		return err
	}

	logger.Info("ourNode fetched succesfully")

	kvdbTx, err := channelDBDest.BeginReadWriteTx()
	if err != nil {
		return err
	}
	logger.Info("got kvdbTx")

	tx, err := bdb.UnderlineTX(kvdbTx)
	if err != nil {
		return err
	}

	logger.Info("got bdb.UnderlineTX")

	defer tx.Rollback()

	if ourNode == nil && hasSourceNode(tx) {
		return errors.New("source node was set before sync transaction, rolling back")
	}

	if ourNode != nil {
		logger.Info("ourNode != nil")
		channelNodes, channels, policies, err := ourData(kvdbTx, ourNode)
		if err != nil {
			return err
		} else {
			logger.Info("ourNode = nil")
		}

		// add our data to the source db.
		if err := putOurData(channelDBSource, ourNode, channelNodes, channels, policies); err != nil {
			return err
		}
		logger.Info("putOurData was succesfull")
	}

	// clear graph data from the destination db
	for b := range bucketsToCopy {
		if err := tx.DeleteBucket([]byte(b)); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
	}
	logger.Info("destination buckets were deleted succesfully")

	err = merge(tx, sourceDB,
		func(keyPath [][]byte, k []byte, v []byte) bool {
			pathElements := extractPathElements(keyPath, k)
			_, shouldCopy := bucketsToCopy[pathElements[0]]
			return !shouldCopy
		})
	if err != nil {
		logger.Infof("merged error %v", err)
		return err
	}
	logger.Info("merged succesfully")
	return tx.Commit()
}

// Merge copies from source to dest and ignoring items using the skip function.
// It is different from Compact in that it tries to create a bucket only if not exists.
func merge(tx *bbolt.Tx, src *bbolt.DB, skip skipFunc) error {

	if err := walk(src, func(keys [][]byte, k, v []byte, seq uint64) error {
		keyStr := ""
		for _, kk := range keys {
			keyStr += "." + string(kk)
		}
		keyStr += "." + string(k)

		logger.Infof("walk started keyStr = %v", keyStr)
		// Create bucket on the root transaction if this is the first level.
		nk := len(keys)
		if nk == 0 {
			logger.Info("nk == 0")
			bkt, err := tx.CreateBucketIfNotExists(k)
			if err != nil {
				logger.Infof("error creating bucket %v", k)
				return err
			}
			logger.Info("before SetSequence")
			if err := bkt.SetSequence(seq); err != nil {
				logger.Infof("SetSequence %v", err)
				return err
			}
			logger.Info("after SetSequence")
			return nil
		}

		// Create buckets on subsequent levels, if necessary.
		logger.Infof("fetching bucket %v", string(keys[0]))
		b := tx.Bucket(keys[0])
		if b == nil {
			logger.Infof("fetched null bucket!! %v", string(keys[0]))
		}
		if nk > 1 {
			for _, k := range keys[1:] {
				logger.Infof("fetching nested bucket %v", string(k))
				b = b.Bucket(k)
				if b == nil {
					logger.Info("fetched null bucket!!")
				}
			}
		}

		// Fill the entire page for best compaction.
		b.FillPercent = 1.0

		// If there is no value then this is a bucket call.
		if v == nil {
			logger.Infof("merge: before CreateBucketIfNotExists %v", string(k))
			bkt, err := b.CreateBucketIfNotExists(k)
			if err != nil {
				return err
			}
			logger.Infof("merge: after CreateBucketIfNotExists %v", string(k))
			if err := bkt.SetSequence(seq); err != nil {
				return err
			}
			logger.Infof("merge: after SetSequence %v", seq)
			return nil
		}

		// Otherwise treat it as a key/value pair.
		return b.Put(k, v)
	}, skip); err != nil {
		logger.Infof("merge returned with error %v", err)
		return err
	}
	logger.Infof("merge succesfull")
	return nil
}

// walkFunc is the type of the function called for keys (buckets and "normal"
// values) discovered by Walk. keys is the list of keys to descend to the bucket
// owning the discovered key/value pair k/v.
type walkFunc func(keys [][]byte, k, v []byte, seq uint64) error

type skipFunc func(keys [][]byte, k, v []byte) bool

// walk walks recursively the bolt database db, calling walkFn for each key it finds.
func walk(db *bbolt.DB, walkFn walkFunc, skipFn skipFunc) error {
	return db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			return walkBucket(b, nil, name, nil, b.Sequence(), walkFn, skipFn)
		})
	})
}

func walkBucket(b *bbolt.Bucket, keypath [][]byte, k, v []byte, seq uint64, fn walkFunc, skip skipFunc) error {

	if skip != nil && skip(keypath, k, v) {
		return nil
	}

	// Execute callback.
	if err := fn(keypath, k, v, seq); err != nil {
		return err
	}

	// If this is not a bucket then stop.
	if v != nil {
		return nil
	}

	// Iterate over each child key/value.
	keypath = append(keypath, k)
	return b.ForEach(func(k, v []byte) error {
		if v == nil {
			bkt := b.Bucket(k)
			return walkBucket(bkt, keypath, k, nil, bkt.Sequence(), fn, skip)
		}
		return walkBucket(b, keypath, k, v, b.Sequence(), fn, skip)
	})
}
