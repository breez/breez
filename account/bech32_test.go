package account

import (
	"fmt"
	"testing"

	"github.com/breez/breez/config"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

type MockTester struct {
}

// NOTE you will need to remove the logging from the ValidateAddress function in order to run these tests.
func TestValidbech32m(t *testing.T) {
	mockService := Service{
		cfg: &config.Config{
			Network: "mainnet",
		},
		activeParams: &chaincfg.MainNetParams,
	}

	addresses := [...]string{
		"BC1QW508D6QEJXTDG4Y5R3ZARVARY0C5XW7KV8F3T4",
		"bc1pw508d6qejxtdg4y5r3zarvary0c5xw7kw508d6qejxtdg4y5r3zarvary0c5xw7kt5nd6y",
		"BC1SW50QGDZ25J",
		"bc1zw508d6qejxtdg4y5r3zarvaryvaxxpcs",
		"bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0",
	}

	fmt.Println("testValidbech32m")
	for _, address := range addresses {
		err := mockService.ValidateAddress(address)
		if err == nil {
			return
		} else {
			t.Fatal("invalid address")
		}

	}

}

// NOTE you will need to remove the logging from the ValidateAddress function in order to run these tests.
func TestInvalidbech32m(t *testing.T) {
	mockService := Service{
		cfg: &config.Config{
			Network: "mainnet",
		},
		activeParams: &chaincfg.MainNetParams,
	}
	var err error

	addresses := [...]string{
		"BC130XLXVLHEMJA6C4DQV22UAPCTQUPFHLXM9H8Z3K2E72Q4K9HCZ7VQ7ZWS8R",
		"bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqh2y7hd",
		"BC1S0XLXVLHEMJA6C4DQV22UAPCTQUPFHLXM9H8Z3K2E72Q4K9HCZ7VQ54WELL",
		"bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7v8n0nx0muaewav253zgeav",
		"BC1QR508D6QEJXTDG4Y5R3ZARVARYV98GJ9P",
		"bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7v07qwwzcrf"}

	for _, address := range addresses {
		err = mockService.ValidateAddress(address)
		if err == nil {
			t.Fatalf("error %v", err)
		}
	}
}

func createAddress() (btcutil.Address, error) {
	privateKey, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, err
	}
	tapKey := txscript.ComputeTaprootKeyNoScript(privateKey.PubKey())
	address, err := btcutil.NewAddressPubKey(tapKey.SerializeCompressed(), &chaincfg.MainNetParams)
	if err != nil {
		return nil, err
	}
	return address, nil
}
