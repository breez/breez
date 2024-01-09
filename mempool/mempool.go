package mempool

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/btcsuite/btcd/btcutil"
)

const (
	url = "https://mempool.space/api"
)

type AddressUTXO struct {
	Txid   string `json:"txid"`
	Vout   uint32 `json:"vout"`
	Value  int64  `json:"value"`
	Status struct {
		Confirmed   bool   `json:"confirmed"`
		BlockHeight uint64 `json:"block_height"`
		BlockHash   string `json:"block_hash"`
		BlockTime   uint32 `json:"block_time"`
	} `json:"status"`
}

type RecommendedFees struct {
	FastestFee  int64 `json:"fastestFee"`
	HalfHourFee int64 `json:"halfHourFee"`
	HourFee     int64 `json:"hourFee"`
	EconomyFee  int64 `json:"economyFee"`
	MinimumFee  int64 `json:"minimumFee"`
}

func GetAddressUTXO(address string) ([]AddressUTXO, error) {
	body, err := get(fmt.Sprintf("address/%s/utxo", address))
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var utxos []AddressUTXO
	if err = json.NewDecoder(body).Decode(&utxos); err != nil {
		return nil, fmt.Errorf("mempool.space address/utxo decode error: %w", err)
	}

	return utxos, nil
}

func GetRecommendedFees() (RecommendedFees, error) {
	fees := RecommendedFees{}
	body, err := get("v1/fees/recommended")
	if err != nil {
		return fees, err
	}
	defer body.Close()

	if err = json.NewDecoder(body).Decode(&fees); err != nil {
		return RecommendedFees{}, fmt.Errorf("mempool.space recommended fees unmarshall error: %w", err)
	}
	return fees, nil
}

func GetTransactionRaw(txID string) (*btcutil.Tx, error) {
	body, err := get(fmt.Sprintf("tx/%s/raw", txID))
	if err != nil {
		return nil, err
	}
	defer body.Close()

	tx, err := btcutil.NewTxFromReader(body)
	if err != nil {
		return nil, err
	}
	return tx, err
}

func get(endpoint string) (io.ReadCloser, error) {
	resp, err := http.Get(fmt.Sprintf("%s/%s", url, endpoint))
	if err != nil {
		return nil, fmt.Errorf("mempool.space %s error: %w", endpoint, err)
	}

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		defer resp.Body.Close()
		return nil, fmt.Errorf("mempool.space %s bad statuscode %v", endpoint, resp.StatusCode)
	}
	return resp.Body, nil
}
