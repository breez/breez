package breez

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	"github.com/breez/breez/data"
	"github.com/golang/protobuf/proto"
	bolt "go.etcd.io/bbolt"
)

const (
	versionBucket          = "version"
	incmoingPayReqBucket   = "paymentRequests"
	addressesBucket        = "swap_addresses"
	paymentsBucket         = "payments"
	redeemableHashesBucket = "redeemableHashes"
	paymentsHashBucket     = "paymentsByHash"
	paymentsSyncInfoBucket = "paymentsSyncInfo"
	accountBucket          = "account"
)

var (
	migrations = []func(tx *bolt.Tx) error{
		convertPaymentsFromProto,
	}
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
		return migrateDB(tx)
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

func migrateDB(tx *bolt.Tx) error {
	versionB := tx.Bucket([]byte(versionBucket))
	upgradedVersion := uint64(len(migrations))
	var currentVersion uint64
	currentVersionBuf := versionB.Get([]byte("currentVersion"))
	if currentVersionBuf != nil {
		currentVersion = btoi(currentVersionBuf)
	}
	if currentVersion >= upgradedVersion {
		return nil
	}
	i := currentVersion
	for i < upgradedVersion {
		if err := migrations[i](tx); err != nil {
			log.Criticalf("migration failed ", err)
			return err
		}
		i++
	}
	log.Infof("migration passed! updating version to %v", i)
	return versionB.Put([]byte("currentVersion"), itob(i))
}

func convertPaymentsFromProto(tx *bolt.Tx) error {
	log.Infof("running migration: convertPaymentsFromProto")
	paymentB := tx.Bucket([]byte(paymentsBucket))
	return paymentB.ForEach(func(k, v []byte) error {
		if v == nil {
			return nil
		}
		var protoPayment data.Payment
		if err := proto.Unmarshal(v, &protoPayment); err != nil {
			return err
		}

		fmt.Println("migrating payment item: ", protoPayment)
		payment := &paymentInfo{
			Amount:            protoPayment.Amount,
			CreationTimestamp: protoPayment.CreationTimestamp,
		}
		if protoPayment.InvoiceMemo != nil {
			payment.Description = protoPayment.InvoiceMemo.Description
			payment.PayeeImageURL = protoPayment.InvoiceMemo.PayeeImageURL
			payment.PayeeName = protoPayment.InvoiceMemo.PayeeName
			payment.PayerImageURL = protoPayment.InvoiceMemo.PayerImageURL
			payment.PayerName = protoPayment.InvoiceMemo.PayerName
		}

		switch protoPayment.Type {
		case data.Payment_SENT:
			payment.Type = sentPayment
		case data.Payment_RECEIVED:
			payment.Type = receivedPayment
		case data.Payment_DEPOSIT:
			payment.Type = depositPayment
		case data.Payment_WITHDRAWAL:
			payment.Type = withdrawalPayment
		}
		paymentBuf, err := serializePaymentInfo(payment)
		if err != nil {
			return err
		}
		return paymentB.Put(k, paymentBuf)
	})
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
		return nil
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
func fetchAllSwapAddresses() ([]*swapAddressInfo, error) {
	return fetchSwapAddresses(func(addr *swapAddressInfo) bool {
		return true
	})
}

func fetchSwapAddresses(filterFunc func(addr *swapAddressInfo) bool) ([]*swapAddressInfo, error) {
	var addresses []*swapAddressInfo
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(addressesBucket))
		b.ForEach(func(k, v []byte) error {
			var address swapAddressInfo
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

func saveSwapAddressInfo(address *swapAddressInfo) error {
	bytes, err := serializeSwapAddressInfo(address)
	if err != nil {
		return err
	}
	return saveItem([]byte(addressesBucket), []byte(address.Address), bytes)
}

func removeSwapAddress(address string) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(addressesBucket))
		return b.Delete([]byte(address))
	})
}

func removeSwapAddressByPaymentHash(pHash []byte) (bool, error) {
	var found bool
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(addressesBucket))
		return b.ForEach(func(k, v []byte) error {
			address, err := deserializeSwapAddressInfo(v)
			if err != nil {
				return err
			}
			if bytes.Equal(address.PaymentHash, pHash) {
				found = true
				return b.Delete(k)
			}
			return nil
		})
	})
	return found, err
}

func updateSwapAddressInfo(address string, updateFund func(*swapAddressInfo)) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(addressesBucket))
		addressInfoBytes := b.Get([]byte(address))
		var decodedAddressInfo swapAddressInfo
		err := json.Unmarshal(addressInfoBytes, &decodedAddressInfo)
		if err != nil {
			return err
		}
		updateFund(&decodedAddressInfo)
		addressInfoBytes, err = json.Marshal(decodedAddressInfo)
		if err != nil {
			return err
		}

		return b.Put([]byte(address), addressInfoBytes)
	})
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
