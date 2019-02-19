package breez

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/golang/protobuf/proto"
	"golang.org/x/sync/singleflight"

	breezservice "github.com/breez/breez/breez"
)

var (
	getPaymentGroup singleflight.Group
)

/*
AddFundsInit is responsible for topping up an existing channel
*/
func AddFundsInit(notificationToken string) (*data.AddFundInitReply, error) {
	acc, err := calculateAccount()
	if err != nil {
		log.Errorf("Error in calculateAccount: %v", err)
		return nil, err
	}

	swap, err := lightningClient.SubSwapClientInit(context.Background(), &lnrpc.SubSwapClientInitRequest{})
	if err != nil {
		log.Criticalf("Failed to call SubSwapClientInit %v", err)
		return nil, err
	}

	c, ctx, cancel := getFundManager()
	defer cancel()

	r, err := c.AddFundInit(ctx, &breezservice.AddFundInitRequest{NodeID: acc.Id, NotificationToken: notificationToken, Pubkey: swap.Pubkey, Hash: swap.Hash})
	if err != nil {
		log.Errorf("Error in AddFundInit: %v", err)
		return nil, err
	}

	log.Infof("AddFundInit response = %v", r)

	if r.ErrorMessage != "" {
		return &data.AddFundInitReply{MaxAllowedDeposit: r.MaxAllowedDeposit, ErrorMessage: r.ErrorMessage}, nil
	}

	client, err := lightningClient.SubSwapClientWatch(context.Background(), &lnrpc.SubSwapClientWatchRequest{Preimage: swap.Preimage, Key: swap.Key, ServicePubkey: r.Pubkey, LockHeight: r.LockHeight})
	if err != nil {
		log.Criticalf("Failed to call SubSwapClientWatch %v", err)
		return nil, err
	}

	log.Infof("Finished watch: %v, %v", hex.EncodeToString(r.Pubkey), r.LockHeight)

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
	log.Infof("Saving new swap info %v", swapInfo)
	breezDB.SaveSwapAddressInfo(swapInfo)

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

	backupManager.RequestBackup()
	return &data.AddFundInitReply{Address: r.Address, MaxAllowedDeposit: r.MaxAllowedDeposit, ErrorMessage: r.ErrorMessage, BackupJson: string(jsonBytes[:])}, nil
}

//GetRefundableAddresses returns all addresses that are refundable, e.g: expired and not paid
func GetRefundableAddresses() ([]*db.SwapAddressInfo, error) {
	info, err := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	refundable, err := breezDB.FetchSwapAddresses(func(a *db.SwapAddressInfo) bool {
		refundable := a.LockHeight < info.BlockHeight && a.ConfirmedAmount > 0
		if refundable {
			log.Infof("found refundable address: %v lockHeight=%v, amount=%v, currentHeight=%v", a.Address, a.LockHeight, a.ConfirmedAmount, info.BlockHeight)
		}
		return refundable
	})

	if err != nil {
		return nil, err
	}
	return refundable, nil
}

//Refund broadcast a refund transaction for a sub swap address.
func Refund(address, refundAddress string) (string, error) {
	res, err := lightningClient.SubSwapClientRefund(context.Background(), &lnrpc.SubSwapClientRefundRequest{
		Address:       address,
		RefundAddress: refundAddress,
	})
	if err != nil {
		return "", err
	}
	_, err = breezDB.UpdateSwapAddress(address, func(a *db.SwapAddressInfo) error {
		a.LastRefundTxID = res.Txid
		return nil
	})
	if err != nil {
		return "", err
	}
	onUnspentChanged()
	return res.Txid, nil
}

/*
RemoveFund transfers the user funds from the chanel to a supplied on-chain address
It is executed in three steps:
1. Send the breez server an address and an amount and get a corresponding payment request
2. Pay the payment request.
3. Redeem the removed funds from the server
*/
func RemoveFund(amount int64, address string) (*data.RemoveFundReply, error) {
	c, ctx, cancel := getFundManager()
	defer cancel()
	reply, err := c.RemoveFund(ctx, &breezservice.RemoveFundRequest{Address: address, Amount: amount})
	if err != nil {
		log.Errorf("RemoveFund: server endpoint call failed: %v", err)
		return nil, err
	}
	if reply.ErrorMessage != "" {
		return &data.RemoveFundReply{ErrorMessage: reply.ErrorMessage}, nil
	}

	log.Infof("RemoveFunds: got payment request: %v", reply.PaymentRequest)
	payreq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: reply.PaymentRequest})
	if err != nil {
		log.Errorf("DecodePayReq of server response failed: %v", err)
		return nil, err
	}

	//mark this payment request as redeemable
	breezDB.AddRedeemablePaymentHash(payreq.PaymentHash)

	log.Infof("RemoveFunds: Sending payment...")
	err = SendPaymentForRequest(reply.PaymentRequest, 0)
	if err != nil {
		log.Errorf("SendPaymentForRequest failed: %v", err)
		return nil, err
	}
	log.Infof("SendPaymentForRequest finished successfully")
	txID, err := redeemRemovedFundsForHash(payreq.PaymentHash)
	if err != nil {
		log.Errorf("RedeemRemovedFunds failed: %v", err)
		return nil, err
	}
	log.Infof("RemoveFunds finished successfully")
	return &data.RemoveFundReply{ErrorMessage: "", Txid: txID}, err
}

func redeemAllRemovedFunds() error {
	log.Infof("redeemAllRemovedFunds")
	hashes, err := breezDB.FetchRedeemablePaymentHashes()
	if err != nil {
		log.Errorf("failed to fetchRedeemablePaymentHashes, %v", err)
		return err
	}
	for _, hash := range hashes {
		log.Infof("Redeeming transaction for has %v", hash)
		txID, err := redeemRemovedFundsForHash(hash)
		if err != nil {
			log.Errorf("failed to redeem funds for hash %v, %v", hash, err)
		} else {
			log.Infof("successfully redeemed funds for hash %v, txid=%v", hash, txID)
		}
	}
	return err
}

func redeemRemovedFundsForHash(hash string) (string, error) {
	fundManager, ctx, cancel := getFundManager()
	defer cancel()
	redeemReply, err := fundManager.RedeemRemovedFunds(ctx, &breezservice.RedeemRemovedFundsRequest{Paymenthash: hash})
	if err != nil {
		log.Errorf("RedeemRemovedFunds failed for hash: %v,   %v", hash, err)
		return "", err
	}
	return redeemReply.Txid, breezDB.UpdateRedeemTxForPayment(hash, redeemReply.Txid)
}

func getFundManager() (breezservice.FundManagerClient, context.Context, context.CancelFunc) {
	con := getBreezClientConnection()
	log.Infof("getFundManager - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewFundManagerClient(con), ctx, cancel
}

/*
GetFundStatus gets a notification token and does two things:
1. Register for notifications on all saved addresses
2. Fetch the current status for the saved addresses from the server
*/
func GetFundStatus(notificationToken string) (*data.FundStatusReply, error) {
	addresses, err := breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.PaidAmount == 0
	})
	if err != nil {
		return nil, err
	}
	log.Infof("GetFundStatus len = %v", len(addresses))
	if len(addresses) == 0 {
		return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
	}

	var confirmedAddresses, unConfirmedAddresses []string
	var hasMempool bool
	for _, a := range addresses {

		//log.Infof("GetFundStatus paid=%v confirmed=%v lockHeight=%v mempool=%v address=%v refundTX=%v", a.PaidAmount, a.ConfirmedAmount, a.LockHeight, a.EnteredMempool, a.Address, a.LastRefundTxID)
		if len(a.ConfirmedTransactionIds) > 0 || a.LockHeight > 0 || a.LastRefundTxID != "" {
			confirmedAddresses = append(confirmedAddresses, a.Address)
			log.Infof("GetFundStatus adding confirmed transaction for address %v", a.Address)
		} else {
			hasMempool = hasMempool || a.EnteredMempool
			unConfirmedAddresses = append(unConfirmedAddresses, a.Address)
		}
	}

	if hasMempool {
		log.Infof("GetFundStatus return status 'waiting confirmation'")
		return &data.FundStatusReply{Status: data.FundStatusReply_WAITING_CONFIRMATION}, nil
	}

	log.Infof("GetFundStatus checknig unConfirmedAddresses len=%v", len(unConfirmedAddresses))
	if len(unConfirmedAddresses) > 0 {
		c, ctx, cancel := getFundManager()
		defer cancel()

		statusesMap, err := c.AddFundStatus(ctx, &breezservice.AddFundStatusRequest{NotificationToken: notificationToken, Addresses: unConfirmedAddresses})
		if err != nil {
			return nil, err
		}

		var hasUnconfirmed bool
		for addr, status := range statusesMap.Statuses {
			log.Infof("GetFundStatus - got status for address %v", status)
			if !status.Confirmed && status.Tx != "" {
				hasUnconfirmed = true
				breezDB.UpdateSwapAddress(addr, func(swapInfo *db.SwapAddressInfo) error {
					swapInfo.EnteredMempool = true
					return nil
				})
			}
		}
		if hasUnconfirmed {
			log.Infof("GetFundStatus return status 'waiting confirmation")
			return &data.FundStatusReply{Status: data.FundStatusReply_WAITING_CONFIRMATION}, nil
		}
	}

	if len(confirmedAddresses) > 0 {
		log.Infof("GetFundStatus return status 'confirmed'")
		return &data.FundStatusReply{Status: data.FundStatusReply_CONFIRMED}, nil
	}

	log.Infof("GetFundStatus return status 'no funds")
	return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
}

func watchFundTransfers() {
	go watchSettledSwapAddresses()
	go watchSettlePendingTransfers()
	go watchSwapAddressConfirmations()
}

//watchSwapAddressConfirmations subscribe to cofirmed transaction notifications in order
//to update the status of changed SwapAddressInfo in the db.
//On every notification if a new confirmation was detected it calls getPaymentsForConfirmedTransactions
//In order to calim the payments from the swap service.
func watchSwapAddressConfirmations() {

	//first of all subscribe to transaction so we won't loose any transaction on startup
	stream, err := lightningClient.SubscribeTransactions(context.Background(), &lnrpc.GetTransactionsRequest{})
	if err != nil {
		log.Errorf("watchSwapAddressConfirmations - Failed to call SubscribeTransactions %v, %v", stream, err)
		return
	}

	//then initiate an update for all swap addresses in the db
	addresses, err := breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return true
	})
	log.Infof("watchSwapAddressConfirmations got these addresses to check: %v", addresses)
	if err != nil {
		log.Errorf("failed to call fetchSwapAddresses %v", err)
		return
	}

	for _, a := range addresses {
		_, err = updateUnspentAmount(a.Address)
		if err != nil {
			log.Errorf("Failed to update unspent output for address %v", a.Address)
		}
	}

	//Now enter the loopp of updating on each confirmed transaction
	for {
		_, err := stream.Recv()
		log.Infof("watchSwapAddressConfirmations - transactions subscription received new transaction")
		if err != nil {
			log.Errorf("watchSwapAddressConfirmations - Failed to call SubscribeTransactions %v, %v", stream, err)
			return
		}
		addresses, err := breezDB.FetchAllSwapAddresses()
		if err != nil {
			log.Errorf("watchSwapAddressConfirmations - Failed to call fetchAllSwapAddresses %v", err)
			return
		}
		log.Infof("watchSwapAddressConfirmations updating swap addresses")
		var newConfirmation bool
		for _, addr := range addresses {
			updated, err := updateUnspentAmount(addr.Address)
			if err != nil {
				log.Criticalf("Unable to call updateUnspentAmount for address %v", addr.Address)
			}
			newConfirmation = newConfirmation || updated
		}

		//if we got new confirmation, let's try to get payments
		if newConfirmation {
			onUnspentChanged()
			go getPaymentsForConfirmedTransactions()
		}
	}
}

func updateUnspentAmount(address string) (bool, error) {
	return breezDB.UpdateSwapAddress(address, func(swapInfo *db.SwapAddressInfo) error {
		unspentResponse, err := lightningClient.UnspentAmount(context.Background(), &lnrpc.UnspentAmountRequest{Address: address})
		if err != nil {
			return err
		}

		swapInfo.ConfirmedAmount = unspentResponse.Amount //get unsepnt amount
		if len(unspentResponse.Utxos) > 0 {
			log.Infof("Updating unspent amount %v for address %v", unspentResponse.Amount, address)
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

//watchSettledSwapAddresses watch for settled invoices and for each invoice update
//the corresponding swap address with the LN paid amount.
func watchSettledSwapAddresses() {
	stream, err := lightningClient.SubscribeInvoices(context.Background(), &lnrpc.InvoiceSubscription{})
	if err != nil {
		log.Criticalf("watchSettledSwapAddresses failed to call SubscribeInvoices %v, %v", stream, err)
	}

	//then initiate an update for all swap addresses in the db
	addresses, err := breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.PaidAmount == 0
	})
	log.Infof("watchSettledSwapAddresses got these addresses to check: %v", addresses)
	if err != nil {
		log.Errorf("failed to call fetchSwapAddresses %v", err)
		return
	}

	for _, a := range addresses {
		invoice, err := lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: a.PaymentHash})
		if err != nil {
			log.Errorf("failed to lookup invoice, %v", err)
			continue
		}
		if invoice != nil && invoice.Settled {
			_, err := breezDB.UpdateSwapAddress(a.Address, func(a *db.SwapAddressInfo) error {
				a.PaidAmount = invoice.AmtPaidSat
				return nil
			})
			if err != nil {
				log.Errorf("Failed to update paid amount for address %v", a.Address)
			}
		}
	}

	//Now enter the loop of detecting each paid invoice and upate the corresponding
	//swap address info
	for {
		invoice, err := stream.Recv()
		log.Infof("watchSettledSwapAddresses - Invoice received by subscription")
		if err != nil {
			log.Criticalf("watchSettledSwapAddresses - failed to receive an invoice : %v", err)
			return
		}
		if invoice.Settled {
			log.Infof("watchSettledSwapAddresses - removing paid SwapAddressInfo")
			_, err := breezDB.UpdateSwapAddressByPaymentHash(invoice.RHash, func(addressInfo *db.SwapAddressInfo) error {
				addressInfo.PaidAmount = invoice.AmtPaidSat
				return nil
			})
			if err != nil {
				log.Criticalf("watchSettledSwapAddresses - failed to call updateSwapAddressByPaymentHash : %v", err)
				return
			}
		}
	}
}

//settlePendingTransfers watch for routing peer connection and once connected it does two things:
//1. Ask the breez server to pay in lightning for addresses that the user has sent funds to and
//   that the funds are confirmred
//2. Ask the breez server to pay on-chain for funds were sent to him in lightning as part of the
//   remove funds flow
func watchSettlePendingTransfers() error {
	log.Infof("askForIncomingTransfers started")
	subscription, err := lightningClient.SubscribePeers(context.Background(), &lnrpc.PeerSubscription{})
	if err != nil {
		log.Errorf("askForIncomingTransfers - Failed to subscribe peers %v", err)
		return err
	}
	for {
		notification, err := subscription.Recv()
		if err == io.EOF {
			return err
		}
		if err != nil {
			log.Errorf("askForIncomingTransfers - subscribe peers Failed to get notification %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
			continue
		}

		if notification.PubKey == cfg.RoutingNodePubKey && notification.Connected {
			settlePendingTransfers()
		}
	}
}

func settlePendingTransfers() {
	go getPaymentsForConfirmedTransactions()
	go redeemAllRemovedFunds()
}

func getPaymentsForConfirmedTransactions() {
	log.Infof("getPaymentsForConfirmedTransactions: asking for pending payments")
	confirmedAddresses, err := breezDB.FetchSwapAddresses(func(addr *db.SwapAddressInfo) bool {
		return addr.ConfirmedAmount > 0 && addr.PaidAmount == 0
	})
	if err != nil {
		log.Errorf("getPaymentsForConfirmedTransactions: failed to fetch swap addresses %v", err)
		return
	}
	log.Infof("getPaymentsForConfirmedTransactions: confirmedAddresses length = %v", len(confirmedAddresses))
	for _, address := range confirmedAddresses {
		getPaymentGroup.Do(fmt.Sprintf("getPayment - %v", address), func() (interface{}, error) {
			retryGetPayment(address, 3)
			return nil, nil
		})
	}
}

func retryGetPayment(addressInfo *db.SwapAddressInfo, retries int) {
	for i := 0; i < retries; i++ {
		err := getPayment(addressInfo)
		if err == nil {
			log.Infof("succeed to get payment for address %v", addressInfo.Address)
			break
		}
		log.Errorf("retryGetPayment - error getting payment in attempt=%v %v", i, err)
		time.Sleep(2 * time.Second)
	}
}

func getPayment(addressInfo *db.SwapAddressInfo) error {
	invoiceData := &data.InvoiceMemo{TransferRequest: true}
	memo, err := proto.Marshal(invoiceData)
	if err != nil {
		return fmt.Errorf("failed to marshal invoice data, err = %v", err)
	}
	//first lookup for an existing invoice
	var paymentRequest string
	invoice, err := lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: addressInfo.PaymentHash})
	if invoice != nil {
		if invoice.Value != addressInfo.ConfirmedAmount {
			errorMsg := "Money was added after the invoice was created"
			_, err = breezDB.UpdateSwapAddress(addressInfo.Address, func(a *db.SwapAddressInfo) error {
				a.ErrorMessage = errorMsg
				return nil
			})
			return errors.New(errorMsg)
		}
		paymentRequest = invoice.PaymentRequest
	} else {
		addInvoice, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{RPreimage: addressInfo.Preimage, Value: addressInfo.ConfirmedAmount, Memo: string(memo), Private: true, Expiry: 60 * 60 * 24 * 30})
		if err != nil {
			return fmt.Errorf("failed to call AddInvoice, err = %v", err)
		}
		paymentRequest = addInvoice.PaymentRequest
	}

	c, ctx, cancel := getFundManager()
	defer cancel()
	var paymentError string
	reply, err := c.GetSwapPayment(ctx, &breezservice.GetSwapPaymentRequest{PaymentRequest: paymentRequest})
	if err != nil {
		paymentError = err.Error()
	} else if reply.PaymentError != "" {
		paymentError = reply.PaymentError
	}
	if paymentError != "" {
		breezDB.UpdateSwapAddress(addressInfo.Address, func(a *db.SwapAddressInfo) error {
			a.ErrorMessage = paymentError
			return nil
		})
		return fmt.Errorf("failed to get payment for address %v, err = %v", addressInfo.Address, paymentError)
	}
	return nil
}

func onUnspentChanged() {
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_FUND_ADDRESS_UNSPENT_CHANGED}
}
