package db

import bolt "go.etcd.io/bbolt"

// FetchNostrPubKey fetches the nostrPrivatekey for nostr auth.
func (db *DB) FetchNostrPrivKey(createNew func() string) (string, error) {
	var key []byte
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(nostrAuthBucket))
		key = b.Get([]byte("key"))
		if key == nil {
			newKey := createNew()
			keyBytesToStore := []byte(newKey)
			b.Put([]byte("key"), keyBytesToStore)
				
			key = keyBytesToStore
		}
		return nil
	})
	
	return string(key), err
}
