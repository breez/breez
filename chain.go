package breez

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/btcsuite/btcd/chaincfg"

	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/btcsuite/btcutil"
)

const (
	syncToChainSecondsInterval  = 3
	maxBtcFundingAmount         = 1<<24 - 1
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
SendNonDepositedCoins executes a request to send all the wallet coins to a particular address.
The wallet coins are considered as "non deposited" in oppoesed to the channel balance.
*/
func SendNonDepositedCoins(address string) error {
	walletBalance, err := lightningClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return err
	}
	transferAmount := walletBalance.ConfirmedBalance - estimateFee()
	if _, err := lightningClient.SendCoins(context.Background(), &lnrpc.SendCoinsRequest{Addr: address, Amount: transferAmount}); err != nil {
		return err
	}
	return nil
}

func syncToChain() {
	onAccountChanged()
	for {
		chainInfo, chainErr := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			log.Warnf("Failed get chain info", chainErr)
		} else {
			log.Infof("Sync to chain interval Synced=%v BlockHeight=%v", chainInfo.SyncedToChain, chainInfo.BlockHeight)
			if chainInfo.SyncedToChain {
				log.Infof("Synchronized to chain finshed BlockHeight=%v", chainInfo.BlockHeight)
				break
			}
		}
		time.Sleep(syncToChainSecondsInterval * time.Second)
	}
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
		}
		log.Infof("watchOnChainState sending account change notification")
		onAccountChanged()
	}
}

func estimateFee() int64 {
	estimator := &lnwallet.StaticFeeEstimator{FeePerKW: lnwallet.SatPerKWeight(12500)}
	satPerKWeight, err := estimator.EstimateFeePerKW(1) //one block confirmation
	if err != nil {
		log.Errorf("estimateFee: can't calculate fee", err)
		return fallbackFee
	}
	return int64(satPerKWeight.FeeForWeight(lnwallet.CommitWeight).ToUnit(btcutil.AmountSatoshi))
}
