package breez

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/golang/protobuf/proto"

	breezservice "github.com/breez/breez/breez"
)

type swapAddressInfo struct {
	Address              string
	PaymentHash          []byte
	Transaction          string
	TransactinoConfirmed bool
	Payed                bool
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
AddFunds is responsible for topping up an existing channel
*/
func AddFunds(notificationToken string) (*data.AddFundReply, error) {
	invoiceData := &data.InvoiceMemo{TransferRequest: true}
	memo, err := proto.Marshal(invoiceData)
	if err != nil {
		return nil, err
	}

	invoice, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: string(memo), Private: true, Expiry: 60 * 60 * 24 * 30})
	if err != nil {
		log.Criticalf("Failed to call AddInvoice %v", err)
		return nil, err
	}

	c := breezservice.NewFundManagerClient(breezClientConnection)
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()

	r, err := c.AddFund(ctx, &breezservice.AddFundRequest{NotificationToken: notificationToken, PaymentRequest: invoice.PaymentRequest})
	if err != nil {
		log.Errorf("Error in AddFund: %v", err)
		return nil, err
	}

	if r.Address != "" {
		addressInfo := &swapAddressInfo{Address: r.Address, Payed: false, TransactinoConfirmed: false, PaymentHash: invoice.RHash}
		err = saveSwapAddressInfo(addressInfo)
		if err != nil {
			return nil, err
		}
	}

	return &data.AddFundReply{Address: r.Address, MaxAllowedDeposit: r.MaxAllowedDeposit, ErrorMessage: r.ErrorMessage}, nil
}

/*
RemoveFunds transfers the user funds from the chanel to a supplied on-chain address
It is executed in three steps:
1. Send the breez server an address and an amount and get a corresponding payment request
2. Pay the payment request.
3. Redeem the removed funds from the server
*/
func RemoveFunds(amount int64, address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	c := breezservice.NewFundManagerClient(breezClientConnection)
	reply, err := c.RemoveFund(ctx, &breezservice.RemoveFundRequest{Address: address, Amount: amount})
	if err != nil {
		log.Errorf("RemoveFund: server endpoint call failed: %v", err)
		return err
	}
	log.Infof("RemoveFunds: got payment request: %v", reply.PaymentRequest)
	payreq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: reply.PaymentRequest})
	if err != nil {
		log.Errorf("DecodePayReq of server response failed: %v", err)
		return err
	}

	err = SendPaymentForRequest(reply.PaymentRequest)
	if err != nil {
		log.Errorf("SendPaymentForRequest failed: %v", err)
		return err
	}
	log.Infof("SendPaymentForRequest finished successfully")
	_, err = c.RedeemRemovedFunds(ctx, &breezservice.RedeemRemovedFundsRequest{Paymenthash: payreq.PaymentHash})
	if err != nil {
		log.Errorf("RedeemRemovedFunds failed: %v", err)
	}
	log.Infof("RemoveFunds finished successfully")
	return err
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
			addressInfo.TransactinoConfirmed = status.Confirmed
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
		return addr.TransactinoConfirmed
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
