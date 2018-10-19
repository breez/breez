package breez

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/golang/protobuf/proto"
	"io"
	"time"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/submarinelib"
)

type swapAddressInfo struct {
	Address              string
	PaymentHash          []byte
	PaymentPreimage      []byte
	OurPubKey            []byte
	OurPrivateKey        []byte
	PayeePubKey          []byte
	SerializedScript     []byte
	Transaction          string
	TransactionConfirmed bool
	Paid                 bool
}

func serializeSwapAddressInfo(s *swapAddressInfo) ([]byte, error) {
	return json.Marshal(s)
}

func deserializeSwapAddressInfo(addressBytes []byte) (*swapAddressInfo, error) {
	var addressInfo swapAddressInfo
	err := json.Unmarshal(addressBytes, &addressInfo)
	return &addressInfo, err
}

/*
AddFunds is responsible for topping up an existing channel via a submarine swap
*/
func AddFunds(notificationToken string) (*data.AddFundReply, error) {
	// Generate two secrets
	lightningPaymentHash, lightningPreimage := submarinelib.GenSecret()
	chainPublicKey, chainPrivateKey, err := submarinelib.GenPublicPrivateKeypair()

	// Send them to the server along with the notification token
	c := breezservice.NewFundManagerClient(breezClientConnection)
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()

	r, err := c.AddFund(ctx, &breezservice.AddFundRequest{
		NotificationToken:  notificationToken,
		PaymentHash:        lightningPaymentHash,
		ChainPublicKey:     chainPublicKey,
		LightningNodeId:    lightningPubKey})
	if err != nil {
		log.Errorf("Error in AddFund: %v", err)
		return nil, err
	}

	log.Infof("AddFunds got server address %v", r.Address)

	// Create a script with server's data
	script, err := submarinelib.GenSubmarineSwapScript(r.ChainPublicKey, chainPublicKey, lightningPaymentHash, int64(72))
	if err != nil {
		log.Errorf("Error in GenSubmarineSwapScript: %v", err)
		return nil, err
	}

	log.Infof("AddFunds local script %v", script)


	// Determine which network we are running on
	var network *chaincfg.Params

	if cfg.Network == "testnet" {
		network = &chaincfg.TestNet3Params
	} else if cfg.Network == "simnet" {
		network = &chaincfg.SimNetParams
	} else if cfg.Network == "mainnet" {
		network = &chaincfg.MainNetParams
	} else {
		return nil, errors.New("unknown network type " + cfg.Network)
	}

	log.Infof("AddFunds local network %v", network)

	ourAddress := submarinelib.GenBase58Address(script, network)

	// Verify we are on the same page
	if ourAddress != r.Address {
		return nil, errors.New("base58 address mismatch")
	}

	// Save everything to DB
	addressInfo := &swapAddressInfo{
		Address: r.Address,
		Paid: false,
		TransactionConfirmed: false,
		PaymentHash: lightningPaymentHash,
		PaymentPreimage: lightningPreimage,
		OurPubKey: chainPublicKey,
		OurPrivateKey: chainPrivateKey,
		PayeePubKey: r.ChainPublicKey,
		SerializedScript: script,
	}

	err = saveSwapAddressInfo(addressInfo)
	if err != nil {
		return nil, err
	}

	log.Infof("AddFunds got address %v", r.Address)

	// Create JSON with the script and our private key (in case user wants to do the refund by himself)
	type ScriptBackup struct {
		Script string
		PrivateKey string
	}

	backup := ScriptBackup{Script: hex.EncodeToString(script), PrivateKey: hex.EncodeToString(chainPrivateKey)}
	jsonBytes,err := json.Marshal(backup)
	if err != nil {
		return nil, err
	}

	return &data.AddFundReply{Address: r.Address, MaxAllowedDeposit: r.MaxAllowedDeposit, ErrorMessage: r.ErrorMessage, BackupJson: string(jsonBytes[:])}, nil
}

func sendSwapInvoice(address string, tx string, value int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	c := breezservice.NewFundManagerClient(breezClientConnection)

	invoiceData := &data.InvoiceMemo {
		TransferRequest: true,
		Amount: value,
		PayerName: address,
		Description: tx,
	}

	memo, err := proto.Marshal(invoiceData)
	if err != nil {
		return err
	}

	paymentRequest, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: string(memo), Value: value, Private: true, Expiry: 60 * 60 * 24 * 30})
	if err != nil {
		log.Criticalf("Failed to call AddInvoice %v", err)
		return err
	}

	_, err = c.GetPayment(ctx, &breezservice.GetPaymentRequest{PaymentRequest: paymentRequest.PaymentRequest, Address: address})
	if err != nil {
		log.Criticalf("GetPayment failed: %v", err)
		return err
	}

	return nil
}

/*
GetFundStatus gets a notification token and does two things:
1. Register for notifications on all saved addresses
2. Fetch the current status for the saved addresses from the server
*/
func GetFundStatus(notificationToken string) (*data.FundStatusReply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	c := breezservice.NewFundManagerClient(breezClientConnection)
	addresses, err := fetchAllSwapAddresses()
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
	}

	var rawAddresses []string
	for _, add := range addresses {
		rawAddresses = append(rawAddresses, add.Address)
	}

	statusesMap, err := c.AddFundStatus(ctx, &breezservice.AddFundStatusRequest{NotificationToken: notificationToken, Addresses: rawAddresses})
	if err != nil {
		return nil, err
	}

	var confirmedAddresses []string
	var hasWaitingConfirmation bool
	for address, status := range statusesMap.Statuses {
		updateSwapAddressInfo(address, func(addressInfo *swapAddressInfo) {
			addressInfo.TransactionConfirmed = status.Confirmed
			addressInfo.Transaction = status.Tx
		})
		if status.Confirmed {
			confirmedAddresses = append(confirmedAddresses, address)
		} else {
			hasWaitingConfirmation = true
		}
	}

	status := data.FundStatusReply_NO_FUND
	if hasWaitingConfirmation {
		status = data.FundStatusReply_WAITING_CONFIRMATION
	} else if len(confirmedAddresses) > 0 {
		status = data.FundStatusReply_CONFIRMED
	}

	getPaymentsForConfirmedTransactions()
	return &data.FundStatusReply{Status: status}, nil
}

func watchFundTransfers() {
	go watchSettledSwapAddresses()
	go settlePendingTransfers()
}

func watchSettledSwapAddresses() {
	stream, err := lightningClient.SubscribeInvoices(context.Background(), &lnrpc.InvoiceSubscription{})
	if err != nil {
		log.Criticalf("watchSettledSwapAddresses failed to call SubscribeInvoices %v, %v", stream, err)
	}

	for {
		invoice, err := stream.Recv()
		log.Infof("watchSettledSwapAddresses - Invoice received by subscription")
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Criticalf("watchSettledSwapAddresses - failed to receive an invoice : %v", err)
		}
		if invoice.Settled {
			log.Infof("watchSettledSwapAddresses - removing paid swapAddressInfo")
			removed, err := removeSwapAddressByPaymentHash(invoice.RHash)
			if err != nil {
				log.Errorf("watchSettledSwapAddresses - failed to remove swap address %v", err)
			}

			log.Infof("watchSettledSwapAddresses - removed swap address from database result = %v", removed)
		}
	}
}

func settlePendingTransfers() error {
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
			continue
		}

		if notification.PubKey == cfg.RoutingNodePubKey && notification.Connected {
			getPaymentsForConfirmedTransactions()
		}
	}
}

func getPaymentsForConfirmedTransactions() {
	log.Infof("getPaymentsForConfirmedTransactions: asking for pending payments")
	confirmedAddresses, err := fetchSwapAddresses(func(addr *swapAddressInfo) bool {
		return addr.TransactionConfirmed
	})
	if err != nil {
		log.Errorf("getPaymentsForConfirmedTransactions: failed to fetch swap addresses %v", err)
		return
	}
	log.Infof("getPaymentsForConfirmedTransactions: confirmedAddresses length = %v", len(confirmedAddresses))
	for _, address := range confirmedAddresses {
		go getPayment(address.Address)
	}
}

func getPayment(address string) {
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	c := breezservice.NewFundManagerClient(breezClientConnection)
	reply, err := c.GetPayment(ctx, &breezservice.GetPaymentRequest{Address: address})
	if err != nil {
		log.Errorf("failed to get payment for address %v, err = %v", address, err)
		return
	}
	if len(reply.PaymentError) > 0 {
		log.Errorf("failed to get payment for address %v, err = %v", address, reply.PaymentError)
	}
	log.Infof("succeed to get payment for address %v", address)
}
