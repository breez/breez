package breez

import (
	"strings"
	"testing"
)

func TestAddresses(t *testing.T) {
	if err := openDB("testDB"); err != nil {
		t.Error(err)
	}
	defer deleteDB()
	if err := saveSwapAddressInfo(&swapAddressInfo{Address: "addr1", PaymentHash: []byte{1, 2, 3}}); err != nil {
		t.Error(err)
	}
	if err := saveSwapAddressInfo(&swapAddressInfo{Address: "addr2", PaymentHash: []byte{4, 5, 6}}); err != nil {
		t.Error(err)
	}
	addresses, err := fetchAllSwapAddresses()

	if len(addresses) != 2 {
		t.Error("addresses length is ", len(addresses))
	}
	if strings.Compare(addresses[0].Address, "addr1") != 0 || strings.Compare(addresses[1].Address, "addr2") != 0 {
		t.Error("addresses from db are ", addresses[0].Address)
	}

	found, err := updateSwapAddressByPaymentHash([]byte{1, 2, 3}, func(a *swapAddressInfo) error {
		a.ConfirmedAmount = 100
		return nil
	})
	if err != nil || !found {
		t.Errorf("failed to update swap address found=%v, error = %v", found, err)
	}

	found, err = updateSwapAddress("addr2", func(a *swapAddressInfo) error {
		a.ConfirmedAmount = 200
		return nil
	})
	if err != nil || !found {
		t.Errorf("failed to update swap address found=%v, error = %v", found, err)
	}

	addresses, err = fetchAllSwapAddresses()
	if len(addresses) != 2 {
		t.Error("addresses length is ", len(addresses))
	}

	if addresses[0].ConfirmedAmount != 100 {
		t.Errorf("first address confirmed amount = %v", addresses[0].ConfirmedAmount)
	}

	if addresses[1].ConfirmedAmount != 200 {
		t.Errorf("second address confirmed amount = %v", addresses[1].ConfirmedAmount)
	}
}

func TestAddPayments(t *testing.T) {
	var err error
	openDB("testdb")
	defer deleteDB()
	err = addAccountPayment(&paymentInfo{PaymentHash: "h1"}, 1, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	err = addAccountPayment(&paymentInfo{PaymentHash: "h2"}, 0, 11)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	timestamp, settledIndex := fetchPaymentsSyncInfo()
	if timestamp != 11 {
		t.Error("timestamp should be 11 and it is: ", timestamp)
	}
	if settledIndex != 1 {
		t.Error("settled index should be 1 and it is: ", settledIndex)
	}
}

func TestPaymentsSyncInfo(t *testing.T) {
	var err error
	openDB("testdb")
	defer deleteDB()
	err = addAccountPayment(&paymentInfo{PaymentHash: "h1"}, 5, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}
	err = addAccountPayment(&paymentInfo{PaymentHash: "h2"}, 4, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	err = addAccountPayment(&paymentInfo{PaymentHash: "h3"}, 0, 13)
	if err != nil {
		t.Error("failed to add payment", err)
	}
	err = addAccountPayment(&paymentInfo{PaymentHash: "h4"}, 0, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	timestamp, settledIndex := fetchPaymentsSyncInfo()
	if timestamp != 13 {
		t.Error("timestamp should be 13 and it is: ", timestamp)
	}
	if settledIndex != 5 {
		t.Error("settled index should be 5 and it is: ", settledIndex)
	}
}

func TestAccount(t *testing.T) {
	var err error
	openDB("testdb")
	defer deleteDB()
	acc, err := fetchAccount()
	if err != nil {
		t.Error("failed to add payment", err)
	}
	if acc != nil {
		t.Error("account should be nil")
	}
}
