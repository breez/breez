package swapfunds

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/sync/singleflight"

	breezservice "github.com/breez/breez/breez"
)

const (
	transferFundsRequest = "Bitcoin Transfer"
)

var (
	getPaymentGroup singleflight.Group
)

/*
AddFundsInit is responsible for topping up an existing channel
*/
func (s *Service) AddFundsInit(notificationToken string) (*data.AddFundInitReply, error) {
	accountID := s.daemonAPI.NodePubkey()
	if accountID == "" {
		return nil, fmt.Errorf("Account is not ready")
	}

	lnclient := s.daemonAPI.APIClient()
	swap, err := lnclient.SubSwapClientInit(context.Background(), &lnrpc.SubSwapClientInitRequest{})
	if err != nil {
		s.log.Criticalf("Failed to call SubSwapClientInit %v", err)
		return nil, err
	}

	c, ctx, cancel := s.breezAPI.NewFundManager()
	defer cancel()

	r, err := c.AddFundInit(ctx, &breezservice.AddFundInitRequest{NodeID: accountID, NotificationToken: notificationToken, Pubkey: swap.Pubkey, Hash: swap.Hash})
	if err != nil {
		s.log.Errorf("Error in AddFundInit: %v", err)
		return nil, err
	}

	s.log.Infof("AddFundInit response = %v", r)

	if r.ErrorMessage != "" {
		return &data.AddFundInitReply{MaxAllowedDeposit: r.MaxAllowedDeposit, ErrorMessage: r.ErrorMessage}, nil
	}

	client, err := lnclient.SubSwapClientWatch(context.Background(), &lnrpc.SubSwapClientWatchRequest{Preimage: swap.Preimage, Key: swap.Key, ServicePubkey: r.Pubkey, LockHeight: r.LockHeight})
	if err != nil {
		s.log.Criticalf("Failed to call SubSwapClientWatch %v", err)
		return nil, err
	}

	s.log.Infof("Finished watch: %v, %v", hex.EncodeToString(r.Pubkey), r.LockHeight)

	// Verify we are on the same page
	if client.Address != r.Address {
		return nil, errors.New("address mismatch")
	}

	swapInfo := &db.SwapAddressInfo{
		Address:     r.Address,
		PaymentHash: swap.Hash,
		Preimage:    swap.Preimage,
		PrivateKey:  swap.Key,
		PublicKey:   swap.Pubkey,
		Script:      client.Script,
	}
	s.log.Infof("Saving new swap info %v", swapInfo)
	s.breezDB.SaveSwapAddressInfo(swapInfo)

	// Create JSON with the script and our private key (in case user wants to do the refund by himself)
	type ScriptBackup struct {
		Script     string
		PrivateKey string
	}
	backup := ScriptBackup{Script: hex.EncodeToString(client.Script), PrivateKey: hex.EncodeToString(swap.Key)}
	jsonBytes, err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}
	s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_FUND_ADDRESS_CREATED})
	s.log.Infof("r.RequiredReserve = %v ", r.RequiredReserve)
	return &data.AddFundInitReply{
		Address:           r.Address,
		MaxAllowedDeposit: r.MaxAllowedDeposit,
		ErrorMessage:      r.ErrorMessage,
		BackupJson:        string(jsonBytes[:]),
		RequiredReserve:   r.RequiredReserve,
	}, nil
}

/*
GetFundStatus gets a notification token and does two things:
1. Register for notifications on all saved addresses
2. Fetch the current status for the saved addresses from the server
*/
func (s *Service) GetFundStatus(notificationToken string) (*data.FundStatusReply, error) {
	addresses, err := s.breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.PaidAmount == 0
	})
	if err != nil {
		return nil, err
	}
	s.log.Infof("GetFundStatus len = %v", len(addresses))
	if len(addresses) == 0 {
		return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
	}

	var confirmedAddresses, transferErrors, unConfirmedAddresses []string
	var hasMempool bool
	for _, a := range addresses {

		//log.Infof("GetFundStatus paid=%v confirmed=%v lockHeight=%v mempool=%v address=%v refundTX=%v", a.PaidAmount, a.ConfirmedAmount, a.LockHeight, a.EnteredMempool, a.Address, a.LastRefundTxID)
		if len(a.ConfirmedTransactionIds) > 0 || a.LockHeight > 0 || a.LastRefundTxID != "" {
			if a.ErrorMessage != "" {
				transferErrors = append(transferErrors, a.ErrorMessage)
				s.log.Infof("GetFundStatus adding transfer error for address %v", a.Address)
			} else {
				confirmedAddresses = append(confirmedAddresses, a.Address)
				s.log.Infof("GetFundStatus adding confirmed transaction for address %v", a.Address)
			}
		} else {
			hasMempool = hasMempool || a.EnteredMempool
			unConfirmedAddresses = append(unConfirmedAddresses, a.Address)
		}
	}

	if hasMempool {
		s.log.Infof("GetFundStatus return status 'waiting confirmation'")
		return &data.FundStatusReply{Status: data.FundStatusReply_WAITING_CONFIRMATION}, nil
	}

	s.log.Infof("GetFundStatus checknig unConfirmedAddresses len=%v", len(unConfirmedAddresses))
	if len(unConfirmedAddresses) > 0 {
		c, ctx, cancel := s.breezAPI.NewFundManager()
		defer cancel()

		statusesMap, err := c.AddFundStatus(ctx, &breezservice.AddFundStatusRequest{NotificationToken: notificationToken, Addresses: unConfirmedAddresses})
		if err != nil {
			return nil, err
		}

		var hasUnconfirmed bool
		for addr, status := range statusesMap.Statuses {
			s.log.Infof("GetFundStatus - got status for address %v", status)
			if !status.Confirmed && status.Tx != "" {
				hasUnconfirmed = true
				s.breezDB.UpdateSwapAddress(addr, func(swapInfo *db.SwapAddressInfo) error {
					swapInfo.EnteredMempool = true
					return nil
				})
			}
		}
		if hasUnconfirmed {
			s.log.Infof("GetFundStatus return status 'waiting confirmation")
			return &data.FundStatusReply{Status: data.FundStatusReply_WAITING_CONFIRMATION}, nil
		}
	}

	if len(confirmedAddresses) > 0 {
		s.log.Infof("GetFundStatus return status 'confirmed'")
		return &data.FundStatusReply{Status: data.FundStatusReply_CONFIRMED}, nil
	}

	if len(transferErrors) > 0 {
		return &data.FundStatusReply{
			Status:       data.FundStatusReply_TRANSFER_ERROR,
			ErrorMessage: transferErrors[0],
		}, nil
	}

	s.log.Infof("GetFundStatus return status 'no funds")
	return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
}

//GetRefundableAddresses returns all addresses that are refundable, e.g: expired and not paid
func (s *Service) GetRefundableAddresses() ([]*db.SwapAddressInfo, error) {
	lnclient := s.daemonAPI.APIClient()
	info, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	refundable, err := s.breezDB.FetchSwapAddresses(func(a *db.SwapAddressInfo) bool {
		refundable := a.LockHeight < info.BlockHeight && a.ConfirmedAmount > 0
		if refundable {
			s.log.Infof("found refundable address: %v lockHeight=%v, amount=%v, currentHeight=%v", a.Address, a.LockHeight, a.ConfirmedAmount, info.BlockHeight)
		}
		return refundable
	})

	if err != nil {
		return nil, err
	}
	return refundable, nil
}

//Refund broadcast a refund transaction for a sub swap address.
func (s *Service) Refund(address, refundAddress string) (string, error) {
	lnclient := s.daemonAPI.APIClient()
	res, err := lnclient.SubSwapClientRefund(context.Background(), &lnrpc.SubSwapClientRefundRequest{
		Address:       address,
		RefundAddress: refundAddress,
	})
	if err != nil {
		return "", err
	}
	_, err = s.breezDB.UpdateSwapAddress(address, func(a *db.SwapAddressInfo) error {
		a.LastRefundTxID = res.Txid
		return nil
	})
	if err != nil {
		return "", err
	}
	s.onUnspentChanged()
	return res.Txid, nil
}

func (s *Service) onDaemonReady() error {
	//then initiate an update for all swap addresses in the db
	addresses, err := s.breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.PaidAmount == 0
	})
	s.log.Infof("watchSettledSwapAddresses got these addresses to check: %v", addresses)
	if err != nil {
		s.log.Errorf("failed to call fetchSwapAddresses %v", err)
		return err
	}

	lnclient := s.daemonAPI.APIClient()
	for _, a := range addresses {
		invoice, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: a.PaymentHash})
		if err != nil {
			s.log.Errorf("failed to lookup invoice, %v", err)
			continue
		}
		if err := s.onInvoice(invoice); err != nil {
			return err
		}
	}

	//then initiate an update for all swap addresses in the db
	addresses, err = s.breezDB.FetchAllSwapAddresses()
	s.log.Infof("watchSwapAddressConfirmations got these addresses to check: %v", addresses)
	if err != nil {
		s.log.Errorf("failed to call fetchSwapAddresses %v", err)
		return err
	}

	for _, a := range addresses {
		_, err = s.updateUnspentAmount(a.Address)
		if err != nil {
			s.log.Errorf("Failed to update unspent output for address %v", a.Address)
			return err
		}
	}
	return nil
}

//onTransaction subscribe to cofirmed transaction notifications in order
//to update the status of changed SwapAddressInfo in the db.
//On every notification if a new confirmation was detected it calls getPaymentsForConfirmedTransactions
//In order to calim the payments from the swap service.
func (s *Service) onTransaction() error {
	addresses, err := s.breezDB.FetchAllSwapAddresses()
	if err != nil {
		s.log.Errorf("watchSwapAddressConfirmations - Failed to call fetchAllSwapAddresses %v", err)
		return nil
	}
	s.log.Infof("watchSwapAddressConfirmations updating swap addresses")
	var newConfirmation bool
	for _, addr := range addresses {
		updated, err := s.updateUnspentAmount(addr.Address)
		if err != nil {
			s.log.Criticalf("Unable to call updateUnspentAmount for address %v", addr.Address)
		}
		newConfirmation = newConfirmation || updated
	}

	//if we got new confirmation we will raise change event.
	if newConfirmation {
		s.onUnspentChanged()

		// if we are connected to the routing node, let's redeem our payment.
		go s.getPaymentsForConfirmedTransactions()
	}
	return nil
}

func (s *Service) onInvoice(invoice *lnrpc.Invoice) error {
	if invoice.Settled {
		s.log.Infof("watchSettledSwapAddresses - removing paid SwapAddressInfo")
		_, err := s.breezDB.UpdateSwapAddressByPaymentHash(invoice.RHash, func(addressInfo *db.SwapAddressInfo) error {
			addressInfo.PaidAmount = invoice.AmtPaidSat
			return nil
		})
		if err != nil {
			s.log.Criticalf("watchSettledSwapAddresses - failed to call updateSwapAddressByPaymentHash : %v", err)
			return err
		}
	}
	return nil
}

func (s *Service) lightningTransfersReady() bool {
	return s.daemonAPI.ConnectedToRoutingNode() && s.daemonAPI.HasChannelWithRoutingNode()
}

//SettlePendingTransfers watch for routing peer connection and once connected it does two things:
//1. Ask the breez server to pay in lightning for addresses that the user has sent funds to and
//   that the funds are confirmred
//2. Ask the breez server to pay on-chain for funds were sent to him in lightning as part of the
//   remove funds flow
func (s *Service) SettlePendingTransfers() {
	go s.getPaymentsForConfirmedTransactions()
	go s.redeemAllRemovedFunds()
}

func (s *Service) updateUnspentAmount(address string) (bool, error) {
	lnclient := s.daemonAPI.APIClient()
	return s.breezDB.UpdateSwapAddress(address, func(swapInfo *db.SwapAddressInfo) error {
		unspentResponse, err := lnclient.UnspentAmount(context.Background(), &lnrpc.UnspentAmountRequest{Address: address})
		if err != nil {
			return err
		}

		swapInfo.ConfirmedAmount = unspentResponse.Amount //get unsepnt amount
		if len(unspentResponse.Utxos) > 0 {
			s.log.Infof("Updating unspent amount %v for address %v", unspentResponse.Amount, address)
			swapInfo.LockHeight = uint32(unspentResponse.LockHeight + unspentResponse.Utxos[0].BlockHeight)
		}

		var confirmedTransactionIDs []string
		for _, tx := range unspentResponse.Utxos {
			confirmedTransactionIDs = append(confirmedTransactionIDs, tx.Txid)
		}
		swapInfo.ConfirmedTransactionIds = confirmedTransactionIDs
		return nil
	})
}

func (s *Service) getPaymentsForConfirmedTransactions() {
	s.log.Infof("getPaymentsForConfirmedTransactions: asking for pending payments")

	if !s.lightningTransfersReady() {
		s.log.Infof("Skipping getPaymentsForConfirmedTransactions connected=%v, hasChannel=%v",
			s.daemonAPI.ConnectedToRoutingNode(), s.daemonAPI.HasChannelWithRoutingNode())
		return
	}

	confirmedAddresses, err := s.breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.ConfirmedAmount > 0 && addr.PaidAmount == 0
	})
	if err != nil {
		s.log.Errorf("getPaymentsForConfirmedTransactions: failed to fetch swap addresses %v", err)
		return
	}
	s.log.Infof("getPaymentsForConfirmedTransactions: confirmedAddresses length = %v", len(confirmedAddresses))
	for _, address := range confirmedAddresses {
		getPaymentGroup.Do(fmt.Sprintf("getPayment - %v", address), func() (interface{}, error) {
			s.retryGetPayment(address, 3)
			s.onUnspentChanged()
			return nil, nil
		})
	}
}

func (s *Service) retryGetPayment(addressInfo *db.SwapAddressInfo, retries int) {
	for i := 0; i < retries; i++ {
		waitDuration := 5 * time.Second
		deadlineExceeded, err := s.getPayment(addressInfo)
		if err == nil {
			s.log.Infof("succeed to get payment for address %v", addressInfo.Address)
			break
		}
		s.log.Errorf("retryGetPayment - error getting payment in attempt=%v %v", i, err)
		if deadlineExceeded {
			waitDuration = 30 * time.Second
		}
		time.Sleep(waitDuration)
	}
}

func (s *Service) getPayment(addressInfo *db.SwapAddressInfo) (bool, error) {
	//first lookup for an existing invoice
	var paymentRequest string
	lnclient := s.daemonAPI.APIClient()
	invoice, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: addressInfo.PaymentHash})
	if invoice != nil {
		if invoice.Value != addressInfo.ConfirmedAmount {
			errorMsg := "Money was added after the invoice was created"
			_, err = s.breezDB.UpdateSwapAddress(addressInfo.Address, func(a *db.SwapAddressInfo) error {
				a.ErrorMessage = errorMsg
				return nil
			})
			return false, errors.New(errorMsg)
		}
		paymentRequest = invoice.PaymentRequest
	} else {
		addInvoice, err := lnclient.AddInvoice(context.Background(), &lnrpc.Invoice{RPreimage: addressInfo.Preimage, Value: addressInfo.ConfirmedAmount, Memo: transferFundsRequest, Private: true, Expiry: 60 * 60 * 24 * 30})
		if err != nil {
			return false, fmt.Errorf("failed to call AddInvoice, err = %v", err)
		}
		paymentRequest = addInvoice.PaymentRequest
	}

	c, ctx, cancel := s.breezAPI.NewFundManager()
	defer cancel()
	var paymentError string
	reply, err := c.GetSwapPayment(ctx, &breezservice.GetSwapPaymentRequest{PaymentRequest: paymentRequest})
	deadlineExceeded := ctx.Err() == context.DeadlineExceeded
	if err != nil {
		paymentError = err.Error()
	} else if reply.PaymentError != "" {
		paymentError = reply.PaymentError
	}
	if paymentError != "" {
		s.breezDB.UpdateSwapAddress(addressInfo.Address, func(a *db.SwapAddressInfo) error {
			a.ErrorMessage = paymentError
			return nil
		})
		return deadlineExceeded, fmt.Errorf("failed to get payment for address %v, err = %v", addressInfo.Address, paymentError)
	}
	return deadlineExceeded, nil
}

func (s *Service) onUnspentChanged() {
	s.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_FUND_ADDRESS_UNSPENT_CHANGED})
}
