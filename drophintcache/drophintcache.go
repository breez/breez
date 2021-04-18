package drophintcache

import (
	"path"

	"github.com/breez/breez/config"
	"github.com/btcsuite/btcwallet/walletdb"
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

	db, err := channeldb.Open(path.Join(workingDir, "data/graph/", cfg.Network))
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
	return db.Update(func(tx walletdb.ReadWriteTx) error {
		var err error
		spendBucket := tx.ReadWriteBucket(spendHintBucket)
		if spendBucket != nil {
			err = tx.DeleteTopLevelBucket(spendHintBucket)
			if err != nil {
				return err
			}
		}

		confirmBucket := tx.ReadWriteBucket(confirmHintBucket)
		if confirmBucket != nil {
			err = tx.DeleteTopLevelBucket(confirmHintBucket)
		}
		return err
	}, func() {})
}
