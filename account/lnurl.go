package account

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/breez/breez/data"
	lnurllib "github.com/fiatjaf/go-lnurl"
)

func (a *Service) HandleLNURL(lnurl string) (*data.LNUrlResponse, error) {
	iparams, err := lnurllib.HandleLNURL(lnurl)
	if err != nil {
		return nil, err
	}

	switch params := iparams.(type) {
	case lnurllib.LNURLWithdrawResponse:
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
	default:
		return nil, errors.New("Unsupported LNUrl")
	}
}

func (a *Service) FinishLNURLWithdraw(bolt11 string) error {
	callback := a.lnurlWithdrawing

	resp, err := http.Get(callback + "&pr=" + bolt11)
	if err != nil {
		return err
	}

	var lnurlresp lnurllib.LNURLResponse
	err = json.NewDecoder(resp.Body).Decode(&lnurlresp)
	if err != nil {
		return err
	}

	if lnurlresp.Status == "ERROR" {
		return errors.New(lnurlresp.Reason)
	}

	return nil
}
