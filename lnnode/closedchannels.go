package lnnode

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/channeldb"
)

type txInfo struct {
	Spent  bool `json:"spent"`
	Status struct {
		Confirmed bool `json:"confirmed"`
	} `json:"status"`
}

func TestTxSpentURL(URL string) error {
	spent, confirmed, err := isSpent(
		URL,
		"f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16",
		1,
	)
	if err != nil {
		return err
	}
	if spent && confirmed {
		return nil
	}
	return errors.New("Bad URL")
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

func closedChannels(log btclog.Logger, db *channeldb.DB, txSpentURL string) (int, error) {
	if !(strings.Contains(txSpentURL, ":txid") && strings.Contains(txSpentURL, ":vout")) {
		return 0, errors.New("Bad or disabled URL")
	}
	channels, err := db.FetchAllOpenChannels()
	if err != nil {
		log.Errorf("Error in fetching channels:%v", err)
		return 0, err
	}
	closed := 0
	for _, c := range channels {
		txid := c.FundingOutpoint.Hash.String()
		vout := c.FundingOutpoint.Index
		spent, _, err := isSpent(txSpentURL, txid, vout)
		if err != nil {
			log.Errorf("Error in checking closed channel URL:%v error:%v", txSpentURL, err)
			return closed, err
		}
		if spent {
			closed++
		}
	}
	return closed, nil
}
