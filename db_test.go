package breez

import (
	"strings"
	"testing"
)

func TestAddresses(t *testing.T) {
	openDB("testdb")
	defer deleteDB()
	if err := saveFundingAddress("addr1"); err != nil {
		t.Error(err)
	}
	if err := saveFundingAddress("addr2"); err != nil {
		t.Error(err)
	}
	addresses := fetchAllFundingAddresses()

	if len(addresses) != 2 {
		t.Error("addresses length is ", len(addresses))
		t.FailNow()
	}
	if strings.Compare(addresses[0], "addr1") != 0 || strings.Compare(addresses[1], "addr2") != 0 {
		t.Error("addresses from db are ", addresses)
		t.FailNow()
	}
}

func TestAddPayments(t *testing.T) {
	var err error
	openDB("testdb")
	defer deleteDB()
	err = addAccountPayment([]byte{}, 1, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	err = addAccountPayment([]byte{}, 0, 11)
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
	err = addAccountPayment([]byte{}, 5, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}
	err = addAccountPayment([]byte{}, 4, 0)
	if err != nil {
		t.Error("failed to add payment", err)
	}

	err = addAccountPayment([]byte{}, 0, 13)
	if err != nil {
		t.Error("failed to add payment", err)
	}
	err = addAccountPayment([]byte{}, 0, 0)
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
