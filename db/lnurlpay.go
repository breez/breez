package db

import (
	"encoding/json"
	"fmt"

	"github.com/breez/breez/data"
	bolt "go.etcd.io/bbolt"
)

func (db *DB) SaveLNUrlPayInfo(info *data.LNUrlPayInfo) error {

	db.log.Info("SaveLNUrlPayInfo")
	err := db.Update(func(tx *bolt.Tx) error {

		b := tx.Bucket([]byte(lnurlPayBucket))
		if data := b.Get([]byte(info.PaymentHash)); data == nil {

			buf, err := json.Marshal(info)
			if err != nil {
				return err
			}

			b.Put([]byte(info.PaymentHash), buf)
			return nil

		} else {

			return fmt.Errorf("successAction for paymentHash %x already exists.", info.PaymentHash)

		}
	})

	return err
}

func (db *DB) FetchLNUrlPayInfo(paymentHash string) (*data.LNUrlPayInfo, error) {

	var info *data.LNUrlPayInfo
	err := db.View(func(tx *bolt.Tx) (err error) {
		b := tx.Bucket([]byte(lnurlPayBucket))
		if v := b.Get([]byte(paymentHash)); v != nil {
			if info, err = deserializeLNUrlPayInfo(v); err != nil {
				return err
			}
		}

		return nil
	})

	return info, err
}

func (db *DB) FetchAllLNUrlPayInfos() ([]*data.LNUrlPayInfo, error) {

	var infos []*data.LNUrlPayInfo
	err := db.View(func(tx *bolt.Tx) error {

		b := tx.Bucket([]byte(lnurlPayBucket))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if v == nil {
				continue
			}
			info, err := deserializeLNUrlPayInfo(v) 
			if err != nil {
				return err
			}
			infos = append(infos, info)
		}

		return nil
	})

	// db.log.Infof("FetchAllLNUrlPayInfos infos = %v", infos)
	return infos, err
}

func deserializeLNUrlPayInfo(bytes []byte) (*data.LNUrlPayInfo, error) {
	var info data.LNUrlPayInfo
	err := json.Unmarshal(bytes, &info)
	return &info, err
}
