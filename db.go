package breez

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	bolt "go.etcd.io/bbolt"
)

const (
	versionBucket        = "version"
	incmoingPayReqBucket = "paymentRequests"

	//add funds
	addressesBucket           = "subswap_addresses"
	swapAddressesByHashBucket = "subswap_addresses_by_hash"

	//remove funds
	redeemableHashesBucket = "redeemableHashes"

	//payments and account
	paymentsBucket         = "payments"
	paymentsHashBucket     = "paymentsByHash"
	paymentsSyncInfoBucket = "paymentsSyncInfo"
	accountBucket          = "account"

	//encrypted sessions
	encryptedSessionsBucket = "encrypted_sessions"
)

var db *bolt.DB

func openDB(dbPath string) error {
	var err error
	db, err = bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Criticalf("Failed to open database %v", err)
		return err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		var err error
		_, err = tx.CreateBucketIfNotExists([]byte(incmoingPayReqBucket))
		if err != nil {
			return err
		}
		paymenetBucket, err := tx.CreateBucketIfNotExists([]byte(paymentsBucket))
		if err != nil {
			return err
		}
		_, err = paymenetBucket.CreateBucketIfNotExists([]byte(paymentsSyncInfoBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(accountBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(addressesBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(swapAddressesByHashBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(versionBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(redeemableHashesBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(paymentsHashBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(encryptedSessionsBucket))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func closeDB() error {
	return db.Close()
}

func deleteDB() error {
	return os.Remove(db.Path())
}

func addRedeemablePaymentHash(hash string) error {
	return db.Update(func(tx *bolt.Tx) error {
		redeemableHashesB := tx.Bucket([]byte(redeemableHashesBucket))
		return redeemableHashesB.Put([]byte(hash), []byte{})
	})
}

func fetchRedeemablePaymentHashes() ([]string, error) {
	var hashes []string
	err := db.View(func(tx *bolt.Tx) error {
		hashB := tx.Bucket([]byte(paymentsHashBucket))
		return hashB.ForEach(func(k, v []byte) error {
			hashes = append(hashes, string(k))
			return nil
		})
	})
	return hashes, err
}

func updateRedeemTxForPayment(hash string, txID string) error {
	log.Infof("updateRedeemTxForPayment hash = %v, txid=%v", hash, txID)
	return db.Update(func(tx *bolt.Tx) error {
		paymentB := tx.Bucket([]byte(paymentsBucket))
		hashB := tx.Bucket([]byte(paymentsHashBucket))
		paymentIndex := hashB.Get([]byte(hash))
		if paymentIndex == nil {
			return fmt.Errorf("payment doesn't exist for hash %v", hash)
		}
		paymentBuf := paymentB.Get(paymentIndex)
		payment, err := deserializePaymentInfo(paymentBuf)
		if err != nil {
			return err
		}
		payment.RedeemTxID = txID
		paymentBuf, err = serializePaymentInfo(payment)
		if err != nil {
			return err
		}
		err = paymentB.Put(paymentIndex, paymentBuf)
		if err != nil {
			return err
		}

		redeemableHashesB := tx.Bucket([]byte(redeemableHashesBucket))
		return redeemableHashesB.Delete([]byte(hash))
	})
}

func addAccountPayment(accPayment *paymentInfo, receivedIndex uint64, sentTime uint64) error {
	log.Infof("addAccountPayment hash = %v", accPayment.PaymentHash)
	return db.Update(func(tx *bolt.Tx) error {
		paymentBuf, err := serializePaymentInfo(accPayment)
		if err != nil {
			return err
		}

		b := tx.Bucket([]byte(paymentsBucket))
		id, err := b.NextSequence()
		if err != nil {
			return err
		}

		//write the payment value with the next sequence as key
		if err := b.Put(itob(id), paymentBuf); err != nil {
			return err
		}

		hashB := tx.Bucket([]byte(paymentsHashBucket))
		if err := hashB.Put([]byte(accPayment.PaymentHash), itob(id)); err != nil {
			return err
		}

		syncInfoBucket := b.Bucket([]byte(paymentsSyncInfoBucket))

		//if we have a newer item, update the last payment timestamp
		lastPaymentTime := uint64(0)
		if lastPaymentTimeBuf := syncInfoBucket.Get([]byte("lastSentPaymentTime")); lastPaymentTimeBuf != nil {
			lastPaymentTime = btoi(lastPaymentTimeBuf)
		}
		if lastPaymentTime < sentTime {
			if err := syncInfoBucket.Put([]byte("lastSentPaymentTime"), itob(sentTime)); err != nil {
				return err
			}
		}

		lastInvoiceSettledIndex := uint64(0)
		if lastInvoiceSettledIndexBuf := syncInfoBucket.Get([]byte("lastSettledIndex")); lastInvoiceSettledIndexBuf != nil {
			lastInvoiceSettledIndex = btoi(lastInvoiceSettledIndexBuf)
		}
		if lastInvoiceSettledIndex < receivedIndex {
			if err := syncInfoBucket.Put([]byte("lastSettledIndex"), itob(receivedIndex)); err != nil {
				return err
			}
		}
		return nil
	})
}

func fetchAllAccountPayments() ([]*paymentInfo, error) {
	var payments []*paymentInfo
	err := db.View(func(tx *bolt.Tx) error {
		// Assume bucket exists and has keys
		b := tx.Bucket([]byte(paymentsBucket))

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if v == nil {
				//nested bucket
				continue
			}
			payment, err := deserializePaymentInfo(v)
			if err != nil {
				return err
			}
			payments = append(payments, payment)
		}

		return nil
	})
	return payments, err
}

func fetchPaymentsSyncInfo() (lastTime int64, lastSetteledIndex uint64) {
	lastPaymentTime := int64(0)
	lastInvoiceSettledIndex := uint64(0)
	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(paymentsBucket))
		syncInfoBucket := b.Bucket([]byte(paymentsSyncInfoBucket))
		lastPaymentTimeBuf := syncInfoBucket.Get([]byte("lastSentPaymentTime"))
		if lastPaymentTimeBuf != nil {
			lastPaymentTime = int64(btoi(lastPaymentTimeBuf))
		}
		lastInvoiceSettledIndexBuf := syncInfoBucket.Get([]byte("lastSettledIndex"))
		if lastInvoiceSettledIndexBuf != nil {
			lastInvoiceSettledIndex = btoi(lastInvoiceSettledIndexBuf)
		}
		return nil
	})
	return lastPaymentTime, lastInvoiceSettledIndex
}

func saveAccount(account []byte) error {
	return saveItem([]byte(accountBucket), []byte("account"), account)
}

func fetchAccount() ([]byte, error) {
	return fetchItem([]byte(accountBucket), []byte("account"))
}

func savePaymentRequest(payReqHash string, payReq []byte) error {
	return saveItem([]byte(incmoingPayReqBucket), []byte(payReqHash), payReq)
}

func fetchPaymentRequest(payReqHash string) ([]byte, error) {
	return fetchItem([]byte(incmoingPayReqBucket), []byte(payReqHash))
}

/**
Swap addresses
**/
func fetchAllSwapAddresses() ([]*SwapAddressInfo, error) {
	return fetchSwapAddresses(func(addr *SwapAddressInfo) bool {
		return true
	})
}

func fetchSwapAddresses(filterFunc func(addr *SwapAddressInfo) bool) ([]*SwapAddressInfo, error) {
	var addresses []*SwapAddressInfo
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(addressesBucket))
		b.ForEach(func(k, v []byte) error {
			var address SwapAddressInfo
			err := json.Unmarshal(v, &address)
			if err != nil {
				return err
			}
			if filterFunc(&address) {
				addresses = append(addresses, &address)
			}
			return nil
		})
		return nil
	})
	return addresses, err
}

func saveSwapAddressInfo(address *SwapAddressInfo) error {
	return db.Update(func(tx *bolt.Tx) error {
		byHashBucket := tx.Bucket([]byte(swapAddressesByHashBucket))
		addressesBucket := tx.Bucket([]byte(addressesBucket))
		bytes, err := serializeSwapAddressInfo(address)
		if err != nil {
			return err
		}
		if err = byHashBucket.Put(address.PaymentHash, []byte(address.Address)); err != nil {
			return err
		}
		if err = addressesBucket.Put([]byte(address.Address), bytes); err != nil {
			return err
		}
		return nil
	})
}

func updateSwapAddressByPaymentHash(pHash []byte, updateFunc func(*SwapAddressInfo) error) (bool, error) {

	address, err := fetchItem([]byte(swapAddressesByHashBucket), pHash)
	if err != nil {
		return false, err
	}
	if address == nil {
		return false, nil
	}
	return updateSwapAddress(string(address), updateFunc)
}

func updateSwapAddress(address string, updateFunc func(*SwapAddressInfo) error) (bool, error) {
	var found bool
	err := db.Update(func(tx *bolt.Tx) error {
		addressesBucket := tx.Bucket([]byte(addressesBucket))
		addressInfoBytes := addressesBucket.Get([]byte(address))
		if addressInfoBytes == nil {
			return nil
		}
		found = true
		swapAddress, err := deserializeSwapAddressInfo(addressInfoBytes)
		if err != nil {
			return err
		}
		if err = updateFunc(swapAddress); err != nil {
			return err
		}

		if err != nil {
			return err
		}

		swapAddressData, err := serializeSwapAddressInfo(swapAddress)
		if err != nil {
			return err
		}

		if err = addressesBucket.Put([]byte(swapAddress.Address), swapAddressData); err != nil {
			return err
		}
		return nil
	})
	return found, err
}

func saveEncryptedSession(sessionID []byte, sessionData []byte) error {
	return saveItem([]byte(encryptedSessionsBucket), sessionID, sessionData)
}

func fetchEncryptedSession(sessionID []byte) ([]byte, error) {
	return fetchItem([]byte(encryptedSessionsBucket), []byte(sessionID))
}

func saveItem(bucket []byte, key []byte, value []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Put(key, value)
	})
}

func fetchItem(bucket []byte, key []byte) ([]byte, error) {
	var value []byte
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		value = b.Get(key)
		return nil
	})
	return value, err
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func btoi(bytes []byte) uint64 {
	return binary.BigEndian.Uint64(bytes)
}
