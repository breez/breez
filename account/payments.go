package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"time"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lspd"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing"
	"google.golang.org/grpc/status"
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
			IsKeySend:                  payment.IsKeySend,
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
func (a *Service) SendPaymentForRequest(paymentRequest string, amountSatoshi int64) (string, error) {
	a.log.Infof("sendPaymentForRequest: amount = %v", amountSatoshi)
	routing.DefaultShardMinAmt = 5000
	lnclient := a.daemonAPI.APIClient()
	decodedReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return "", err
	}
	if decodedReq.NumSatoshis == amountSatoshi {
		amountSatoshi = 0
	}
	if err := a.breezDB.SavePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return "", err
	}
	a.log.Infof("sendPaymentForRequest: before sending payment...")

	// At this stage we are ready to send asynchronously the payment through the daemon.
	return a.sendPayment(decodedReq.PaymentHash, &routerrpc.SendPaymentRequest{
		PaymentRequest: paymentRequest,
		TimeoutSeconds: 60,
		FeeLimitSat:    math.MaxInt64,
		MaxParts:       20,
		Amt:            amountSatoshi,
	})
}

// SendSpontaneousPayment send a payment without a payment request.
func (a *Service) SendSpontaneousPayment(destNode string, description string, amount int64) (string, error) {
	if err := a.waitReadyForPayment(); err != nil {
		return "", err
	}
	destBytes, err := hex.DecodeString(destNode)
	if err != nil {
		return "", err
	}
	req := &routerrpc.SendPaymentRequest{
		Dest:              destBytes,
		Amt:               amount,
		TimeoutSeconds:    60,
		FeeLimitSat:       math.MaxInt64,
		MaxParts:          20,
		DestCustomRecords: make(map[uint64][]byte),
	}

	// Since this is a spontaneous payment we need to generate the pre-image and hash by ourselves.
	var preimage lntypes.Preimage
	if _, err := rand.Read(preimage[:]); err != nil {
		return "", err
	}
	req.DestCustomRecords[record.KeySendType] = preimage[:]
	hash := preimage.Hash()
	req.PaymentHash = hash[:]

	// Also use the 'tip' key to set the description.
	req.DestCustomRecords[7629168] = []byte(description)
	hashStr := hex.EncodeToString(hash[:])
	if err := a.breezDB.SaveTipMessage(hashStr, []byte(description)); err != nil {
		return "", err
	}

	return a.sendPayment(hashStr, req)
}

func (a *Service) getMaxAmount(sendRequest *routerrpc.SendPaymentRequest) (uint64, error) {
	lnclient := a.daemonAPI.APIClient()
	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		a.log.Errorf("lnclient.ListChannels error: %v", err)
		return 0, fmt.Errorf("lnclient.ListChannels error: %w", err)
	}
	var totalMax uint64
	for _, c := range channels.Channels {
		a.log.Infof("cid: %v, active: %v, balance: %v, channnel reserve: %v", c.ChanId, c.Active, c.LocalBalance, c.LocalConstraints.ChanReserveSat)
		_, err := lnclient.QueryRoutes(context.Background(), &lnrpc.QueryRoutesRequest{
			PubKey:         hex.EncodeToString(sendRequest.Dest),
			Amt:            c.LocalBalance - int64(c.LocalConstraints.ChanReserveSat) + 1,
			OutgoingChanId: c.ChanId,
		})
		if err != nil {
			errStatus, _ := status.FromError(err)
			a.log.Infof("message: %v", errStatus.Message())
			var max uint64
			_, _ = fmt.Sscanf(errStatus.Message(), "insufficient local balance. Try to lower the amount to: %d mSAT", &max)
			a.log.Infof("max: %v", max)
			totalMax += max
		}
	}
	return totalMax, nil
}

func (a *Service) checkAmount(sendRequest *routerrpc.SendPaymentRequest) error {
	amt, _ := lnrpc.UnmarshallAmt(sendRequest.Amt, sendRequest.AmtMsat)
	max, err := a.getMaxAmount(sendRequest)
	if err != nil {
		a.log.Errorf("a.getMaxAmount error: %v", err)
		return fmt.Errorf("a.getMaxAmount error: %w", err)
	}
	if uint64(amt) > max {
		a.log.Errorf("insufficient balance: %v < %v", max, amt)
		return errors.New("insufficient balance")
	}
	return nil
}

func (a *Service) sendPayment(paymentHash string, sendRequest *routerrpc.SendPaymentRequest) (string, error) {

	if err := a.checkAmount(sendRequest); err != nil {
		a.log.Infof("sendPaymentAsync: error sending payment %v", err)
		return "", err
	}

	lnclient := a.daemonAPI.RouterClient()
	if err := a.waitReadyForPayment(); err != nil {
		a.log.Infof("sendPaymentAsync: error sending payment %v", err)
		return "", err
	}
	response, err := lnclient.SendPaymentV2(context.Background(), sendRequest)
	if err != nil {
		a.log.Infof("sendPaymentForRequest: error sending payment %v", err)
		return "", err
	}

	failureReason := lnrpc.PaymentFailureReason_FAILURE_REASON_NONE
	for {
		payment, err := response.Recv()
		if err != nil {
			return "", err
		}
		if payment.Status == lnrpc.Payment_IN_FLIGHT {
			continue
		}
		if payment.Status != lnrpc.Payment_SUCCEEDED {
			failureReason = payment.FailureReason
		}
		break
	}

	if failureReason != lnrpc.PaymentFailureReason_FAILURE_REASON_NONE {
		a.log.Infof("sendPaymentForRequest finished with error, %v", failureReason.String())
		traceReport, err := a.createPaymentTraceReport(sendRequest.PaymentRequest, sendRequest.AmtMsat, failureReason.String())
		if err != nil {
			a.log.Errorf("failed to create trace report for failed payment %v", err)
		}
		errorMsg := failureReason.String()
		if failureReason == lnrpc.PaymentFailureReason_FAILURE_REASON_NO_ROUTE {
			_, maxPay, _, err := a.getReceivePayLimit()
			if err == nil && maxPay-sendRequest.Amt < 50 {
				errorMsg += ". Try sending a smaller amount to keep the required minimum balance."
			}
		}
		return traceReport, errors.New(errorMsg)
	}
	a.log.Infof("sendPaymentForRequest finished successfully")
	go a.syncSentPayments()
	return "", nil
}

/*
AddInvoice encapsulate a given amount and description in a payment request
*/
func (a *Service) AddInvoice(invoiceRequest *data.AddInvoiceRequest) (paymentRequest string, lspFee int64, err error) {
	lnclient := a.daemonAPI.APIClient()

	// Format the standard invoice memo
	invoice := invoiceRequest.InvoiceDetails
	memo := formatTextMemo(invoice)

	if invoice.Expiry <= 0 {
		invoice.Expiry = defaultInvoiceExpiry
	}

	maxReceive, err := a.getMaxReceiveSingleChannel()
	if err != nil {
		a.log.Infof("failed to get account limits %v", err)
		return "", 0, err
	}

	// in case we don't need a new channel, we make sure the
	// existing channels are active.
	if maxReceive >= invoice.Amount {
		if err := a.waitReadyForPayment(); err != nil {
			return "", 0, err
		}
	}

	lspInfo := invoiceRequest.LspInfo
	if lspInfo == nil {
		return "", 0, errors.New("missing LSP information")
	}

	maxReceiveMsat := maxReceive * 1000
	amountMsat := invoice.Amount * 1000
	smallAmountMsat := amountMsat
	needOpenChannel := maxReceiveMsat < amountMsat
	var routingHints []*lnrpc.RouteHint

	// We need the LSP to open a channel.
	if needOpenChannel {

		fakeHints, err := a.getFakeChannelRoutingHint(lspInfo)
		if err != nil {
			return "", 0, err
		}
		routingHints = []*lnrpc.RouteHint{fakeHints}
		a.log.Infof("Generated zero-conf invoice for amount: %v", amountMsat)

		// Calculate the channel fee such that it's an integral number of sat.
		channelFeesMsat := amountMsat * lspInfo.ChannelFeePermyriad / 10_000 / 1_000 * 1_000
		a.log.Infof("zero-conf fee calculation: lsp fee rate (permyriad): %v, total fees for channel: %v",
			lspInfo.ChannelFeePermyriad, channelFeesMsat)

		smallAmountMsat = amountMsat - channelFeesMsat
	} else {
		if routingHints, err = a.getLSPRoutingHints(lspInfo); err != nil {
			return "", 0, fmt.Errorf("failed to get LSP routing hints %w", err)
		}
	}

	// create invoice with the lower amount.
	response, err := lnclient.AddInvoice(context.Background(), &lnrpc.Invoice{
		RPreimage: invoice.Preimage,
		Memo:      memo, ValueMsat: smallAmountMsat,
		Expiry: invoice.Expiry, RouteHints: routingHints,
	})
	if err != nil {
		return "", 0, err
	}
	if err := a.breezDB.AddZeroConfHash(response.RHash, []byte(response.PaymentRequest)); err != nil {
		return "", 0, fmt.Errorf("failed to add zero-conf invoice %w", err)
	}
	a.trackInvoice(response.RHash)
	a.log.Infof("Tracking invoice amount=%v, hash=%v", smallAmountMsat, response.RHash)

	payeeInvoice := response.PaymentRequest
	// create invoice with the larger amount and send to LSP the details.
	if needOpenChannel {
		var paymentAddress []byte
		payeeInvoice, paymentAddress, err = a.generateInvoiceWithNewAmount(response.PaymentRequest, amountMsat)
		if err != nil {
			return "", 0, fmt.Errorf("failed to generate LSP invoice %w", err)
		}
		a.log.Infof("Generated payee invoice: %v", payeeInvoice)
		lspInfo := invoiceRequest.LspInfo
		pubKey := lspInfo.LspPubkey

		if err := a.breezDB.AddZeroConfHash(response.RHash, []byte(payeeInvoice)); err != nil {
			return "", 0, fmt.Errorf("failed to add zero-conf invoice %w", err)
		}

		if err := a.registerPayment(response.RHash, paymentAddress, amountMsat, smallAmountMsat, pubKey, lspInfo.Id); err != nil {
			return "", 0, fmt.Errorf("failed to register payment with LSP %w", err)
		}
		a.log.Infof("Zero-conf payment registered: %v", string(response.RHash))
	}

	a.log.Infof("Generated Invoice: %v", payeeInvoice)
	return payeeInvoice, (amountMsat - smallAmountMsat) / 1_000, nil
}

func (a *Service) getFakeChannelRoutingHint(lspInfo *data.LSPInformation) (*lnrpc.RouteHint, error) {
	fakeChanID := &lnwire.ShortChannelID{BlockHeight: 1, TxIndex: 0, TxPosition: 0}
	return &lnrpc.RouteHint{
		HopHints: []*lnrpc.HopHint{
			{
				NodeId:                    lspInfo.Pubkey,
				ChanId:                    fakeChanID.ToUint64(),
				FeeBaseMsat:               uint32(lspInfo.BaseFeeMsat),
				FeeProportionalMillionths: uint32(lspInfo.FeeRate * 1000000),
				CltvExpiryDelta:           lspInfo.TimeLockDelta,
			},
		},
	}, nil
}

func (a *Service) getLSPRoutingHints(lspInfo *data.LSPInformation) ([]*lnrpc.RouteHint, error) {

	lnclient := a.daemonAPI.APIClient()
	channelsRes, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return nil, err
	}

	var hints []*lnrpc.RouteHint
	usedPeers := make(map[string]struct{})
	for _, h := range channelsRes.Channels {
		if _, ok := usedPeers[h.RemotePubkey]; ok {
			continue
		}
		ci, err := lnclient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{
			ChanId: h.ChanId,
		})
		if err != nil {
			a.log.Errorf("Unable to add routing hint for channel %v error=%v", h.ChanId, err)
			continue
		}
		remotePolicy := ci.Node1Policy
		if h.RemotePubkey == lspInfo.Pubkey && ci.Node2Pub == h.RemotePubkey {
			remotePolicy = ci.Node2Policy
		}

		// skip non lsp channels without remote policy
		if remotePolicy == nil && h.RemotePubkey != lspInfo.Pubkey {
			continue
		}

		feeBaseMsat := uint32(lspInfo.BaseFeeMsat)
		proportionalFee := uint32(lspInfo.FeeRate * 1000000)
		cltvExpiryDelta := lspInfo.TimeLockDelta
		if remotePolicy != nil {
			feeBaseMsat = uint32(remotePolicy.FeeBaseMsat)
			proportionalFee = uint32(remotePolicy.FeeRateMilliMsat)
			cltvExpiryDelta = remotePolicy.TimeLockDelta
		}
		a.log.Infof("adding routing hint = %v", h.RemotePubkey)
		hints = append(hints, &lnrpc.RouteHint{
			HopHints: []*lnrpc.HopHint{
				{
					NodeId:                    h.RemotePubkey,
					ChanId:                    h.ChanId,
					FeeBaseMsat:               feeBaseMsat,
					FeeProportionalMillionths: proportionalFee,
					CltvExpiryDelta:           cltvExpiryDelta,
				},
			},
		})
		usedPeers[h.RemotePubkey] = struct{}{}
	}
	return hints, nil
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

func (a *Service) createPaymentTraceReport(paymentRequest string, amount int64, errorMsg string) (string, error) {
	lnclient := a.daemonAPI.APIClient()

	var decodedPayReq *lnrpc.PayReq
	var err error
	if paymentRequest != "" {
		decodedPayReq, err = lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
		if err != nil {
			a.log.Errorf("DecodePaymentRequest error: %v", err)
			return "", err
		}
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

	if amount == 0 && decodedPayReq != nil {
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

	responseMap["payment_error"] = errorMsg

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

func (a *Service) GetPaymentRequestHash(paymentRequest string) (string, error) {
	lnclient := a.daemonAPI.APIClient()
	if lnclient == nil {
		return "", errors.New("daemon is not ready")
	}
	decodedPayReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		a.log.Errorf("DecodePaymentRequest error: %v", err)
		return "", err
	}
	return decodedPayReq.PaymentHash, nil
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
	a.log.Infof("last invoice settled index %v", lastInvoiceSettledIndex)
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
	var pendingPayment *channeldb.MPPayment
	lnclient := a.daemonAPI.APIClient()

	a.log.Infof("createPendingPayment")
	if htlc.Incoming {
		invoice, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: htlc.HashLock})
		if err != nil {
			a.log.Errorf("createPendingPayment - failed to call LookupInvoice %v", err)
			return nil, err
		}
		if invoice != nil {
			paymentRequest = invoice.PaymentRequest
		}
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
	}

	if pendingPayment != nil {
		paymentData.IsKeySend = len(pendingPayment.Info.PaymentRequest) == 0
		paymentData.PaymentHash = hex.EncodeToString(pendingPayment.Info.PaymentHash[:])
		paymentData.Amount = int64(pendingPayment.Info.Value.ToSatoshis())
		paymentData.CreationTimestamp = pendingPayment.Info.CreationTime.Unix()
	}

	return paymentData, nil
}

func (a *Service) onNewSentPayment(paymentItem *lnrpc.Payment) error {

	paymentData := &db.PaymentInfo{
		Type:              db.SentPayment,
		Amount:            paymentItem.Value,
		Fee:               paymentItem.Fee,
		CreationTimestamp: paymentItem.CreationDate,
		PaymentHash:       paymentItem.PaymentHash,
		Preimage:          paymentItem.PaymentPreimage,
	}

	if len(paymentItem.PaymentRequest) > 0 {
		invoiceMemo, err := a.DecodePaymentRequest(paymentItem.PaymentRequest)
		if err != nil {
			return err
		}

		paymentData.Description = invoiceMemo.Description
		paymentData.PayeeImageURL = invoiceMemo.PayeeImageURL
		paymentData.PayeeName = invoiceMemo.PayeeName
		paymentData.PayerImageURL = invoiceMemo.PayerImageURL
		paymentData.PayerName = invoiceMemo.PayerName
		paymentData.TransferRequest = invoiceMemo.TransferRequest

		lnclient := a.daemonAPI.APIClient()
		decodedReq, err := lnclient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: string(paymentItem.PaymentRequest)})
		if err != nil {
			return err
		}
		paymentData.Destination = decodedReq.Destination
		if decodedReq.Destination == a.cfg.SwapperPubkey {
			paymentData.Type = db.WithdrawalPayment
		}
	} else {
		paymentData.IsKeySend = true
		message, err := a.breezDB.FetchTipMessage(paymentItem.PaymentHash)
		if err != nil {
			return err
		}
		// pathLength := len(paymentItem.Path)
		// if pathLength > 0 {
		// 	paymentData.Destination = paymentItem.Path[pathLength-1]
		// }
		paymentData.Description = string(message)
	}

	swap, err := a.breezDB.FetchReverseSwap(paymentItem.PaymentHash)
	if err != nil {
		return err
	}
	if swap != nil {
		paymentData.RedeemTxID = swap.ClaimTxid
		paymentData.Amount = swap.OnchainAmount - swap.ClaimFee
		paymentData.Fee += paymentItem.Value - swap.OnchainAmount + swap.ClaimFee
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

	zeroConfPayreq, err := a.breezDB.FetchZeroConfInvoice(invoice.RHash)
	if err != nil {
		return err
	}
	var zeroConfMemo *data.InvoiceMemo
	if len(zeroConfPayreq) > 0 {
		if zeroConfMemo, err = a.DecodePaymentRequest(invoice.PaymentRequest); err != nil {
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
	if zeroConfMemo != nil {
		paymentData.Fee = zeroConfMemo.Amount - invoiceMemo.Amount
		if err = a.breezDB.RemoveZeroConfHash(invoice.RHash); err != nil {
			return err
		}
	}

	err = a.breezDB.AddAccountPayment(paymentData, invoice.SettleIndex, 0)
	if err != nil {
		a.log.Criticalf("Unable to add reveived payment : %v", err)
		return err
	}
	a.onServiceEvent(data.NotificationEvent{
		Type: data.NotificationEvent_INVOICE_PAID,
		Data: []string{invoice.PaymentRequest}})
	a.onAccountChanged()
	return nil
}

func (a *Service) registerPayment(paymentHash, paymentSecret []byte, incomingAmountMsat,
	outgoingAmountMsat int64, lspPubkey []byte, lspID string) error {

	destination, err := hex.DecodeString(a.daemonAPI.NodePubkey())
	if err != nil {
		a.log.Infof("hex.DecodeString(%v) error: %v", a.daemonAPI.NodePubkey(), err)
		return fmt.Errorf("hex.DecodeString(%v) error: %w", a.daemonAPI.NodePubkey(), err)
	}
	pi := &lspd.PaymentInformation{
		PaymentHash:        paymentHash,
		PaymentSecret:      paymentSecret,
		Destination:        destination,
		IncomingAmountMsat: incomingAmountMsat,
		OutgoingAmountMsat: outgoingAmountMsat,
	}
	data, err := proto.Marshal(pi)

	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()

	a.log.Infof("Register Payment pubkey = %v", lspPubkey)
	pubkey, err := btcec.ParsePubKey(lspPubkey, btcec.S256())
	if err != nil {
		a.log.Infof("btcec.ParsePubKey(%x) error: %v", lspPubkey, err)
		return fmt.Errorf("btcec.ParsePubKey(%x) error: %w", lspPubkey, err)
	}
	blob, err := btcec.Encrypt(pubkey, data)
	if err != nil {
		a.log.Infof("btcec.Encrypt(%x) error: %v", data, err)
		return fmt.Errorf("btcec.Encrypt(%x) error: %w", data, err)
	}

	_, err = c.RegisterPayment(ctx, &breezservice.RegisterPaymentRequest{LspId: lspID, Blob: blob})
	if err != nil {
		a.log.Infof("RegisterPayment() error: %v", err)
		return fmt.Errorf("RegisterPayment() error: %w", err)
	}
	return nil
}
