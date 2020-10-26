package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/breez/breez/data"
	"github.com/fiatjaf/go-lnurl"
)

func (a *Service) HandleLNURL(rawString string) (*data.LNUrlResponse, error) {
	encodedLnurl, ok := lnurl.FindLNURLInText(rawString)
	if !ok {
		return nil, fmt.Errorf("'%s' does not contain an LNURL.", rawString)
	}

	a.log.Infof("HandleLNURL %v", encodedLnurl)
	iparams, err := lnurl.HandleLNURL(encodedLnurl)
	if err != nil {
		return nil, err
	}

	switch params := iparams.(type) {
	case lnurl.LNURLAuthParams:
		return &data.LNUrlResponse{
			Action: &data.LNUrlResponse_Auth{
				Auth: &data.LNURLAuth{
					Tag:      params.Tag,
					Callback: params.Callback,
					K1:       params.K1,
					Host:     params.Host,
				},
			},
		}, nil
	case lnurl.LNURLWithdrawResponse:
		qs := params.CallbackURL.Query()
		qs.Set("k1", params.K1)
		params.CallbackURL.RawQuery = qs.Encode()
		a.lnurlWithdrawing = params.CallbackURL.String()
		a.log.Infof("lnurl response: %v", a.lnurlWithdrawing)
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
		return &data.LNUrlResponse{
			Action: &data.LNUrlResponse_Channel{
				Channel: &data.LNURLChannel{
					K1:       params.K1,
					Callback: params.Callback,
					Uri:      params.URI,
				},
			},
		}, nil
	default:
		return nil, errors.New("Unsupported LNUrl")
	}
}

func (a *Service) FinishLNURLAuth(authParams *data.LNURLAuth) error {
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
