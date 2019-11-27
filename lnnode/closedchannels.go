package lnnode

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/lightningnetwork/lnd/channeldb"
)

type txInfo struct {
	Spent  bool `json:"spent"`
	Status struct {
		Confirmed bool `json:"confirmed"`
	} `json:"status"`
}

func isSpent(txSpentURL string, txid string, vout uint32) (spent, confirmed bool, err error) {
	txSpentURL = strings.ReplaceAll(txSpentURL, ":txid", txid)
	txSpentURL = strings.ReplaceAll(txSpentURL, ":vout", strconv.FormatUint(uint64(vout), 10))
	var resp *http.Response
	if resp, err = http.Get(txSpentURL); err != nil {
		return
	}
	defer resp.Body.Close()
	var body []byte
	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		return
	}
	var txi txInfo
	if err = json.Unmarshal(body, &txi); err != nil {
		return
	}
	return txi.Spent, txi.Status.Confirmed, nil
}

func closedChannels(db *channeldb.DB, txSpentURL string) (int, error) {
	channels, err := db.FetchAllOpenChannels()
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, c := range channels {
		txid := c.FundingOutpoint.Hash.String()
		vout := c.FundingOutpoint.Index
		spent, _, err := isSpent(txSpentURL, txid, vout)
		if err != nil {
			return closed, err
		}
		if spent {
			closed++
		}
	}
	return closed, nil
}
