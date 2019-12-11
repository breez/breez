package lnnode

import (
	"github.com/coreos/bbolt"
	"github.com/lightningnetwork/lnd/channeldb"
)

var (
	edgeBucket   = []byte("graph-edge")
	zombieBucket = []byte("zombie-index")
)

func deleteZombies(chanDB *channeldb.DB) error {
	err := chanDB.Update(func(tx *bbolt.Tx) error {
		edges := tx.Bucket(edgeBucket)
		if edges == nil {
			return channeldb.ErrGraphNoEdgesFound
		}
		return edges.DeleteBucket(zombieBucket)
	})
	return err
}
