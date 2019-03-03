package breez

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/sync/singleflight"
)

const (
	defaultInvoiceExpiry int64 = 3600
)

var blankInvoiceGroup singleflight.Group

/*
GetPayments is responsible for retrieving the payment were made in this account
*/
func GetPayments() (*data.PaymentsList, error) {
	rawPayments, err := breezDB.FetchAllAccountPayments()
	if err != nil {
		return nil, err
	}

	pendingPayments, err := getPendingPayments()
	if err != nil {
		return nil, err
	}
	rawPayments = append(rawPayments, pendingPayments...)

	var paymentsList []*data.Payment
	for _, payment := range rawPayments {
		paymentItem := &data.Payment{
			Amount:            payment.Amount,
			Fee:               payment.Fee,
			CreationTimestamp: payment.CreationTimestamp,
			RedeemTxID:        payment.RedeemTxID,
			PaymentHash:       payment.PaymentHash,
			Destination:       payment.Destination,
			InvoiceMemo: &data.InvoiceMemo{
				Description:     payment.Description,
				Amount:          payment.Amount,
				PayeeImageURL:   payment.PayeeImageURL,
				PayeeName:       payment.PayeeName,
				PayerImageURL:   payment.PayerImageURL,
				PayerName:       payment.PayerName,
				TransferRequest: payment.TransferRequest,
			},
			PendingExpirationHeight:    payment.PendingExpirationHeight,
			PendingExpirationTimestamp: payment.PendingExpirationTimestamp,
		}
		switch payment.Type {
		case db.SentPayment:
			paymentItem.Type = data.Payment_SENT
		case db.ReceivedPayment:
			paymentItem.Type = data.Payment_RECEIVED
		case db.DepositPayment:
			paymentItem.Type = data.Payment_DEPOSIT
		case db.WithdrawalPayment:
			paymentItem.Type = data.Payment_WITHDRAWAL
		}

		paymentsList = append(paymentsList, paymentItem)
	}

	sort.Slice(paymentsList, func(i, j int) bool {
		return paymentsList[i].CreationTimestamp > paymentsList[j].CreationTimestamp
	})

	resultPayments := &data.PaymentsList{PaymentsList: paymentsList}
	return resultPayments, nil
}

/*
SendPaymentForRequest send the payment according to the details specified in the bolt 11 payment request.
If the payment was failed an error is returned
*/
func SendPaymentForRequest(paymentRequest string, amountSatoshi int64) error {
	log.Infof("sendPaymentForRequest: amount = %v", amountSatoshi)
	if err := waitRoutingNodeConnected(); err != nil {
		return err
	}
	decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return err
	}
	if err := breezDB.SavePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return err
	}
	log.Infof("sendPaymentForRequest: before sending payment...")
	response, err := lightningClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest, Amt: amountSatoshi})
	if err != nil {
		log.Infof("sendPaymentForRequest: error sending payment %v", err)
		return err
	}

	if len(response.PaymentError) > 0 {
		log.Infof("sendPaymentForRequest finished with error, %v", response.PaymentError)
		return errors.New(response.PaymentError)
	}
	log.Infof("sendPaymentForRequest finished successfully")

	syncSentPayments()
	return nil
}

/*
AddInvoice encapsulate a given amount and description in a payment request
*/
func AddInvoice(invoice *data.InvoiceMemo) (paymentRequest string, err error) {
	// Format the standard invoice memo
	memo := invoice.Description
	formatPayeeData := invoice.PayeeName != "" && invoice.PayeeImageURL != ""
	if formatPayeeData {
		memo += (" | " + invoice.PayeeName + " | " + invoice.PayeeImageURL)
		formatPayerData := invoice.PayerName != "" && invoice.PayerImageURL != ""
		if formatPayerData {
			memo += (" | " + invoice.PayerName + " | " + invoice.PayerImageURL)
		}
	}

	if invoice.Expiry <= 0 {
		invoice.Expiry = defaultInvoiceExpiry
	}
	if err := waitRoutingNodeConnected(); err != nil {
		return "", err
	}
	response, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: memo, Private: true, Value: invoice.Amount, Expiry: invoice.Expiry})
	if err != nil {
		return "", err
	}
	log.Infof("Generated Invoice: %v", response.PaymentRequest)
	return response.PaymentRequest, nil
}

/*
DecodePaymentRequest is used by the payer to decode the payment request and read the invoice details.
*/
func DecodePaymentRequest(paymentRequest string) (*data.InvoiceMemo, error) {
	log.Infof("DecodePaymentRequest %v", paymentRequest)
	decodedPayReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		log.Errorf("DecodePaymentRequest error: %v", err)
		return nil, err
	}
	invoiceMemo := extractMemo(decodedPayReq)
	return invoiceMemo, nil
}

/*
GetRelatedInvoice is used by the payee to fetch the related invoice of its sent payment request so he can see if it is settled.
*/
func GetRelatedInvoice(paymentRequest string) (*data.Invoice, error) {
	decodedPayReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		log.Infof("Can't decode payment request: %v", paymentRequest)
		return nil, err
	}

	invoiceMemo := extractMemo(decodedPayReq)

	lookup, err := lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHashStr: decodedPayReq.PaymentHash})
	if err != nil {
		return nil, err
	}

	invoice := &data.Invoice{
		Memo:    invoiceMemo,
		AmtPaid: lookup.AmtPaidSat,
		Settled: lookup.Settled,
	}

	return invoice, nil
}

func extractMemo(decodedPayReq *lnrpc.PayReq) *data.InvoiceMemo {
	invoiceMemo := &data.InvoiceMemo{}
	invoiceMemo.Amount = decodedPayReq.NumSatoshis
	parseTextMemo(decodedPayReq.Description, invoiceMemo)
	return invoiceMemo
}

func formatTextMemo(invoice *data.InvoiceMemo) string {
	memo := invoice.Description
	formatPayeeData := invoice.PayeeName != "" && invoice.PayeeImageURL != ""
	if formatPayeeData {
		memo += (" | " + invoice.PayeeName + " | " + invoice.PayeeImageURL)
		formatPayerData := invoice.PayerName != "" && invoice.PayerImageURL != ""
		if formatPayerData {
			memo += (" | " + invoice.PayerName + " | " + invoice.PayerImageURL)
		}
	}
	return memo
}

func parseTextMemo(memo string, invoiceMemo *data.InvoiceMemo) {
	if strings.Count(memo, " | ") >= 2 {
		// There is also the 'description | payee | logo' encoding
		// meant to encode breez metadata in a way that's human readable
		invoiceData := strings.Split(memo, " | ")
		invoiceMemo.Description = invoiceData[0]
		invoiceMemo.PayeeName = invoiceData[1]
		invoiceMemo.PayeeImageURL = invoiceData[2]

		// If we also have the payer data, grap it.
		if len(invoiceData) == 5 {
			invoiceMemo.PayerName = invoiceData[3]
			invoiceMemo.PayerImageURL = invoiceData[4]
		}
	} else {
		invoiceMemo.Description = memo
	}
}

func watchPayments() {
	syncSentPayments()
	_, lastInvoiceSettledIndex := breezDB.FetchPaymentsSyncInfo()
	log.Infof("last invoice settled index ", lastInvoiceSettledIndex)
	stream, err := lightningClient.SubscribeInvoices(context.Background(), &lnrpc.InvoiceSubscription{SettleIndex: lastInvoiceSettledIndex})
	if err != nil {
		log.Criticalf("Failed to call SubscribeInvoices %v, %v", stream, err)
	}

	go func() {
		for {
			invoice, err := stream.Recv()
			log.Infof("watchPayments - Invoice received by subscription")
			if err != nil {
				log.Criticalf("Failed to receive an invoice : %v", err)
				return
			}
			if invoice.Settled {
				log.Infof("watchPayments adding a received payment")
				if err = onNewReceivedPayment(invoice); err != nil {
					log.Criticalf("Failed to update received payment : %v", err)
					return
				}
			}
		}
	}()
}

func syncSentPayments() error {
	log.Infof("syncSentPayments")
	lightningPayments, err := lightningClient.ListPayments(context.Background(), &lnrpc.ListPaymentsRequest{})
	if err != nil {
		return err
	}
	lastPaymentTime, _ := breezDB.FetchPaymentsSyncInfo()
	for _, paymentItem := range lightningPayments.Payments {
		if paymentItem.CreationDate <= lastPaymentTime {
			continue
		}
		log.Infof("syncSentPayments adding an outgoing payment")
		onNewSentPayment(paymentItem)
	}

	return nil

	//TODO delete history of payment requests after the new payments API stablized.
}

func getPendingPayments() ([]*db.PaymentInfo, error) {
	var payments []*db.PaymentInfo

	if DaemonReady() {
		channelsRes, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			return nil, err
		}

		chainInfo, chainErr := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			log.Errorf("Failed get chain info", chainErr)
			return nil, chainErr
		}

		for _, ch := range channelsRes.Channels {
			for _, htlc := range ch.PendingHtlcs {
				pendingItem, err := createPendingPayment(htlc, chainInfo.BlockHeight)
				if err != nil {
					return nil, err
				}
				payments = append(payments, pendingItem)
			}
		}
	}

	return payments, nil
}

func createPendingPayment(htlc *lnrpc.HTLC, currentBlockHeight uint32) (*db.PaymentInfo, error) {
	paymentType := db.SentPayment
	if htlc.Incoming {
		paymentType = db.ReceivedPayment
	}

	var paymentRequest string
	if htlc.Incoming {
		invoice, err := lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: htlc.HashLock})
		if err != nil {
			log.Errorf("createPendingPayment - failed to call LookupInvoice %v", err)
			return nil, err
		}
		paymentRequest = invoice.PaymentRequest
	} else {
		payReqBytes, err := breezDB.FetchPaymentRequest(string(htlc.HashLock))
		if err != nil {
			log.Errorf("createPendingPayment - failed to call fetchPaymentRequest %v", err)
			return nil, err
		}
		paymentRequest = string(payReqBytes)
	}

	minutesToExpire := time.Duration((htlc.ExpirationHeight - currentBlockHeight) * 10)
	paymentData := &db.PaymentInfo{
		Type:                       paymentType,
		Amount:                     htlc.Amount,
		CreationTimestamp:          time.Now().Unix(),
		PendingExpirationHeight:    htlc.ExpirationHeight,
		PendingExpirationTimestamp: time.Now().Add(minutesToExpire * time.Minute).Unix(),
	}

	if paymentRequest != "" {
		decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
		if err != nil {
			return nil, err
		}

		invoiceMemo, err := DecodePaymentRequest(paymentRequest)
		if err != nil {
			return nil, err
		}

		paymentData.Description = invoiceMemo.Description
		paymentData.PayeeImageURL = invoiceMemo.PayeeImageURL
		paymentData.PayeeName = invoiceMemo.PayeeName
		paymentData.PayerImageURL = invoiceMemo.PayerImageURL
		paymentData.PayerName = invoiceMemo.PayerName
		paymentData.TransferRequest = invoiceMemo.TransferRequest
		paymentData.PaymentHash = decodedReq.PaymentHash
		paymentData.Destination = decodedReq.Destination
		paymentData.CreationTimestamp = decodedReq.Timestamp
	}

	return paymentData, nil
}

func onNewSentPayment(paymentItem *lnrpc.Payment) error {
	paymentRequest, err := breezDB.FetchPaymentRequest(paymentItem.PaymentHash)
	if err != nil {
		return err
	}
	var invoiceMemo *data.InvoiceMemo
	if paymentRequest != nil && len(paymentRequest) > 0 {
		if invoiceMemo, err = DecodePaymentRequest(string(paymentRequest)); err != nil {
			return err
		}
	}

	paymentType := db.SentPayment
	decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: string(paymentRequest)})
	if err != nil {
		return err
	}
	if decodedReq.Destination == cfg.RoutingNodePubKey {
		paymentType = db.WithdrawalPayment
	}

	paymentData := &db.PaymentInfo{
		Type:              paymentType,
		Amount:            paymentItem.Value,
		Fee:               paymentItem.Fee,
		CreationTimestamp: paymentItem.CreationDate,
		Description:       invoiceMemo.Description,
		PayeeImageURL:     invoiceMemo.PayeeImageURL,
		PayeeName:         invoiceMemo.PayeeName,
		PayerImageURL:     invoiceMemo.PayerImageURL,
		PayerName:         invoiceMemo.PayerName,
		TransferRequest:   invoiceMemo.TransferRequest,
		PaymentHash:       decodedReq.PaymentHash,
		Destination:       decodedReq.Destination,
	}

	err = breezDB.AddAccountPayment(paymentData, 0, uint64(paymentItem.CreationDate))
	backupManager.RequestBackup()
	onAccountChanged()
	return err
}

func onNewReceivedPayment(invoice *lnrpc.Invoice) error {
	var invoiceMemo *data.InvoiceMemo
	var err error
	if len(invoice.PaymentRequest) > 0 {
		if invoiceMemo, err = DecodePaymentRequest(invoice.PaymentRequest); err != nil {
			return err
		}
	}

	paymentType := db.ReceivedPayment
	if invoiceMemo.TransferRequest {
		paymentType = db.DepositPayment
	}

	paymentData := &db.PaymentInfo{
		Type:              paymentType,
		Amount:            invoice.AmtPaidSat,
		CreationTimestamp: invoice.SettleDate,
		Description:       invoiceMemo.Description,
		PayeeImageURL:     invoiceMemo.PayeeImageURL,
		PayeeName:         invoiceMemo.PayeeName,
		PayerImageURL:     invoiceMemo.PayerImageURL,
		PayerName:         invoiceMemo.PayerName,
		TransferRequest:   invoiceMemo.TransferRequest,
		PaymentHash:       hex.EncodeToString(invoice.RHash),
	}

	err = breezDB.AddAccountPayment(paymentData, invoice.SettleIndex, 0)
	if err != nil {
		log.Criticalf("Unable to add reveived payment : %v", err)
		return err
	}
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_INVOICE_PAID}
	backupManager.RequestBackup()
	onAccountChanged()
	return nil
}
