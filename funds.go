package breez

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/btcsuite/btcd/chaincfg"
	"io"
	"time"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	submarine "github.com/breez/submarinelib"
)

type swapAddressInfo struct {
	Address              string
	PaymentHash          []byte
	PaymentPreimage      []byte
	OurPubKey            []byte
	OurPrivateKey        []byte
	PayeePubKey          []byte
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
func AddFunds(notificationToken string) (string, error) {
	// Generate two secrets
	lightningPaymentHash, lightningPreimage := submarine.GenSecret()
	chainPublicKey, chainPrivateKey, err := submarine.GenPublicPrivateKeypair()

	// Send them to the server along with the notification token
	c := breezservice.NewFundManagerClient(breezClientConnection)
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()

	r, err := c.AddFund(ctx, &breezservice.AddFundRequest{
		NotificationToken:  notificationToken,
		LightningPublicKey: lightningPaymentHash[:],
		ChainPublicKey:     chainPublicKey[:]})
	if err != nil {
		log.Errorf("Error in AddFund: %v", err)
		return "", err
	}

	// Create a script with server's data
	script, err := submarine.GenSubmarineSwapScript(r.ChainPublicKey, chainPublicKey[:], lightningPaymentHash[:], 600)
	if err != nil {
		log.Errorf("Error in GenSubmarineSwapScript: %v", err)
		return "", err
	}

	// Determine which network we are running on
	var network *chaincfg.Params

	if cfg.Network == "testnet" {
		network = &chaincfg.TestNet3Params
	} else if cfg.Network == "simnet" {
		network = &chaincfg.SimNetParams
	} else if cfg.Network == "mainnet" {
		network = &chaincfg.MainNetParams
	} else {
		return "", errors.New("unknown network type " + cfg.Network)
	}

	// Verify we are on the same page
	if submarine.GenBase58Address(script, network)  != r.Address {
		return "", errors.New("base58 address mismatch")
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
	}

	err = saveSwapAddressInfo(addressInfo)
	if err != nil {
		return "", err
	}

	return r.Address, nil

	// Create an invoice when server tells us to... After someone paid into the address
	// This is done on a different call

	//invoiceData := &data.InvoiceMemo{TransferRequest: true}
	//memo, err := proto.Marshal(invoiceData)
	//if err != nil {
	//	return "", err
	//}

	//invoice, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: string(memo), Private: true, Expiry: 60 * 60 * 24 * 30})
	//if err != nil {
	//	log.Criticalf("Failed to call AddInvoice %v", err)
	//	return "", err
	//}
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
