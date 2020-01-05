package swapfunds

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/breez/boltz"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/walletrpc"
	"github.com/lightningnetwork/lnd/lntypes"
)

func (s *Service) claimReverseSwap(rs *data.ReverseSwap, rawTx []byte, confTarget int32) error {
	_, err := boltz.CheckTransaction(hex.EncodeToString(rawTx), rs.LockupAddress, rs.OnchainAmount)
	if err != nil {
		s.log.Errorf("boltz.CheckTransaction(%x, %v, %v): %v", rawTx, rs.LockupAddress, rs.OnchainAmount, err)
		return fmt.Errorf("boltz.CheckTransaction(%x, %v, %v): %w", rawTx, rs.LockupAddress, rs.OnchainAmount, err)
	}
	// TODO: Check that there is enought time to claim
	walletKitClient := s.daemonAPI.WalletKitClient()
	f, err := walletKitClient.EstimateFee(context.Background(), &walletrpc.EstimateFeeRequest{ConfTarget: confTarget})
	if err != nil {
		s.log.Errorf("walletKitClient.EstimateFee(%v): %v", confTarget, err)
		return fmt.Errorf("walletKitClient.EstimateFee(%v): %w", confTarget, err)
	}
	claimTx, err := boltz.ClaimTransaction(rs.Script, hex.EncodeToString(rawTx), rs.ClaimAddress, rs.Preimage, rs.Key, f.SatPerKw)
	if err != nil {
		s.log.Errorf("boltz.ClaimTransaction(%v, %x, %v, %v, %v): %v", rawTx, rs.ClaimAddress, rs.Preimage, rs.Key, f.SatPerKw, err)
		return fmt.Errorf("walletKitClient.EstimateFee(%v, %x, %v, %v, %v): %w", rawTx, rs.ClaimAddress, rs.Preimage, rs.Key, f.SatPerKw, err)
	}
	tx, err := hex.DecodeString(claimTx)
	if err != nil {
		s.log.Errorf("hex.DecodeString(%v): %v", claimTx, err)
		return fmt.Errorf("hex.DecodeString(%v): %w", claimTx, err)
	}
	pr, err := walletKitClient.PublishTransaction(context.Background(), &walletrpc.Transaction{TxHex: tx})
	if err != nil {
		s.log.Errorf("walletKitClient.PublishTransaction(%x): %v", tx, err)
		return fmt.Errorf("walletKitClient.PublishTransaction(%x): %w", tx, err)
	}
	if pr.PublishError != "" {
		s.log.Errorf("walletKitClient.PublishTransaction(%x): %v", tx, pr.PublishError)
		return fmt.Errorf("walletKitClient.PublishTransaction(%x): %v", tx, pr.PublishError)
	}
	return nil
}

func (s *Service) subscribeLockupScript(rs *data.ReverseSwap) error {
	lnClient := s.daemonAPI.APIClient()
	info, err := lnClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		s.log.Errorf("lnClient.GetInfo: %v", err)
		return fmt.Errorf("lnClient.GetInfo: %w", err)
	}

	a, err := btcutil.DecodeAddress(rs.LockupAddress, s.chainParams)
	if err != nil {
		return fmt.Errorf("btcutil.DecodeAddress(%v)", rs.LockupAddress, err)
	}
	script, err := txscript.PayToAddrScript(a)
	if err != nil {
		return fmt.Errorf("txscript.PayToAddrScript(%v)", a, err)
	}
	client := s.daemonAPI.ChainNotifierClient()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.RegisterConfirmationsNtfn(ctx, &chainrpc.ConfRequest{
		NumConfs:   1,
		HeightHint: info.BlockHeight,
		Txid:       lntypes.ZeroHash[:],
		Script:     script,
	})
	if err != nil {
		s.log.Errorf("client.RegisterConfirmationsNtfn(%v, %x): %v", info.BlockHeight, script, err)
		return fmt.Errorf("client.RegisterConfirmationsNtfn(%v, %x): %w", info.BlockHeight, script, err)
	}
	//fmt.Printf("Register: %#v\n", rs)
	go func() {
		for {
			confEvent, err := stream.Recv()
			if err != nil {
				s.log.Criticalf("Failed to receive an event : %v", err)
				s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_REVERSE_SWAP_CLAIM_FAILED, Data: []string{rs.Key, "internal error"}})
				return
			}
			s.log.Infof("confEvent: %#v; rawTX:%x", confEvent.GetConf(), confEvent.GetConf().GetRawTx())
			s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_REVERSE_SWAP_CLAIM_STARTED, Data: []string{rs.Key}})
			err = s.claimReverseSwap(rs, confEvent.GetConf().GetRawTx(), 6)
			if err != nil {
				s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_REVERSE_SWAP_CLAIM_FAILED, Data: []string{rs.Key, err.Error()}})
			} else {
				s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_REVERSE_SWAP_CLAIM_SUCCEEDED, Data: []string{rs.Key}})
			}
		}
	}()
	go func() {
		<-s.quitChan
		s.log.Infof("Canceling subscription")
		cancel()
	}()
	return nil
}

func (s *Service) NewReverseSwap(amt int64, claimAddress string) (string, error) {
	r, err := boltz.NewReverseSwap(btcutil.Amount(amt))
	if err != nil {
		s.log.Errorf("boltz.NewReverseSwap(%v): %v", amt, err)
		return "", fmt.Errorf("boltz.NewReverseSwap(%v): %w", amt, err)
	}
	_, err = btcutil.DecodeAddress(claimAddress, s.chainParams)
	if err != nil {
		s.log.Errorf("btcutil.DecodeAddress(%v): %v", claimAddress, err)
		return "", fmt.Errorf("btcutil.DecodeAddress(%v): %w", claimAddress, err)
	}
	rs := &data.ReverseSwap{
		Id:                 r.ID,
		Invoice:            r.Invoice,
		Script:             r.RedeemScript,
		LockupAddress:      r.LockupAddress,
		Preimage:           r.Preimage,
		Key:                r.Key,
		ClaimAddress:       claimAddress,
		LnAmount:           amt,
		OnchainAmount:      r.OnchainAmount,
		TimeoutBlockHeight: r.TimeoutBlockHeight,
	}
	h, err := s.breezDB.SaveReverseSwap(rs)
	if err != nil {
		return "", fmt.Errorf("breezDB.SaveReverseSwap(%#v): %w", rs, err)
	}
	return h, nil
}

func (s *Service) FetchReverseSwap(hash string) (*data.ReverseSwap, error) {
	return s.breezDB.FetchReverseSwap(hash)
}

func (s *Service) PayReverseSwap(hash string) error {
	rs, err := s.breezDB.FetchReverseSwap(hash)
	if err != nil {
		s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
		return fmt.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
	}

	go s.sendPayment(rs.Invoice, rs.LnAmount)

	err = s.subscribeLockupScript(rs)
	if err != nil {
		return fmt.Errorf("s.subscribeLockupScript(%v): %w", rs, err)
	}
	return nil
}

func (s *Service) handleReverseSwapsPayments() error {
	chanDB, chanDBCleanUp, err := channeldbservice.Get(s.cfg.WorkingDir)
	if err != nil {
		s.log.Errorf("channeldbservice.Get(%v): %v", s.cfg.WorkingDir, err)
		return fmt.Errorf("channeldbservice.Get(%v): %w", s.cfg.WorkingDir, err)
	}
	paymentControl := channeldb.NewPaymentControl(chanDB)
	payments, err := paymentControl.FetchInFlightPayments()
	if err != nil {
		s.log.Errorf("paymentControl.FetchInFlightPayments(): %v", err)
		chanDBCleanUp()
		return fmt.Errorf("paymentControl.FetchInFlightPayments(): %w", err)
	}
	for _, p := range payments {
		hash := p.Info.PaymentHash.String()
		rs, err := s.breezDB.FetchReverseSwap(hash)
		if err != nil {
			s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
			continue
		}
		err = s.subscribeLockupScript(rs)
		if err != nil {
			s.log.Errorf("s.subscribeLockupScript(%v): %v", rs, err)
		}
	}
	chanDBCleanUp()
	return nil
}
