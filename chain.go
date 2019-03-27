package breez

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/btcsuite/btcd/chaincfg"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/btcsuite/btcutil"
)

const (
	defaultSatPerByteFee        = 50
	fundingMaxRetries           = 3
	fundingRetryInterval        = 3 * time.Second
	fallbackFee                 = 10000
	defaultBitcoinStaticFeeRate = 50
)

var (
	fundingRunning int32
)

/*
ValidateAddress validates a bitcoin address based on the network type
*/
func ValidateAddress(address string) error {
	var network *chaincfg.Params

	if cfg.Network == "testnet" {
		network = &chaincfg.TestNet3Params
	} else if cfg.Network == "simnet" {
		network = &chaincfg.SimNetParams
	} else if cfg.Network == "mainnet" {
		network = &chaincfg.MainNetParams
	} else {
		return errors.New("unknown network type " + cfg.Network)
	}

	_, err := btcutil.DecodeAddress(address, network)
	if err != nil {
		log.Errorf("Error parsing %s as address\t", address)
		return err
	}

	return nil
}

/*
SendWalletCoins executes a request to send wallet coins to a particular address.
*/
func SendWalletCoins(address string, satAmount, satPerByteFee int64) (string, error) {
	res, err := lightningClient.SendCoins(context.Background(), &lnrpc.SendCoinsRequest{Addr: address, Amount: satAmount, SatPerByte: satPerByteFee})
	if err != nil {
		return "", err
	}
	return res.Txid, nil
}

/*
GetDefaultSatPerByteFee returns the default sat per byte fee for on chain transactions
*/
func GetDefaultSatPerByteFee() int64 {
	return defaultSatPerByteFee
}

/*
RegisterPeriodicSync registeres this token for periodic sync notifications.
*/
func RegisterPeriodicSync(token string) error {
	c, ctx, cancel := servicesClient.NewSyncNotifierClient()
	defer cancel()
	_, err := c.RegisterPeriodicSync(ctx, &breezservice.RegisterPeriodicSyncRequest{NotificationToken: token})
	if err != nil {
		log.Errorf("fail to register for periodic sync: %v", err)
	} else {
		log.Info("registered successfuly for periodic sync")
	}
	return err
}

func syncToChain(pollInterval time.Duration) error {
	for {
		chainInfo, chainErr := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			log.Warnf("Failed get chain info", chainErr)
			return chainErr
		}

		log.Infof("Sync to chain interval Synced=%v BlockHeight=%v", chainInfo.SyncedToChain, chainInfo.BlockHeight)
		if chainInfo.SyncedToChain {
			log.Infof("Synchronized to chain finshed BlockHeight=%v", chainInfo.BlockHeight)
			break
		}
		time.Sleep(pollInterval)
	}
	return nil
}

//This function is responsible for refreshing the account on each transaction.
// mainly it is for synchronizing with channel open/close events.
func watchOnChainState() {
	stream, err := lightningClient.SubscribeTransactions(context.Background(), &lnrpc.GetTransactionsRequest{})
	if err != nil {
		log.Criticalf("Failed to call SubscribeTransactions %v, %v", stream, err)
	}
	log.Infof("Wallet transactions subscription created")
	for {
		_, err := stream.Recv()
		log.Infof("watchOnChainState Wallet transactions subscription received new transaction")
		if err == io.EOF {
			log.Errorf("Failed to call SubscribeTransactions %v, %v", stream, err)
			return
		}
		if err != nil {
			log.Errorf("Failed to receive a transaction : %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
		}
		log.Infof("watchOnChainState sending account change notification")
		onAccountChanged()
		go ensureRoutingChannelOpened()
	}
}
