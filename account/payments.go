package account

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

	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/golang/protobuf/jsonpb"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lntypes"
)

const (
	defaultInvoiceExpiry       int64 = 3600
	invoiceCustomPartDelimiter       = " |\n"
	transferFundsRequest             = "Bitcoin Transfer"
)

// PaymentResponse is the response of a payment attempt.
type PaymentResponse struct {
	PaymentError string
	TraceReport  string
}

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
			Amount:                     payment.Amount,
			Fee:                        payment.Fee,
			CreationTimestamp:          payment.CreationTimestamp,
			RedeemTxID:                 payment.RedeemTxID,
			PaymentHash:                payment.PaymentHash,
			Destination:                payment.Destination,
			PendingExpirationHeight:    payment.PendingExpirationHeight,
			PendingExpirationTimestamp: payment.PendingExpirationTimestamp,
			Preimage:                   payment.Preimage,
			ClosedChannelPoint:         payment.ClosedChannelPoint,
			IsChannelPending:           payment.Type == db.ClosedChannelPayment && payment.ClosedChannelStatus != db.ConfirmedClose,
			IsChannelCloseConfimed:     payment.Type == db.ClosedChannelPayment && payment.ClosedChannelStatus != db.WaitingClose,
			ClosedChannelTxID:          payment.ClosedChannelTxID,
		}
		if payment.Type != db.ClosedChannelPayment {
			paymentItem.InvoiceMemo = &data.InvoiceMemo{
				Description:     payment.Description,
				Amount:          payment.Amount,
				PayeeImageURL:   payment.PayeeImageURL,
				PayeeName:       payment.PayeeName,
				PayerImageURL:   payment.PayerImageURL,
				PayerName:       payment.PayerName,
				TransferRequest: payment.TransferRequest,
			}
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
		case db.ClosedChannelPayment:
			paymentItem.Type = data.Payment_CLOSED_CHANNEL
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
	if err := a.waitChannelActive(); err != nil {
		return err
	}
	lnclient := a.daemonAPI.APIClient()
	decodedReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return err
	}
	if err := a.breezDB.SavePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return err
	}
	a.log.Infof("sendPaymentForRequest: before sending payment...")

	// At this stage we are ready to send asynchronously the payment through the daemon.

	go func() {
		response, err := lnclient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest, Amt: amountSatoshi})
		if err != nil {
			a.log.Infof("sendPaymentForRequest: error sending payment %v", err)
			a.notifyPaymentResult(false, paymentRequest, err.Error(), "")
			return
		}

		if len(response.PaymentError) > 0 {
			a.log.Infof("sendPaymentForRequest finished with error, %v", response.PaymentError)
			traceReport, err := a.createPaymentTraceReport(paymentRequest, amountSatoshi, response)
			if err != nil {
				a.log.Errorf("failed to create trace report for failed payment %v", err)
			}
			a.notifyPaymentResult(false, paymentRequest, response.PaymentError, traceReport)
			return
		}
		a.log.Infof("sendPaymentForRequest finished successfully")
		a.notifyPaymentResult(true, paymentRequest, "", "")
		a.syncSentPayments()
	}()

	return nil
}

/*
AddInvoice encapsulate a given amount and description in a payment request
*/
func (a *Service) AddInvoice(invoice *data.InvoiceMemo) (paymentRequest string, err error) {
	lnclient := a.daemonAPI.APIClient()

	// Format the standard invoice memo
	memo := formatTextMemo(invoice)

	if invoice.Expiry <= 0 {
		invoice.Expiry = defaultInvoiceExpiry
	}
	if err := a.waitChannelActive(); err != nil {
		return "", err
	}
	channelsRes, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return "", err
	}
	var routeHints []*lnrpc.RouteHint
	for _, h := range channelsRes.Channels {
		ci, err := lnclient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{
			ChanId: h.ChanId,
		})
		if err != nil {
			a.log.Errorf("Unable to add routing hint for channel %v error=%v", h.ChanId, err)
			continue
		}
		remotePolicy := ci.Node1Policy
		if ci.Node2Pub == h.RemotePubkey {
			remotePolicy = ci.Node2Policy
		}
		a.log.Infof("adding routing hint = %v", h.RemotePubkey)
		routeHints = append(routeHints, &lnrpc.RouteHint{
			HopHints: []*lnrpc.HopHint{
				&lnrpc.HopHint{
					NodeId:                    h.RemotePubkey,
					ChanId:                    h.ChanId,
					FeeBaseMsat:               uint32(remotePolicy.FeeBaseMsat),
					FeeProportionalMillionths: uint32(remotePolicy.FeeRateMilliMsat),
					CltvExpiryDelta:           remotePolicy.TimeLockDelta,
				},
			},
		})
	}
	response, err := lnclient.AddInvoice(context.Background(), &lnrpc.Invoice{
		Memo: memo, Private: true, Value: invoice.Amount,
		Expiry: invoice.Expiry, RouteHints: routeHints,
	})
	if err != nil {
		return "", err
	}
	a.log.Infof("Generated Invoice: %v", response.PaymentRequest)
	return response.PaymentRequest, nil
}

// SendPaymentFailureBugReport is used for investigating payment failures.
// It should be used if the user agrees to send his payment details and the response of
// QueryRoutes running in his node. The information is sent to the "bugreporturl" service.
func (a *Service) SendPaymentFailureBugReport(jsonReport string) error {
	client := &http.Client{}
	req, err := http.NewRequest("POST", a.cfg.BugReportURL+"/paymentfailure", bytes.NewBuffer([]byte(jsonReport)))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("PF-Key", a.cfg.BugReportURLSecret)
	_, err = client.Do(req)
	if err != nil {
		a.log.Errorf("Error in sending bug report: ", err)
		return err
	}
	a.log.Infof(jsonReport)
	return nil
}

func (a *Service) createPaymentTraceReport(paymentRequest string, amount int64, paymentResponse *lnrpc.SendResponse) (string, error) {
	lnclient := a.daemonAPI.APIClient()

	decodedPayReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Errorf("DecodePaymentRequest error: %v", err)
		return "", err
	}

	lnInfo, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		a.log.Errorf("GetInfo error: %v", err)
		return "", err
	}

	netInfo, err := lnclient.GetNetworkInfo(context.Background(), &lnrpc.NetworkInfoRequest{})
	if err != nil {
		a.log.Errorf("GetNetworkInfo error: %v", err)
		return "", err
	}
	marshaller := jsonpb.Marshaler{}
	netInfoData, err := marshaller.MarshalToString(netInfo)
	if err != nil {
		a.log.Errorf("failed to marshal network info: %v", err)
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
			"network_info":    netInfoData,
		},
	}

	responseMap["payment_error"] = paymentResponse.PaymentError

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
func (a *Service) DecodePaymentRequest(paymentRequest string) (*data.InvoiceMemo, error) {
	a.log.Infof("DecodePaymentRequest %v", paymentRequest)
	lnclient := a.daemonAPI.APIClient()
	decodedPayReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
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
	lnclient := a.daemonAPI.APIClient()
	decodedPayReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Infof("Can't decode payment request: %v", paymentRequest)
		return nil, err
	}

	invoiceMemo := a.extractMemo(decodedPayReq)

	lookup, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHashStr: decodedPayReq.PaymentHash})
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
	lnclient := a.daemonAPI.APIClient()
	if err := a.syncSentPayments(); err != nil {
		a.log.Errorf("failed to sync payments: %v", err)
	}
	go func() {
		retry := 0
		for retry < 3 {
			if err := a.syncClosedChannels(); err != nil {
				a.log.Errorf("failed to sync closed chanels retry:%v error: %v", retry, err)
				time.Sleep(4 * time.Second)
				retry++
				continue
			}
			return
		}
	}()

	_, lastInvoiceSettledIndex := a.breezDB.FetchPaymentsSyncInfo()
	a.log.Infof("last invoice settled index ", lastInvoiceSettledIndex)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := lnclient.SubscribeInvoices(ctx, &lnrpc.InvoiceSubscription{SettleIndex: lastInvoiceSettledIndex})
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
	lnclient := a.daemonAPI.APIClient()
	lightningPayments, err := lnclient.ListPayments(context.Background(), &lnrpc.ListPaymentsRequest{})
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
	chanDB, chanDBCleanUp, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		a.log.Errorf("channeldbservice.Get(%v): %v", a.cfg.WorkingDir, err)
		return nil, fmt.Errorf("channeldbservice.Get(%v): %w", a.cfg.WorkingDir, err)
	}
	defer chanDBCleanUp()
	paymentControl := channeldb.NewPaymentControl(chanDB)
	lnclient := a.daemonAPI.APIClient()
	if a.daemonRPCReady() {
		channelsRes, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			return nil, err
		}
		chainInfo, chainErr := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			a.log.Errorf("Failed get chain info", chainErr)
			return nil, chainErr
		}

		for _, ch := range channelsRes.Channels {
			for _, htlc := range ch.PendingHtlcs {
				pendingItem, err := a.createPendingPayment(htlc, chainInfo.BlockHeight, paymentControl)
				if err != nil {
					return nil, err
				}
				payments = append(payments, pendingItem)
			}
		}
	}

	return payments, nil
}

func (a *Service) createPendingPayment(htlc *lnrpc.HTLC, currentBlockHeight uint32, pc *channeldb.PaymentControl) (*db.PaymentInfo, error) {
	paymentType := db.SentPayment
	if htlc.Incoming {
		paymentType = db.ReceivedPayment
	}

	var paymentRequest string
	var pendingPayment *channeldb.Payment
	lnclient := a.daemonAPI.APIClient()

	if htlc.Incoming {
		invoice, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: htlc.HashLock})
		if err != nil {
			a.log.Errorf("createPendingPayment - failed to call LookupInvoice %v", err)
			return nil, err
		}
		paymentRequest = invoice.PaymentRequest
	} else {
		payReqBytes, err := a.breezDB.FetchPaymentRequest(hex.EncodeToString(htlc.HashLock))
		if err != nil {
			a.log.Errorf("createPendingPayment - failed to call fetchPaymentRequest %v", err)
			return nil, err
		}
		paymentRequest = string(payReqBytes)
		pHash, err := lntypes.MakeHash(htlc.HashLock)
		if err != nil {
			return nil, err
		}
		pendingPayment, err = pc.FetchPayment(pHash)
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
		decodedReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
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
		if pendingPayment != nil {
			paymentData.CreationTimestamp = decodedReq.Timestamp
			paymentData.Amount = int64(pendingPayment.Info.Value.ToSatoshis())
			paymentData.CreationTimestamp = pendingPayment.Info.CreationDate.Unix()
		}
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
	lnclient := a.daemonAPI.APIClient()
	decodedReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: string(paymentRequest)})
	if err != nil {
		return err
	}
	if decodedReq.Destination == a.cfg.SwapperPubkey {
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
		Preimage:          paymentItem.PaymentPreimage,
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
		Preimage:          hex.EncodeToString(invoice.RPreimage),
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
