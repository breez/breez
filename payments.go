package breez

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/sync/singleflight"
)

const (
	defaultInvoiceExpiry       int64 = 3600
	invoiceCustomPartDelimiter       = " |\n"
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
func SendPaymentForRequest(paymentRequest string, amountSatoshi int64) (*data.PaymentResponse, error) {
	log.Infof("sendPaymentForRequest: amount = %v", amountSatoshi)
	if err := waitRoutingNodeConnected(); err != nil {
		return nil, err
	}
	decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return nil, err
	}
	if err := breezDB.SavePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return nil, err
	}
	log.Infof("sendPaymentForRequest: before sending payment...")
	response, err := lightningClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest, Amt: amountSatoshi})
	if err != nil {
		log.Infof("sendPaymentForRequest: error sending payment %v", err)
		return nil, err
	}

	if len(response.PaymentError) > 0 {
		log.Infof("sendPaymentForRequest finished with error, %v", response.PaymentError)
		traceReport, err := createPaymentTraceReport(paymentRequest, amountSatoshi, response)
		if err != nil {
			log.Errorf("failed to create trace report for failed payment %v", err)
		}
		return &data.PaymentResponse{PaymentError: response.PaymentError, TraceReport: traceReport}, nil
	}
	log.Infof("sendPaymentForRequest finished successfully")

	syncSentPayments()
	return &data.PaymentResponse{PaymentError: ""}, nil
}

/*
AddInvoice encapsulate a given amount and description in a payment request
*/
func AddInvoice(invoice *data.InvoiceMemo) (paymentRequest string, err error) {
	// Format the standard invoice memo
	memo := formatTextMemo(invoice)

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

// SendPaymentFailureBugReport is used for investigating payment failures.
// It should be used if the user agrees to send his payment details and the response of
// QueryRoutes running in his node. The information is sent to the "bugreporturl" service.
func SendPaymentFailureBugReport(jsonReport string) error {
	client := &http.Client{}
	req, err := http.NewRequest("POST", cfg.BugReportURL+"/paymentfailure", bytes.NewBuffer([]byte(jsonReport)))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("PF-Key", cfg.BugReportURLSecret)
	_, err = client.Do(req)
	if err != nil {
		log.Errorf("Error in sending bug report: ", err)
		return err
	}
	log.Infof(jsonReport)
	return nil
}

func createPaymentTraceReport(paymentRequest string, amount int64, paymentResponse *lnrpc.SendResponse) (string, error) {
	decodedPayReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		log.Errorf("DecodePaymentRequest error: %v", err)
		return "", err
	}

	lnInfo, err := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Errorf("GetInfo error: %v", err)
		return "", err
	}

	if amount == 0 {
		amount = decodedPayReq.NumSatoshis
	}

	responseMap := map[string]interface{}{
		"request_details": map[string]interface{}{
			"source_node":     lnInfo.IdentityPubkey,
			"amount":          amount,
			"payment_request": decodedPayReq,
		},
	}

	responseMap["payment_error"] = paymentResponse.PaymentError

	if paymentResponse.FailedRoutes != nil {
		responseMap["routes"] = paymentResponse.FailedRoutes
	}

	response, err := json.MarshalIndent(responseMap, "", "  ")
	if err != nil {
		fmt.Println("unable to marshal response to json: ", err)
		return "", err
	}

	return string(response), nil
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
	if invoiceMemo.Description == transferFundsRequest {
		invoiceMemo.TransferRequest = true
	}
	return invoiceMemo
}

func formatTextMemo(invoice *data.InvoiceMemo) string {
	memo := invoice.Description
	formatPayeeData := invoice.PayeeName != "" && invoice.PayeeImageURL != ""
	if formatPayeeData {
		memo += invoiceCustomPartDelimiter
		customParts := []string{invoice.PayeeName, invoice.PayeeImageURL}
		formatPayerData := invoice.PayerName != "" && invoice.PayerImageURL != ""
		if formatPayerData {
			customParts = append(customParts, invoice.PayerName, invoice.PayerImageURL)
		}
		memo += strings.Join(customParts, " | ")
	}
	return memo
}

func parseTextMemo(memo string, invoiceMemo *data.InvoiceMemo) {
	invoiceMemo.Description = memo
	invoiceParts := strings.Split(memo, invoiceCustomPartDelimiter)
	if len(invoiceParts) == 2 {
		invoiceMemo.Description = invoiceParts[0]
		customData := invoiceParts[1]
		customDataParts := strings.Split(customData, " | ")
		invoiceMemo.PayeeName = customDataParts[0]
		invoiceMemo.PayeeImageURL = customDataParts[1]
		if len(customDataParts) == 4 {
			invoiceMemo.PayerName = customDataParts[2]
			invoiceMemo.PayerImageURL = customDataParts[3]
		}
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
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_PAYMENT_SENT}
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
	onAccountChanged()
	return nil
}
