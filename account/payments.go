package account

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/lnrpc"
)

const (
	defaultInvoiceExpiry       int64 = 3600
	invoiceCustomPartDelimiter       = " |\n"
	transferFundsRequest             = "Bitcoin Transfer"
)

/*
GetPayments is responsible for retrieving the payment were made in this account
*/
func (a *Service) GetPayments() (*data.PaymentsList, error) {
	rawPayments, err := a.breezDB.FetchAllAccountPayments()
	if err != nil {
		return nil, err
	}

	pendingPayments, err := a.getPendingPayments()
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
func (a *Service) SendPaymentForRequest(paymentRequest string, amountSatoshi int64) error {
	a.log.Infof("sendPaymentForRequest: amount = %v", amountSatoshi)
	if err := a.waitRoutingNodeConnected(); err != nil {
		return err
	}
	decodedReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return err
	}
	if err := a.breezDB.SavePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return err
	}
	a.log.Infof("sendPaymentForRequest: before sending payment...")
	response, err := a.lightningClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest, Amt: amountSatoshi})
	if err != nil {
		a.log.Infof("sendPaymentForRequest: error sending payment %v", err)
		return err
	}

	if len(response.PaymentError) > 0 {
		a.log.Infof("sendPaymentForRequest finished with error, %v", response.PaymentError)
		return errors.New(response.PaymentError)
	}
	a.log.Infof("sendPaymentForRequest finished successfully")

	a.syncSentPayments()
	return nil
}

/*
AddInvoice encapsulate a given amount and description in a payment request
*/
func (a *Service) AddInvoice(invoice *data.InvoiceMemo) (paymentRequest string, err error) {
	// Format the standard invoice memo
	memo := formatTextMemo(invoice)

	if invoice.Expiry <= 0 {
		invoice.Expiry = defaultInvoiceExpiry
	}
	if err := a.waitRoutingNodeConnected(); err != nil {
		return "", err
	}
	response, err := a.lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: memo, Private: true, Value: invoice.Amount, Expiry: invoice.Expiry})
	if err != nil {
		return "", err
	}
	a.log.Infof("Generated Invoice: %v", response.PaymentRequest)
	return response.PaymentRequest, nil
}

// SendPaymentFailureBugReport is used for investigating payment failures.
// It should be used if the user agrees to send his payment details and the response of
// QueryRoutes running in his node. The information is sent to the "bugreporturl" service.
func (a *Service) SendPaymentFailureBugReport(paymentRequest string, amount int64) error {
	decodedPayReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Errorf("DecodePaymentRequest error: %v", err)
		return err
	}

	lnInfo, err := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		a.log.Errorf("GetInfo error: %v", err)
		return err
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

	queryResponse, err := a.lightningClient.QueryRoutes(context.Background(), &lnrpc.QueryRoutesRequest{
		Amt:       amount,
		NumRoutes: 5,
		PubKey:    decodedPayReq.Destination,
	})
	if err != nil {
		responseMap["query_routes_error"] = err
		a.log.Errorf("QueryRoutes error: %v", err)
	}
	if queryResponse != nil {
		responseMap["routes"] = queryResponse.Routes
	}

	response, err := json.MarshalIndent(responseMap, "", "  ")
	if err != nil {
		fmt.Println("unable to marshal response to json: ", err)
		return err
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", a.cfg.BugReportURL+"/paymentfailure", bytes.NewBuffer(response))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("PF-Key", a.cfg.BugReportURLSecret)
	_, err = client.Do(req)
	if err != nil {
		a.log.Errorf("Error in sending bug report: ", err)
		return err
	}
	a.log.Infof(string(response))
	return nil
}

/*
DecodePaymentRequest is used by the payer to decode the payment request and read the invoice details.
*/
func (a *Service) DecodePaymentRequest(paymentRequest string) (*data.InvoiceMemo, error) {
	a.log.Infof("DecodePaymentRequest %v", paymentRequest)
	decodedPayReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Errorf("DecodePaymentRequest error: %v", err)
		return nil, err
	}
	invoiceMemo := a.extractMemo(decodedPayReq)
	return invoiceMemo, nil
}

/*
GetRelatedInvoice is used by the payee to fetch the related invoice of its sent payment request so he can see if it is settled.
*/
func (a *Service) GetRelatedInvoice(paymentRequest string) (*data.Invoice, error) {
	decodedPayReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Infof("Can't decode payment request: %v", paymentRequest)
		return nil, err
	}

	invoiceMemo := a.extractMemo(decodedPayReq)

	lookup, err := a.lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHashStr: decodedPayReq.PaymentHash})
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

func (a *Service) extractMemo(decodedPayReq *lnrpc.PayReq) *data.InvoiceMemo {
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

func (a *Service) watchPayments() {
	defer a.wg.Done()

	a.syncSentPayments()
	_, lastInvoiceSettledIndex := a.breezDB.FetchPaymentsSyncInfo()
	a.log.Infof("last invoice settled index ", lastInvoiceSettledIndex)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := a.lightningClient.SubscribeInvoices(ctx, &lnrpc.InvoiceSubscription{SettleIndex: lastInvoiceSettledIndex})
	if err != nil {
		a.log.Criticalf("Failed to call SubscribeInvoices %v, %v", stream, err)
	}

	go func() {
		for {
			invoice, err := stream.Recv()
			a.log.Infof("watchPayments - Invoice received by subscription")
			if err != nil {
				a.log.Criticalf("Failed to receive an invoice : %v", err)
				return
			}
			if invoice.Settled {
				a.log.Infof("watchPayments adding a received payment")
				if err = a.onNewReceivedPayment(invoice); err != nil {
					a.log.Criticalf("Failed to update received payment : %v", err)
					return
				}
			}
		}
	}()

	go func() {
		<-a.quitChan
		a.log.Infof("Canceling subscription")
		cancel()
	}()
}

func (a *Service) syncSentPayments() error {
	a.log.Infof("syncSentPayments")
	lightningPayments, err := a.lightningClient.ListPayments(context.Background(), &lnrpc.ListPaymentsRequest{})
	if err != nil {
		return err
	}
	lastPaymentTime, _ := a.breezDB.FetchPaymentsSyncInfo()
	for _, paymentItem := range lightningPayments.Payments {
		if paymentItem.CreationDate <= lastPaymentTime {
			continue
		}
		a.log.Infof("syncSentPayments adding an outgoing payment")
		a.onNewSentPayment(paymentItem)
	}

	return nil

	//TODO delete history of payment requests after the new payments API stablized.
}

func (a *Service) getPendingPayments() ([]*db.PaymentInfo, error) {
	var payments []*db.PaymentInfo

	if a.daemonRPCReady() {
		channelsRes, err := a.lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			return nil, err
		}

		chainInfo, chainErr := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			a.log.Errorf("Failed get chain info", chainErr)
			return nil, chainErr
		}

		for _, ch := range channelsRes.Channels {
			for _, htlc := range ch.PendingHtlcs {
				pendingItem, err := a.createPendingPayment(htlc, chainInfo.BlockHeight)
				if err != nil {
					return nil, err
				}
				payments = append(payments, pendingItem)
			}
		}
	}

	return payments, nil
}

func (a *Service) createPendingPayment(htlc *lnrpc.HTLC, currentBlockHeight uint32) (*db.PaymentInfo, error) {
	paymentType := db.SentPayment
	if htlc.Incoming {
		paymentType = db.ReceivedPayment
	}

	var paymentRequest string
	if htlc.Incoming {
		invoice, err := a.lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: htlc.HashLock})
		if err != nil {
			a.log.Errorf("createPendingPayment - failed to call LookupInvoice %v", err)
			return nil, err
		}
		paymentRequest = invoice.PaymentRequest
	} else {
		payReqBytes, err := a.breezDB.FetchPaymentRequest(string(htlc.HashLock))
		if err != nil {
			a.log.Errorf("createPendingPayment - failed to call fetchPaymentRequest %v", err)
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
		decodedReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
		if err != nil {
			return nil, err
		}

		invoiceMemo, err := a.DecodePaymentRequest(paymentRequest)
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

func (a *Service) onNewSentPayment(paymentItem *lnrpc.Payment) error {
	paymentRequest, err := a.breezDB.FetchPaymentRequest(paymentItem.PaymentHash)
	if err != nil {
		return err
	}
	var invoiceMemo *data.InvoiceMemo
	if paymentRequest != nil && len(paymentRequest) > 0 {
		if invoiceMemo, err = a.DecodePaymentRequest(string(paymentRequest)); err != nil {
			return err
		}
	}

	paymentType := db.SentPayment
	decodedReq, err := a.lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: string(paymentRequest)})
	if err != nil {
		return err
	}
	if decodedReq.Destination == a.cfg.RoutingNodePubKey {
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

	err = a.breezDB.AddAccountPayment(paymentData, 0, uint64(paymentItem.CreationDate))
	a.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_PAYMENT_SENT})
	a.onAccountChanged()
	return err
}

func (a *Service) onNewReceivedPayment(invoice *lnrpc.Invoice) error {
	var invoiceMemo *data.InvoiceMemo
	var err error
	if len(invoice.PaymentRequest) > 0 {
		if invoiceMemo, err = a.DecodePaymentRequest(invoice.PaymentRequest); err != nil {
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

	err = a.breezDB.AddAccountPayment(paymentData, invoice.SettleIndex, 0)
	if err != nil {
		a.log.Criticalf("Unable to add reveived payment : %v", err)
		return err
	}
	a.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_INVOICE_PAID})
	a.onAccountChanged()
	return nil
}
