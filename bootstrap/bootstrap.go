package bootstrap

import (
	"fmt"
	"strings"

	"github.com/breez/breez/channeldbservice"
	"github.com/coreos/bbolt"
	"github.com/lightningnetwork/lnd/channeldb"
)

const (
	txMaxSize = 65536
)

var (
	edgeBucket   = []byte("graph-edge")
	zombieBucket = []byte("zombie-index")
)

// SyncGraphDB syncs the channeldb from another db.
func SyncGraphDB(workingDir, sourceDBPath string) error {

	// open the source database.
	sourceDB, err := bbolt.Open(sourceDBPath, 0600, nil)
	if err != nil {
		return err
	}
	defer sourceDB.Close()

	// open the destination database.
	channelDB, cleanup, err := channeldbservice.Get(workingDir)
	if err != nil {
		return fmt.Errorf("failed to open channeldb %v", err)
	}
	channelDB.DB.NoSync = true
	defer func() {
		channelDB.DB.NoSync = false
		cleanup()
	}()

	// delete the zombies from the destination db as we replace it.
	if err := deleteZombies(channelDB); err != nil {
		return err
	}

	// buckets we want to copy from source to destination.
	bucketsToCopy := map[string]struct{}{
		"graph-edge": {},
		"graph-meta": {},
		"graph-node": {},
	}

	// paths that we want to exclude from copying.
	skippedPaths := map[string]struct{}{
		"graph-node.source": {},
	}

	// utility function to convert bolts key to a string path.
	extractPathElements := func(bytesPath [][]byte, key []byte) []string {
		var path []string
		for _, b := range bytesPath {
			path = append(path, string(b))
		}
		return append(path, string(key))
	}

	return merge(channelDB.DB, sourceDB,
		func(keyPath [][]byte, k []byte, v []byte) bool {
			pathElements := extractPathElements(keyPath, k)
			_, shouldCopy := bucketsToCopy[pathElements[0]]
			if shouldCopy {
				itemPath := strings.Join(pathElements, ".")
				if _, exists := skippedPaths[itemPath]; exists {
					fmt.Printf("skipping item %v\n", itemPath)
					return true
				}
			}

			return !shouldCopy
		})
}

// Merge copies from source to dest and ignoring items using the skip function.
// It is different from Compact in that it tries to create a bucket only if not exists.
func merge(dst, src *bbolt.DB, skip skipFunc) error {
	// commit regularly, or we'll run out of memory for large datasets if using one transaction.
	var size int64
	tx, err := dst.Begin(true)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := walk(src, func(keys [][]byte, k, v []byte, seq uint64) error {
		// On each key/value, check if we have exceeded tx size.
		sz := int64(len(k) + len(v))
		if size+sz > txMaxSize {
			// Commit previous transaction.
			if err := tx.Commit(); err != nil {
				return err
			}

			// Start new transaction.
			tx, err = dst.Begin(true)
			if err != nil {
				return err
			}
			size = 0
		}
		size += sz

		// Create bucket on the root transaction if this is the first level.
		nk := len(keys)
		if nk == 0 {
			bkt, err := tx.CreateBucketIfNotExists(k)
			if err != nil {
				return err
			}
			if err := bkt.SetSequence(seq); err != nil {
				return err
			}
			return nil
		}

		// Create buckets on subsequent levels, if necessary.
		b := tx.Bucket(keys[0])
		if nk > 1 {
			for _, k := range keys[1:] {
				b = b.Bucket(k)
			}
		}

		// Fill the entire page for best compaction.
		b.FillPercent = 1.0

		// If there is no value then this is a bucket call.
		if v == nil {
			bkt, err := b.CreateBucketIfNotExists(k)
			if err != nil {
				return err
			}
			if err := bkt.SetSequence(seq); err != nil {
				return err
			}
			return nil
		}

		// Otherwise treat it as a key/value pair.
		return b.Put(k, v)
	}, skip); err != nil {
		return err
	}

	return tx.Commit()
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

func deleteZombies(chanDB *channeldb.DB) error {
	err := chanDB.Update(func(tx *bbolt.Tx) error {
		edges := tx.Bucket(edgeBucket)
		if edges == nil {
			return channeldb.ErrGraphNoEdgesFound
		}
		zombies := edges.Bucket(zombieBucket)
		if zombies == nil {
			return nil
		}
		return edges.DeleteBucket(zombieBucket)
	})
	return err
}
