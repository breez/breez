package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/breez/breez/data"
	"github.com/fiatjaf/go-lnurl"
)

func (a *Service) HandleLNURL(encodedLnurl string) (*data.LNUrlResponse, error) {
	iparams, err := lnurl.HandleLNURL(encodedLnurl)
	if err != nil {
		return nil, err
	}

	switch params := iparams.(type) {
	case lnurl.LNURLWithdrawResponse:
		qs := params.CallbackURL.Query()
		qs.Set("k1", params.K1)
		params.CallbackURL.RawQuery = qs.Encode()
		a.lnurlWithdrawing = params.CallbackURL.String()
		return &data.LNUrlResponse{
			Action: &data.LNUrlResponse_Withdraw{
				&data.LNUrlWithdraw{
					MinAmount: int64(math.Ceil(
						float64(params.MinWithdrawable) / 1000,
					)),
					MaxAmount: int64(math.Floor(
						float64(params.MaxWithdrawable) / 1000,
					)),
					DefaultDescription: params.DefaultDescription,
				},
			},
		}, nil
	case lnurl.LNURLChannelResponse:
		urispl := strings.Split(params.URI, "@")
		if len(urispl) != 2 {
			return nil, errors.New("LSP provider returned invalid response: " + params.URI)
		}
		remoteid = urispl[0]

		lsp := &lnurlLSP{&params}
		a.lnurlChanneling = &lnurlLSP{&params}

		return &data.LNUrlResponse{
			Action: &data.LNUrlResponse_Channel{
				&data.LNUrlChannel{
					RemoteNodeId: remoteid,
				},
			},
		}, nil
	default:
		return nil, errors.New("Unsupported LNUrl")
	}
}

// implement lsp interface (connect.go)
type lnurlLSP struct {
	*lnurl.LNURLChannelResponse
}

func (lsp *lnurlLSP) Connect(a *Service) error {
	s := strings.Split(lsp.URI, "@")
	if len(s) != 2 {
		return fmt.Errorf("Malformed URI: %v", lsp.URI)
	}
	return a.ConnectPeer(s[0], s[1])
}

func (lsp *lnurlLSP) OpenChannel(a *Service, pubkey string) error {
	//<callback>?k1=<k1>&remoteid=<Local LN node ID>&private=<1/0>
	q := lsp.CallbackURL.Query()
	q.Set("k1", lsp.K1)
	q.Set("remoteid", pubkey)
	q.Set("private", "1")
	lsp.CallbackURL.RawQuery = q.Encode()
	res, err := http.Get(lsp.CallbackURL.String())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	var r lnurl.LNURLResponse
	err = decoder.Decode(&r)
	if err != nil {
		return err
	}
	if r.Status == "ERROR" {
		return fmt.Errorf("Error: %v", r.Reason)
	}
	return nil
}

func (a *Service) FinishLNURLWithdraw(bolt11 string) error {
	callback := a.lnurlWithdrawing

	resp, err := http.Get(callback + "&pr=" + bolt11)
	if err != nil {
		return err
	}

	var lnurlresp lnurl.LNURLResponse
	err = json.NewDecoder(resp.Body).Decode(&lnurlresp)
	if err != nil {
		return err
	}

	if lnurlresp.Status == "ERROR" {
		return errors.New(lnurlresp.Reason)
	}

	return nil
}

func (a *Service) OpenLNURLChannel() error {
	lsp := a.lnurlWithdrawing
	return a.openChannel(lsp, true)
}
