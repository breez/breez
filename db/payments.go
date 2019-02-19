package db

import (
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

// PaymentType is the type of payment
type PaymentType byte

const (
	//SentPayment is type for sent payments
	SentPayment = PaymentType(0)

	//ReceivedPayment is type for received payments
	ReceivedPayment = PaymentType(1)

	//DepositPayment is type for payment got from add funds
	DepositPayment = PaymentType(2)

	//WithdrawalPayment is type for payment got from remove funds
	WithdrawalPayment = PaymentType(3)
)

/*
PaymentInfo is the structure that holds the data for a payment in breez
*/
type PaymentInfo struct {
	Type                       PaymentType
	Amount                     int64
	Fee                        int64
	CreationTimestamp          int64
	Description                string
	PayeeName                  string
	PayeeImageURL              string
	PayerName                  string
	PayerImageURL              string
	TransferRequest            bool
	PaymentHash                string
	RedeemTxID                 string
	Destination                string
	PendingExpirationHeight    uint32
	PendingExpirationTimestamp int64
}

/*
AddAccountPayment adds a payment to the database
*/
func (db *DB) AddAccountPayment(accPayment *PaymentInfo, receivedIndex uint64, sentTime uint64) error {
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

/*
FetchAllAccountPayments fetches all payments in the database sorted by date
*/
func (db *DB) FetchAllAccountPayments() ([]*PaymentInfo, error) {
	var payments []*PaymentInfo
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

/*
FetchPaymentsSyncInfo returns the last payment time and last invoice settled index.
This is used for callers to understand when needs to be synchronized into the db
*/
func (db *DB) FetchPaymentsSyncInfo() (lastTime int64, lastSetteledIndex uint64) {
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

// SavePaymentRequest saves a payment request into the database
func (db *DB) SavePaymentRequest(payReqHash string, payReq []byte) error {
	return db.saveItem([]byte(incmoingPayReqBucket), []byte(payReqHash), payReq)
}

// FetchPaymentRequest fetches a payment request by a payment hash
func (db *DB) FetchPaymentRequest(payReqHash string) ([]byte, error) {
	return db.fetchItem([]byte(incmoingPayReqBucket), []byte(payReqHash))
}

func serializePaymentInfo(s *PaymentInfo) ([]byte, error) {
	return json.Marshal(s)
}

func deserializePaymentInfo(paymentBytes []byte) (*PaymentInfo, error) {
	var payment PaymentInfo
	err := json.Unmarshal(paymentBytes, &payment)
	return &payment, err
}
