package swapfunds

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/breez/boltz"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/walletrpc"
	"github.com/lightningnetwork/lnd/lntypes"
)

func (s *Service) claimReverseSwap(rs *data.ReverseSwap, rawTx []byte) error {
	lnClient := s.daemonAPI.APIClient()
	if lnClient == nil {
		s.log.Errorf("daemon is not ready")
		return fmt.Errorf("daemon is not ready")
	}
	info, err := lnClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		s.log.Errorf("lnClient.GetInfo: %v", err)
		return fmt.Errorf("lnClient.GetInfo: %w", err)
	}
	if rs.TimeoutBlockHeight <= int64(info.BlockHeight) {
		s.log.Errorf("too late for the claim transaction: TimeoutBlockHeight=%v <= BlockHeight=%v", rs.TimeoutBlockHeight, info.BlockHeight)
		return fmt.Errorf("too late for the claim transaction: TimeoutBlockHeight=%v <= BlockHeight=%v", rs.TimeoutBlockHeight, info.BlockHeight)
	}

	_, err = boltz.CheckTransaction(hex.EncodeToString(rawTx), rs.LockupAddress, rs.OnchainAmount)
	if err != nil {
		s.log.Errorf("boltz.CheckTransaction(%x, %v, %v): %v", rawTx, rs.LockupAddress, rs.OnchainAmount, err)
		return fmt.Errorf("boltz.CheckTransaction(%x, %v, %v): %w", rawTx, rs.LockupAddress, rs.OnchainAmount, err)
	}
	claimTx, err := boltz.ClaimTransaction(rs.Script, hex.EncodeToString(rawTx), rs.ClaimAddress, rs.Preimage, rs.Key, rs.ClaimFee)
	if err != nil {
		s.log.Errorf("boltz.ClaimTransaction(%v, %x, %v, %v, %v): %v", rawTx, rs.ClaimAddress, rs.Preimage, rs.Key, rs.ClaimFee, err)
		return fmt.Errorf("walletKitClient.EstimateFee(%v, %x, %v, %v, %v): %w", rawTx, rs.ClaimAddress, rs.Preimage, rs.Key, rs.ClaimFee, err)
	}
	txHex, err := hex.DecodeString(claimTx)
	if err != nil {
		s.log.Errorf("hex.DecodeString(%v): %v", claimTx, err)
		return fmt.Errorf("hex.DecodeString(%v): %w", claimTx, err)
	}
	tx, err := btcutil.NewTxFromBytes(txHex)
	if err != nil {
		s.log.Errorf("btcutil.NewTxFromBytes(%v): %v", txHex, err)
		return fmt.Errorf("btcutil.NewTxFromBytes(%v): %w", txHex, err)
	}
	var confRequest *chainrpc.ConfRequest
	txid := tx.Hash().CloneBytes()
	confRequest, err = s.breezDB.FetchUnconfirmedClaimTransaction()
	if err != nil || confRequest == nil || !bytes.Equal(confRequest.Txid, txid) {
		confRequest = &chainrpc.ConfRequest{
			NumConfs:   1,
			HeightHint: info.BlockHeight,
			Txid:       txid,
			Script:     tx.MsgTx().TxOut[0].PkScript,
		}
		err = s.breezDB.SaveUnconfirmedClaimTransaction(confRequest)
		if err != nil {
			s.log.Errorf("s.breezDB.SaveUnconfirmedClaimTransaction(%#v): %v", confRequest, err)
		}
	}
	err = s.subscribeClaimTransaction(confRequest)
	if err != nil {
		s.log.Errorf("s.subscribeClaimTransaction(%#v): %v", confRequest, err)
	}
	walletKitClient := s.daemonAPI.WalletKitClient()
	pr, err := walletKitClient.PublishTransaction(context.Background(), &walletrpc.Transaction{TxHex: txHex})
	if err != nil {
		s.log.Errorf("walletKitClient.PublishTransaction(%x): %v", txHex, err)
		return fmt.Errorf("walletKitClient.PublishTransaction(%x): %w", txHex, err)
	}
	if pr.PublishError != "" {
		s.log.Errorf("walletKitClient.PublishTransaction(%x): %v", txHex, pr.PublishError)
		return fmt.Errorf("walletKitClient.PublishTransaction(%x): %v", txHex, pr.PublishError)
	}
	return nil
}

func (s *Service) subscribeClaimTransaction(confRequest *chainrpc.ConfRequest) error {
	client := s.daemonAPI.ChainNotifierClient()
	ctx, cancel := context.WithCancel(context.Background())
	s.log.Infof("Registering claim transaction notification %v", confRequest.Txid)
	stream, err := client.RegisterConfirmationsNtfn(ctx, confRequest)
	if err != nil {
		s.log.Errorf("client.RegisterConfirmationsNtfn(%#vv): %v", confRequest, err)
		return fmt.Errorf("client.RegisterConfirmationsNtfn(%#v): %w", confRequest, err)
	}
	go func() {
		for {
			confEvent, err := stream.Recv()
			if err != nil {
				s.log.Criticalf("Failed to receive an event : %v", err)
				return
			}
			s.log.Infof("confEvent: %#v; rawTX:%x", confEvent.GetConf(), confEvent.GetConf().GetRawTx())
			s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_REVERSE_SWAP_CLAIM_CONFIRMED,
				Data: []string{hex.EncodeToString(confEvent.GetConf().RawTx)}})
			err = s.breezDB.SaveUnconfirmedClaimTransaction(nil)
		}
	}()
	go func() {
		<-s.quitChan
		s.log.Infof("Canceling subscription")
		cancel()
	}()
	return nil
}

func (s *Service) handleClaimTransaction() error {
	confRequest, err := s.breezDB.FetchUnconfirmedClaimTransaction()
	if err != nil {
		s.log.Errorf("s.breezDB.FetchUnconfirmedClaimTransaction(): %v", err)
		return fmt.Errorf("s.breezDB.FetchUnconfirmedClaimTransaction(): %w", err)
	}
	if confRequest == nil {
		return nil
	}
	err = s.subscribeClaimTransaction(confRequest)
	if err != nil {
		s.log.Errorf("s.subscribeClaimTransaction(%v): %v", confRequest, err)
		return fmt.Errorf("s.subscribeClaimTransaction(%v): %w", confRequest, err)
	}
	return nil
}

func (s *Service) ClaimFeeEstimates(claimAddress string) (map[int32]int64, error) {
	blockRange := []int32{2, 6, 24}
	walletKitClient := s.daemonAPI.WalletKitClient()
	fees := make(map[int32]int64)
	for _, b := range blockRange {
		f, err := walletKitClient.EstimateFee(context.Background(), &walletrpc.EstimateFeeRequest{ConfTarget: b})
		if err != nil {
			s.log.Errorf("walletKitClient.EstimateFee(%v): %v", b, err)
			return nil, fmt.Errorf("walletKitClient.EstimateFee(%v): %w", b, err)
		}
		fAmt, err := boltz.ClaimFee(claimAddress, f.SatPerKw)
		if err != nil {
			s.log.Errorf("boltz.ClaimFee(%v, %v): %v", claimAddress, f.SatPerKw, err)
			return nil, fmt.Errorf("boltz.ClaimFee(%v, %v): %w", claimAddress, f.SatPerKw, err)
		}
		fees[b] = fAmt
	}
	return fees, nil
}

func (s *Service) subscribeLockupScript(rs *data.ReverseSwap) error {
	a, err := btcutil.DecodeAddress(rs.LockupAddress, s.chainParams)
	if err != nil {
		return fmt.Errorf("btcutil.DecodeAddress(%v) %w", rs.LockupAddress, err)
	}
	script, err := txscript.PayToAddrScript(a)
	if err != nil {
		return fmt.Errorf("txscript.PayToAddrScript(%v) %w", a, err)
	}
	client := s.daemonAPI.ChainNotifierClient()
	ctx, cancel := context.WithCancel(context.Background())
	startHeight := uint32(rs.StartBlockHeight)
	s.log.Infof("Registering with start block = %v", startHeight)
	stream, err := client.RegisterConfirmationsNtfn(ctx, &chainrpc.ConfRequest{
		NumConfs:   1,
		HeightHint: startHeight,
		Txid:       lntypes.ZeroHash[:],
		Script:     script,
	})
	if err != nil {
		s.log.Errorf("client.RegisterConfirmationsNtfn(%v, %x): %v", startHeight, script, err)
		return fmt.Errorf("client.RegisterConfirmationsNtfn(%v, %x): %w", startHeight, script, err)
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
			err = s.claimReverseSwap(rs, confEvent.GetConf().GetRawTx())
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
	lnClient := s.daemonAPI.APIClient()
	if lnClient == nil {
		return "", errors.New("daemon is not ready")
	}
	info, err := lnClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return "", fmt.Errorf("lnClient.GetInfo: %w", err)
	}

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
		StartBlockHeight:   int64(info.BlockHeight),
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

func (s *Service) SetReverseSwapClaimFee(hash string, fee int64) error {
	rs, err := s.breezDB.FetchReverseSwap(hash)
	if err != nil {
		s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
		return fmt.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
	}
	rs.ClaimFee = fee
	_, err = s.breezDB.SaveReverseSwap(rs)
	if err != nil {
		return fmt.Errorf("breezDB.SaveReverseSwap(%#v): %w", rs, err)
	}
	return nil
}

func (s *Service) PayReverseSwap(hash, deviceID, title, body string) error {
	rs, err := s.breezDB.FetchReverseSwap(hash)
	if err != nil {
		s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
		return fmt.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
	}

	if rs.ClaimFee == 0 {
		s.log.Errorf("need to set claimFee for %v", hash)
		return fmt.Errorf("need to set claimFee for %v", hash)
	}

	err = s.sendPayment(rs.Invoice, rs.LnAmount)
	if err != nil {
		return fmt.Errorf("s.sendPayment: %v", err)
	}

	err = s.subscribeLockupScript(rs)
	if err != nil {
		return fmt.Errorf("s.subscribeLockupScript(%v): %w", rs, err)
	}
	return nil
}

func (s *Service) UnconfirmedReverseSwapClaimTransaction() (string, error) {
	confRequest, err := s.breezDB.FetchUnconfirmedClaimTransaction()
	if err != nil {
		s.log.Errorf("s.breezDB.FetchUnconfirmedClaimTransaction(): %v", err)
		return "", fmt.Errorf("s.breezDB.FetchUnconfirmedClaimTransaction(): %w", err)
	}
	if confRequest == nil {
		return "", nil
	}
	h, err := chainhash.NewHash(confRequest.Txid)
	if err != nil {
		return "", err
	}
	//tx, err :=  .NewTxFromBytes(confRequest.Txid)
	return h.String(), nil
}

func (s *Service) ReverseSwapPayments() (*data.ReverseSwapPaymentStatuses, error) {
	chanDB, chanDBCleanUp, err := channeldbservice.Get(s.cfg.WorkingDir)
	if err != nil {
		s.log.Errorf("channeldbservice.Get(%v): %v", s.cfg.WorkingDir, err)
		return nil, fmt.Errorf("channeldbservice.Get(%v): %w", s.cfg.WorkingDir, err)
	}
	defer chanDBCleanUp()
	paymentControl := channeldb.NewPaymentControl(chanDB)
	payments, err := paymentControl.FetchInFlightPayments()
	if err != nil {
		s.log.Errorf("paymentControl.FetchInFlightPayments(): %v", err)
		return nil, fmt.Errorf("paymentControl.FetchInFlightPayments(): %w", err)
	}
	s.log.Infof("Fetched %v in flight payments", len(payments))
	var statuses []*data.ReverseSwapPaymentStatus
	for _, p := range payments {
		hash := p.Info.PaymentHash.String()
		rs, err := s.breezDB.FetchReverseSwap(hash)
		if err != nil {
			s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
			return nil, fmt.Errorf("s.breezDB.FetchReverseSwap(%v): %v", hash, err)
		}
		if rs == nil {
			continue
		}
		s.log.Infof("gettting info about reverse swap: hash=%v, swap=%v", hash, rs)
		status, txid, _, eta, err := boltz.GetTransaction(rs.Id, rs.LockupAddress, rs.OnchainAmount)
		if err != nil {
			s.log.Errorf("boltz.GetTransaction(%v, %v, %v): %v", rs.Id, rs.LockupAddress, rs.OnchainAmount, err)
			return nil, fmt.Errorf("boltz.GetTransaction(%v, %v, %v): %w", rs.Id, rs.LockupAddress, rs.OnchainAmount, err)
		}
		s.log.Infof("Reverse swap: hash=%v, status=%v, txid=%v, eta=%v", hash, status, txid, eta)
		statuses = append(statuses, &data.ReverseSwapPaymentStatus{Hash: hash, Eta: int32(eta)})
	}
	return &data.ReverseSwapPaymentStatuses{PaymentsStatus: statuses}, nil
}

func (s *Service) handleReverseSwapsPayments() error {
	chanDB, chanDBCleanUp, err := channeldbservice.Get(s.cfg.WorkingDir)
	if err != nil {
		s.log.Errorf("channeldbservice.Get(%v): %v", s.cfg.WorkingDir, err)
		return fmt.Errorf("channeldbservice.Get(%v): %w", s.cfg.WorkingDir, err)
	}
	defer chanDBCleanUp()
	paymentControl := channeldb.NewPaymentControl(chanDB)
	payments, err := paymentControl.FetchInFlightPayments()
	if err != nil {
		s.log.Errorf("paymentControl.FetchInFlightPayments(): %v", err)
		return fmt.Errorf("paymentControl.FetchInFlightPayments(): %w", err)
	}
	s.log.Infof("Fetched %v in flight payments", len(payments))
	for _, p := range payments {
		hash := p.Info.PaymentHash.String()
		rs, err := s.breezDB.FetchReverseSwap(hash)
		if err != nil {
			s.log.Errorf("s.breezDB.FetchReverseSwap(%v): %w", hash, err)
			continue
		}
		if rs == nil {
			continue
		}
		s.log.Infof("handling reverse swap: hash=%v, swap=%v", hash, rs)
		err = s.subscribeLockupScript(rs)
		if err != nil {
			s.log.Errorf("s.subscribeLockupScript(%v): %v", rs, err)
		}
	}
	return nil
}
