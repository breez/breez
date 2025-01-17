package db

import bolt "go.etcd.io/bbolt"

// FetchNostrPubKey fetches the nostrPrivatekey for nostr auth.
func (db *DB) FetchNostrPrivKey(createNew func() (string)) (string, error) {
	var key []byte
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(nostrAuthBucket))
		key = b.Get([]byte("key"))
		if key == nil {
			newKey := createNew()
			
			keyBytesToStore := []byte(newKey)
			if err := b.Put([]byte("key"), keyBytesToStore); err != nil {
				return err
			}
				
			key = keyBytesToStore
		}
		return nil
	})
	
	return string(key), err
}


func(db *DB) StoreNostrPrivKey(privateKey string)(error){
	return db.saveItem([]byte(nostrAuthBucket) ,[]byte("key") ,  []byte(privateKey))
}

func(db *DB) DeletePresentKey()error{
	return db.deleteItem([]byte(nostrAuthBucket) , []byte("key"))
}