package drophintcache

import (
	"path"

	"github.com/breez/breez/config"
	bolt "github.com/coreos/bbolt"
	"github.com/lightningnetwork/lnd/channeldb"
)

var (
	spendHintBucket   = []byte("spend-hints")
	confirmHintBucket = []byte("confirm-hints")
)

func Drop(workingDir string) error {
	cfg, err := config.GetConfig(workingDir)
	if err != nil {
		return err
	}

	db, err := channeldb.Open(path.Join(workingDir, "data/chain/bitcoin/", cfg.Network, "channel.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	err = deleteBuckets(db)

	return err
}

// initBuckets ensures that the primary buckets used by the circuit are
// initialized so that we can assume their existence after startup.
func deleteBuckets(db *channeldb.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		err := tx.DeleteBucket(spendHintBucket)
		if err != nil {
			return err
		}

		err = tx.DeleteBucket(confirmHintBucket)
		return err
	})
}
