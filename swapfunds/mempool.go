package swapfunds

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	mempoolAPI = "https://mempool.space/api"
)

type AddressUTXO struct {
	Txid   string `json:"txid"`
	Vout   int32  `json:"vout"`
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

func (s *Service) GetMempoolAddressUTXOs(address string) ([]AddressUTXO, error) {
	resp, err := http.Get(fmt.Sprintf("%s/address/%s/utxo", mempoolAPI, address))
	if err != nil {
		return nil, fmt.Errorf("mempool.space address/utxo error: %w", err)
	}
	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, fmt.Errorf("mempool.space address/utxo bad statuscode %v", resp.StatusCode)
	}

	utxos := []AddressUTXO{}
	err = json.NewDecoder(resp.Body).Decode(&utxos)
	if err != nil {
		return nil, fmt.Errorf("mempool.space address/utxo decode error: %w", err)
	}
	return utxos, nil
}

func (s *Service) GetMempoolRecommendedFees() (RecommendedFees, error) {
	fees := RecommendedFees{}

	resp, err := http.Get(fmt.Sprintf("%s/v1/fees/recommended", mempoolAPI))
	if err != nil {
		return fees, fmt.Errorf("mempool.space recommended fees error: %w", err)
	}
	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return fees, fmt.Errorf("mempool.space recommended bad statuscode %v", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(&fees)
	if err != nil {
		return RecommendedFees{}, fmt.Errorf("mempool.space recommended fees unmarshall error: %w", err)
	}
	return fees, nil
}
