package account

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"

	"github.com/btcsuite/btcd/btcec"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/tyler-smith/go-bip32"

	"github.com/breez/breez/data"
	"github.com/fiatjaf/go-lnurl"
)

type LoginResponse struct {
	lnurl.LNURLResponse
	Token string `json:"token"`
}

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
		return nil, errors.New("Unsupported LNURL")
	}
}

// FinishLNURLAuth logs in using lnurl auth protocol
func (a *Service) FinishLNURLAuth(authParams *data.LNURLAuth) (string, error) {

	key, err := a.getLNURLAuthKey()
	if err != nil {
		return "", err
	}

	// hash host using master key
	h := hmac.New(sha256.New, key.Key)
	if _, err := h.Write([]byte(authParams.Host)); err != nil {
		return "", err
	}
	sha := h.Sum(nil)

	// create 4 elements derivation path using hashed value.
	first16 := sha[:16]
	for i := 0; i < 4; i++ {
		nextChildIndex := binary.BigEndian.Uint32(first16[i*4 : i*4+4])
		for key, err = key.NewChildKey(nextChildIndex); err != nil; {
			nextChildIndex++
		}
	}

	// this is the result keypair.
	linkingPrivKey, linkingPubKey := btcec.PrivKeyFromBytes(btcec.S256(), key.Key)
	k1Decoded, err := hex.DecodeString(authParams.K1)
	if err != nil {
		return "", fmt.Errorf("failed to decode k1 challenge %w", err)
	}

	// sign the challenge
	sig, err := linkingPrivKey.Sign(k1Decoded)
	if err != nil {
		return "", fmt.Errorf("failed to sign k1 challenge %w", err)
	}

	//convert to DER
	wireSig, err := lnwire.NewSigFromSignature(sig)
	if err != nil {
		return "", fmt.Errorf("can't convert sig to wire format: %v", err)
	}
	der := wireSig.ToSignatureBytes()

	// call the service
	url, err := url.Parse(authParams.Callback)
	if err != nil {
		return "", fmt.Errorf("invalid callback url %v", err)
	}
	query := url.Query()
	query.Add("key", hex.EncodeToString(linkingPubKey.SerializeCompressed()))
	query.Add("sig", hex.EncodeToString(der))
	if authParams.Jwt {
		query.Add("jwt", "true")
	}
	url.RawQuery = query.Encode()
	resp, err := http.Get(url.String())
	if err != nil {
		return "", err
	}

	// check response
	var lnurlresp LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&lnurlresp)
	if err != nil {
		return "", err
	}

	if lnurlresp.Status == "ERROR" {
		return "", errors.New(lnurlresp.Reason)
	}

	return lnurlresp.Token, nil
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

func (a *Service) getLNURLAuthKey() (*bip32.Key, error) {
	needsBackup := false
	key, err := a.breezDB.FetchLNURLAuthKey(func() ([]byte, error) {
		needsBackup = true
		return bip32.NewSeed()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lnurl key %w", err)
	}
	if needsBackup {
		a.requestBackup()
	}

	// Create master private key from seed
	masterKey, err := bip32.NewMasterKey(key)
	if err != nil {
		return nil, fmt.Errorf("error creating lnurl master key: %w", err)
	}

	return masterKey, nil
}
