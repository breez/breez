package account

import (
	"context"
	"errors"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcutil"
)

const (
	defaultSatPerByteFee = 50
)

/*
ValidateAddress validates a bitcoin address based on the network type
*/
func (a *Service) ValidateAddress(address string) error {
	var network *chaincfg.Params

	if a.cfg.Network == "testnet" {
		network = &chaincfg.TestNet3Params
	} else if a.cfg.Network == "simnet" {
		network = &chaincfg.SimNetParams
	} else if a.cfg.Network == "mainnet" {
		network = &chaincfg.MainNetParams
	} else {
		return errors.New("unknown network type " + a.cfg.Network)
	}

	_, err := btcutil.DecodeAddress(address, network)
	if err != nil {
		a.log.Errorf("Error parsing %s as address\t", address)
		return err
	}

	return nil
}

/*
SendWalletCoins executes a request to send wallet coins to a particular address.
*/
func (a *Service) SendWalletCoins(address string, satAmount, satPerByteFee int64) (string, error) {
	lnclient := a.daemon.APIClient()
	res, err := lnclient.SendCoins(context.Background(), &lnrpc.SendCoinsRequest{Addr: address, Amount: satAmount, SatPerByte: satPerByteFee})
	if err != nil {
		return "", err
	}
	return res.Txid, nil
}

/*
GetDefaultSatPerByteFee returns the default sat per byte fee for on chain transactions
*/
func (a *Service) GetDefaultSatPerByteFee() int64 {
	return defaultSatPerByteFee
}

/*
RegisterPeriodicSync registeres this token for periodic sync notifications.
*/
func (a *Service) RegisterPeriodicSync(token string) error {
	c, ctx, cancel := a.breezAPI.NewSyncNotifierClient()
	defer cancel()
	_, err := c.RegisterPeriodicSync(ctx, &breezservice.RegisterPeriodicSyncRequest{NotificationToken: token})
	if err != nil {
		a.log.Errorf("fail to register for periodic sync: %v", err)
	} else {
		a.log.Info("registered successfuly for periodic sync")
	}
	return err
}
