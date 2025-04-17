package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"

	lspd "github.com/breez/breez/lspd"
	"github.com/breez/lspd/btceclegacy"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightningnetwork/lnd/aliasmgr"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	defaultInvoiceExpiry       int64 = 3600
	invoiceCustomPartDelimiter       = " |\n"
	transferFundsRequest             = "Bitcoin Transfer"
	waitHtlcsSettledInterval         = time.Millisecond * 20
	waitHtlcsSettledTimeout          = time.Second * 5
)

var (
	errNoRoute = errors.New("no route")
)

// PaymentResponse is the response of a payment attempt.
type PaymentResponse struct {
	PaymentError string
	TraceReport  string
}

type hopInfo struct {
	policy *lnrpc.RoutingPolicy
	hint   *lnrpc.HopHint
	hop    *lnrpc.Hop
}

/*
GetPayments is responsible for retrieving the payment were made in this account
*/
func (a *Service) GetPayments() (*data.PaymentsList, error) {
	rawPayments, err := a.breezDB.FetchAllAccountPayments()
	if err != nil {
		return nil, err
	}

	pendingPayments, err := a.getPendingPayments(true)
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
			PendingFull:                payment.PendingFull,
			Preimage:                   payment.Preimage,
			ClosedChannelPoint:         payment.ClosedChannelPoint,
			IsChannelPending:           payment.Type == db.ClosedChannelPayment && payment.ClosedChannelStatus != db.ConfirmedClose,
			ClosedChannelTxID:          payment.ClosedChannelTxID,
			ClosedChannelRemoteTxID:    payment.ClosedChannelRemoteTxID,
			ClosedChannelSweepTxID:     payment.ClosedChannelSweepTxID,
			IsKeySend:                  payment.IsKeySend,
			GroupKey:                   payment.GroupKey,
			GroupName:                  payment.GroupName,
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
			if paymentItem.LnurlPayInfo, err = a.breezDB.FetchLNUrlPayInfo(payment.PaymentHash); err != nil {
				return nil, err
			}

			// Give old reverse swap payments the correct type if necessary.
			if swap, _ := a.breezDB.FetchReverseSwap(paymentItem.PaymentHash); swap != nil {
				paymentItem.Type = data.Payment_WITHDRAWAL
			}
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

func (a *Service) LSPActivity(lspList *data.LSPList) (*data.LSPActivity, error) {

	lnclient := a.daemonAPI.APIClient()
	if lnclient == nil {
		return nil, errors.New("daemon is not ready")
	}

	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		return nil, err
	}
	lspPubkey := make(map[string]string)
	connectedLsps := make(map[string]struct{})
	var exists = struct{}{}
	for _, lsp := range lspList.Lsps {
		lspPubkey[lsp.Pubkey] = lsp.Id
	}

	chanidChannel := make(map[uint64]*lnrpc.Channel)
	chanidLSP := make(map[uint64]string)
	for _, channel := range channels.Channels {
		chanidChannel[channel.ChanId] = channel
		if ID, ok := lspPubkey[channel.RemotePubkey]; ok {
			chanidLSP[channel.ChanId] = ID
			connectedLsps[ID] = exists
		}
	}

	lastPayments := make(map[string]int64)

	invoices, err := lnclient.ListInvoices(context.Background(), &lnrpc.ListInvoiceRequest{NumMaxInvoices: 100000})
	if err != nil {
		return nil, err
	}
	for _, invoice := range invoices.Invoices {
		if invoice.State != lnrpc.Invoice_SETTLED {
			continue
		}
		for _, htlc := range invoice.Htlcs {
			lsp := chanidLSP[htlc.ChanId]
			c, ok := chanidChannel[htlc.ChanId]
			if ok {
				if lsp == "" && c.ZeroConf && c.ZeroConfConfirmedScid == 0 &&
					len(invoice.RouteHints) > 0 && len(invoice.RouteHints[0].HopHints) > 0 {

					htlcLSP := lspPubkey[invoice.RouteHints[0].HopHints[0].NodeId]
					if _, ok := connectedLsps[htlcLSP]; ok {
						lsp = htlcLSP
						chanidLSP[htlc.ChanId] = lsp
					}
				}
			}
			if lsp != "" && lastPayments[lsp] < invoice.SettleDate {
				lastPayments[lsp] = invoice.SettleDate
			}
		}
	}

	payments, err := lnclient.ListPayments(context.Background(), &lnrpc.ListPaymentsRequest{MaxPayments: 100000})
	if err != nil {
		return nil, err
	}
	for _, payment := range payments.Payments {
		if payment.Status != lnrpc.Payment_SUCCEEDED {
			continue
		}
		for _, htlc := range payment.Htlcs {
			lsp := chanidLSP[htlc.Route.Hops[0].ChanId]
			htlcDate := time.Unix(0, htlc.ResolveTimeNs).Unix()
			if lsp != "" && lastPayments[lsp] < htlcDate {
				lastPayments[lsp] = htlcDate
			}
		}
	}

	return &data.LSPActivity{Activity: lastPayments}, nil
}

/*
SendPaymentForRequestV2 send the payment according to the details specified in the bolt 11 payment request.
If the payment was failed an error is returned
*/
func (a *Service) SendPaymentForRequestV2(paymentRequest string, amountSatoshi int64, lastHopPubkey []byte, fee int64) (string, error) {
	return a.sendPaymentForRequest(paymentRequest, amountSatoshi, lastHopPubkey, fee, true)
}

/*
SendPaymentForRequest send the payment according to the details specified in the bolt 11 payment request.
If the payment was failed an error is returned
*/
func (a *Service) SendPaymentForRequest(paymentRequest string, amountSatoshi int64, fee int64) (string, error) {
	return a.sendPaymentForRequest(paymentRequest, amountSatoshi, nil, fee, false)
}

func (a *Service) sendPaymentForRequest(paymentRequest string, amountSatoshi int64, lastHopPubkey []byte, fee int64, selfSplit bool) (string, error) {
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

	maxParts := uint32(20)
	if decodedReq.Features[uint32(lnwire.MPPOptional)] == nil &&
		decodedReq.Features[uint32(lnwire.MPPRequired)] == nil {
		maxParts = 1
	}

	feeLimitMsat := fee * 1000
	if feeLimitMsat < 0 {
		feeLimitMsat = math.MaxInt64
	}
	// At this stage we are ready to send asynchronously the payment through the daemon.
	var timeoutSeconds int32 = 60
	if useTor, _ := a.breezDB.GetTorActive(); useTor {
		/* If Tor is active, extend the timeout to avoid
		frequent payment timeout failures observed in testing.
		*/
		timeoutSeconds *= 2
	}

	if selfSplit {
		paymentHash, err := hex.DecodeString(decodedReq.PaymentHash)
		if err != nil {
			a.log.Errorf("Failed to decode payment hash: %v", err)
			return "", fmt.Errorf("failed to decode payment hash: %w", err)
		}
		return a.sendPaymentSelfSplit(paymentHash, decodedReq, feeLimitMsat, maxParts, amountSatoshi, lastHopPubkey)
	} else {
		return a.sendPayment(decodedReq.PaymentHash, decodedReq, &routerrpc.SendPaymentRequest{
			PaymentRequest: paymentRequest,
			TimeoutSeconds: timeoutSeconds,
			FeeLimitMsat:   feeLimitMsat,
			MaxParts:       maxParts,
			Amt:            amountSatoshi,
			LastHopPubkey:  lastHopPubkey,
		})
	}
}

func (a *Service) sendPaymentSelfSplit(paymentHash []byte, payReq *lnrpc.PayReq, feeLimitMsat int64, maxParts uint32, amountSat int64, lastHopPubkey []byte) (string, error) {
	routerclient := a.daemonAPI.RouterClient()
	if err := a.waitReadyForPayment(); err != nil {
		a.log.Infof("sendPaymentAsync: error sending payment %v", err)
		return "", err
	}
	lnclient := a.daemonAPI.APIClient()
	channelsResp, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		a.log.Errorf("lnclient.ListChannels error: %v", err)
		return "", fmt.Errorf("lnclient.ListChannels error: %w", err)
	}

	var amountMsat int64
	if amountSat == 0 {
		amountMsat = payReq.NumMsat
	} else {
		amountMsat = amountSat * 1000
	}

	channels := channelsResp.Channels

	// Try to deplete the smallest balance first.
	sort.SliceStable(channels, func(i, j int) bool {
		iCanSend, _ := channelConstraints(channels[i])
		jCanSend, _ := channelConstraints(channels[j])
		return iCanSend < jCanSend
	})

	var routes []*lnrpc.Route

	amountRemainingMsat := amountMsat
	for i, c := range channels {
		if amountRemainingMsat <= 0 {
			a.log.Infof("All routes computed, amount remaining %v msat on channel (%d/%d)", amountRemainingMsat, i+1, len(channels))
			break
		}

		thisChannelCanSendSat, minHtlcMsat := channelConstraints(c)

		var route *lnrpc.Route

		// if the amount exceeds the amount remaining, we can simply query the route.
		if thisChannelCanSendSat*1000 >= amountRemainingMsat {
			a.log.Infof("Channel %v can send %v msat, amount remaining %v msat, querying route for %v", c.ChanId, thisChannelCanSendSat*1000, amountRemainingMsat, amountRemainingMsat)
			queriedRoutes, err := lnclient.QueryRoutes(context.Background(), &lnrpc.QueryRoutesRequest{
				PubKey:         payReq.Destination,
				AmtMsat:        amountRemainingMsat,
				OutgoingChanId: c.ChanId,
				RouteHints:     payReq.RouteHints,
				LastHopPubkey:  lastHopPubkey,
				FinalCltvDelta: max(144, int32(payReq.CltvExpiry)),
			})
			if err == nil && queriedRoutes != nil && len(queriedRoutes.Routes) > 0 {
				route = queriedRoutes.Routes[0]

				// Make sure to add the MPP record.
				route.Hops[len(route.Hops)-1].MppRecord = &lnrpc.MPPRecord{
					PaymentAddr:  payReq.PaymentAddr,
					TotalAmtMsat: amountMsat,
				}

				routes = append(routes, route)

				// All the funds have a route now.
				amountRemainingMsat = 0
				continue
			}

			// If this fails, try finding a path ourselves anyway below.
			a.log.Infof("lnclient.QueryRoutes error: %v", err)
		}

		// If the amount is above the current channel amount, create a route using the maximum available amount.
		a.log.Infof("Forward pathfinding for channel %v. Can send %v msat, amount remaining %v msat", c.ChanId, thisChannelCanSendSat*1000, amountRemainingMsat)
		currentChanAmountMsat := min(thisChannelCanSendSat*1000, amountRemainingMsat)
		if currentChanAmountMsat <= int64(minHtlcMsat) {
			a.log.Infof("Channel %v can only send %v msat, which is less than minHtlcMsat %v", c.ChanId, currentChanAmountMsat, minHtlcMsat)
			continue
		}
		hops, totalTimeLock, err := a.getHops(c, payReq.Destination, payReq.RouteHints, lastHopPubkey, max(144, int32(payReq.CltvExpiry)))
		if err != nil {
			a.log.Infof("getHops error: %v", err)
			continue
		}

		a.log.Debugf("Got route with %v hops", len(hops))
		route = &lnrpc.Route{
			TotalTimeLock: totalTimeLock,
			TotalAmtMsat:  currentChanAmountMsat,
		}

		// Now build the route from the hops. The Route struct is very counterintuitive.
		currentHopAmountMsat := currentChanAmountMsat
		for _, hop := range hops {
			feeMsat := computeFeeForNextHop(hop, currentHopAmountMsat)
			currentHopAmountMsat -= feeMsat
			route.Hops = append(route.Hops, &lnrpc.Hop{
				ChanId:           hop.hop.ChanId,
				Expiry:           hop.hop.Expiry,
				AmtToForwardMsat: currentHopAmountMsat,
				FeeMsat:          feeMsat,
				PubKey:           hop.hop.PubKey,
			})
		}

		// The remaining currentHopAmountMsat is the destination amount.
		amountRemainingMsat -= currentHopAmountMsat

		// Add the mpp record.
		route.Hops[len(route.Hops)-1].MppRecord = &lnrpc.MPPRecord{
			PaymentAddr:  payReq.PaymentAddr,
			TotalAmtMsat: amountMsat,
		}

		routes = append(routes, route)
	}

	if amountRemainingMsat > 0 {
		return "", fmt.Errorf("amount %v msat could not be sent, only %v msat could be routed", amountMsat, amountMsat-amountRemainingMsat)
	}

	var mtx sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(routes))
	var errors []error
	havePreimage := false
	for i, r := range routes {
		go func(index int, route *lnrpc.Route) {
			defer wg.Done()
			a.log.Infof("Sending partial payment %d/%d", index+1, len(routes))
			a.log.Debugf("route: %+v", route)
			attempt, err := routerclient.SendToRouteV2(context.Background(), &routerrpc.SendToRouteRequest{
				PaymentHash: paymentHash,
				Route:       route,
			})

			mtx.Lock()
			defer mtx.Unlock()
			if err != nil {
				a.log.Infof("Partial payment %d/%d returned error: %v", index+1, len(routes), err)
				errors = append(errors, err)
				return
			}
			if attempt.Failure != nil {
				a.log.Infof("Partial payment %d/%d returned failure %v at index %v", index+1, len(routes), attempt.Failure.Code, attempt.Failure.FailureSourceIndex)
				errors = append(errors, fmt.Errorf(attempt.Failure.Code.String()))
			}
			emptyPreimage := [32]byte{}
			if attempt.Preimage != nil && len(attempt.Preimage) == 32 && !bytes.Equal(attempt.Preimage, emptyPreimage[:]) {
				a.log.Infof("Partial payment %d/%d returned preimage %x", index+1, len(routes), attempt.Preimage)
				havePreimage = true
			}
		}(i, r)
	}

	wg.Wait()
	if havePreimage {
		a.log.Infof("sendPaymentForRequest finished successfully")
		a.syncSentPayments()
		return "", nil
	}

	if len(errors) == 0 {
		return "", fmt.Errorf("payment completed with no errors, but no preimage was returned")
	}

	return "", fmt.Errorf("partial payment failed: %v", errors[0])
}

// SendSpontaneousPayment send a payment without a payment request.
func (a *Service) SendSpontaneousPayment(destNode string,
	description string, amount int64, feeLimitMSat int64,
	groupKey, groupName string, tlv map[int64]string) (string, error) {

	destBytes, err := hex.DecodeString(destNode)
	if err != nil {
		return "", err
	}
	feeLimit := feeLimitMSat
	if feeLimit == 0 {
		feeLimit = math.MaxInt64
	}
	req := &routerrpc.SendPaymentRequest{
		Dest:              destBytes,
		Amt:               amount,
		TimeoutSeconds:    60,
		FeeLimitMsat:      feeLimit,
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
	features := []lnrpc.FeatureBit{
		lnrpc.FeatureBit_TLV_ONION_OPT,
		lnrpc.FeatureBit_PAYMENT_ADDR_REQ,
		lnrpc.FeatureBit_MPP_OPT,
	}
	req.DestFeatures = features

	// Also use the 'tip' key to set the description.
	req.DestCustomRecords[7629171] = []byte(description)
	hashStr := hex.EncodeToString(hash[:])
	if err := a.breezDB.SaveTipMessage(hashStr, []byte(description)); err != nil {
		return "", err
	}

	for k, v := range tlv {
		a.log.Infof("adding custom record %v, value: %v", uint64(k), v)
		req.DestCustomRecords[uint64(k)] = []byte(v)
	}

	if groupKey != "" {
		if err := a.breezDB.SavePaymentGroup(hashStr, []byte(groupKey), []byte(groupName)); err != nil {
			return "", err
		}
	}

	return a.sendPayment(hashStr, nil, req)
}

func (a *Service) GetMaxAmount(destination string, routeHints []*lnrpc.RouteHint, lastHopPubkey []byte) (uint64, error) {
	a.waitReadyForPayment()
	return a.getMaxAmount(destination, routeHints, lastHopPubkey)
}

func (a *Service) getMaxAmount(destination string, routeHints []*lnrpc.RouteHint, lastHopPubkey []byte) (uint64, error) {
	a.log.Infof("destination: %v, routeHints: %#v (len: %v), lastHopPubkey: %x", destination, routeHints, len(routeHints), lastHopPubkey)
	lnclient := a.daemonAPI.APIClient()
	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		a.log.Errorf("lnclient.ListChannels error: %v", err)
		return 0, fmt.Errorf("lnclient.ListChannels error: %w", err)
	}

	var totalMax uint64
OUTER:
	for _, c := range channels.Channels {
		// Calculate how much can be sent through this channel after accounting for channel reserve
		thisChannelCanSendSat, minHtlcMsat := channelConstraints(c)

		if thisChannelCanSendSat*1000 <= int64(minHtlcMsat) {
			continue
		}

		a.log.Infof("cid: %v, active: %v, balance: %v, can send: %v, channel reserve: %v, minHtlcMsat: %v", c.ChanId, c.Active, c.LocalBalance, thisChannelCanSendSat, c.LocalConstraints.ChanReserveSat, minHtlcMsat)

		hops, _, err := a.getHops(c, destination, routeHints, lastHopPubkey, 9)
		if err != nil {
			a.log.Infof("getHops error: %v", err)
			continue
		}
		a.log.Debugf("Got route with %v hops", len(hops))
		destinationAmtMsat := thisChannelCanSendSat * 1000
		// Skip the first hop for fees, because that's our own channel.
		for i := 1; i < len(hops); i++ {
			feeMsat := computeFeeForNextHop(hops[i], destinationAmtMsat)
			destinationAmtMsat = destinationAmtMsat - feeMsat
			if destinationAmtMsat <= 0 {
				a.log.Debugf("Skipping channel %v because fees exceed channel balance.")
				continue OUTER
			}
		}

		a.log.Debugf("Adding %v to total sendable amount for channel %v", destinationAmtMsat, c.ChanId)
		totalMax += uint64(destinationAmtMsat)
	}
	return totalMax, nil
}

func channelConstraints(c *lnrpc.Channel) (int64, int64) {
	// Calculate how much can be sent through this channel after accounting for channel reserve
	thisChannelCanSendSat := c.LocalBalance - int64(c.LocalConstraints.ChanReserveSat)

	// If we're the initiator, account for commitment and HTLC transaction fees
	if c.Initiator {
		buffer := (c.CommitWeight + input.HTLCWeight) * 2 * c.FeePerKw / 1000
		if buffer < thisChannelCanSendSat {
			thisChannelCanSendSat -= buffer
		} else {
			thisChannelCanSendSat = 0
		}
	}

	minHtlcMsat := max(c.LocalConstraints.MinHtlcMsat, 1000)

	return thisChannelCanSendSat, int64(minHtlcMsat)
}

func (a *Service) getHops(c *lnrpc.Channel, destination string, routeHints []*lnrpc.RouteHint, lastHopPubkey []byte, finalCltvDelta int32) ([]*hopInfo, uint32, error) {
	_, minHtlcMsat := channelConstraints(c)
	lnclient := a.daemonAPI.APIClient()
	queriedRoutes, err := lnclient.QueryRoutes(context.Background(), &lnrpc.QueryRoutesRequest{
		PubKey:         destination,
		AmtMsat:        int64(minHtlcMsat),
		OutgoingChanId: c.ChanId,
		RouteHints:     routeHints,
		LastHopPubkey:  lastHopPubkey,
		FinalCltvDelta: finalCltvDelta,
	})

	if err != nil {
		a.log.Infof("lnclient.QueryRoutes error: %v", err)
		return nil, 0, errNoRoute
	}

	if queriedRoutes == nil || len(queriedRoutes.Routes) == 0 {
		a.log.Debugf("no route on channel %v", c.ChanId)
		return nil, 0, errNoRoute
	}

	queriedRoute := queriedRoutes.Routes[0]
	var prevPubkey string = a.daemonAPI.NodePubkey()
	var hops []*hopInfo
	// Iterate over all the hops in the route to find their policies
HOPS:
	for _, hop := range queriedRoute.Hops {
		// If the hop is from a route hint, use the hint.
		for _, hint := range routeHints {
			for _, hintHop := range hint.HopHints {
				if hintHop.ChanId == hop.ChanId {
					hops = append(hops, &hopInfo{
						hint: hintHop,
						hop:  hop,
					})
					a.log.Infof("added hop from hop hint: %v", hintHop.ChanId)
					continue HOPS
				}
			}
		}

		// Otherwise look up the routing policy.
		chanInfo, err := lnclient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{
			ChanId: hop.ChanId,
		})
		if err != nil {
			a.log.Errorf("Failed to get chaninfo (%v) for hop in route: %v", hop.ChanId, err)
			return nil, 0, fmt.Errorf("failed to get chaninfo (%v) for hop in route: %w", hop.ChanId, err)
		}

		// Find the policy in the right direction.
		var policy *lnrpc.RoutingPolicy
		if chanInfo.Node1Pub == prevPubkey {
			policy = chanInfo.Node1Policy
			prevPubkey = chanInfo.Node2Pub
		} else if chanInfo.Node2Pub == prevPubkey {
			policy = chanInfo.Node2Policy
			prevPubkey = chanInfo.Node1Pub
		} else {
			a.log.Errorf("Failed to get policy for hop (%v): %v", hop.ChanId, err)
			return nil, 0, fmt.Errorf("failed to get policy for hop (%v): %w", hop.ChanId, err)
		}

		hops = append(hops, &hopInfo{
			policy: policy,
			hop:    hop,
		})
	}

	if len(hops) == 0 {
		a.log.Errorf("Got route without hops for %v, chanid %v", destination, c.ChanId)
		return nil, 0, errNoRoute
	}

	return hops, queriedRoute.TotalTimeLock, nil
}

func computeFeeForNextHop(hop *hopInfo, amountMsat int64) int64 {
	if hop == nil {
		return 0
	}

	var feeBaseMsat int64
	var feePpm int64
	if hop.policy != nil {
		feeBaseMsat = hop.policy.FeeBaseMsat
		feePpm = hop.policy.FeeRateMilliMsat
	}
	if hop.hint != nil {
		feeBaseMsat = int64(hop.hint.FeeBaseMsat)
		feePpm = int64(hop.hint.FeeProportionalMillionths)
	}

	// Calculate the fee backwards, normally the fee is calculated on top
	// of the outgoing amount. Now we compute the fee from the incoming amount.
	return (((amountMsat - feeBaseMsat) * 1_000_000) / (1_000_000 + feePpm)) / 1_000_000
}

func (a *Service) sendPayment(paymentHash string, payReq *lnrpc.PayReq, sendRequest *routerrpc.SendPaymentRequest) (string, error) {

	routerclient := a.daemonAPI.RouterClient()
	if err := a.waitReadyForPayment(); err != nil {
		a.log.Infof("sendPaymentAsync: error sending payment %v", err)
		return "", err
	}

	if payReq != nil && len(payReq.RouteHints) == 1 && len(payReq.RouteHints[0].HopHints) == 1 {
		xImportMissionControl(payReq, routerclient)
	}
	a.log.Infof("sending payment with max fee = %v msat", sendRequest.FeeLimitMsat)
	response, err := routerclient.SendPaymentV2(context.Background(), sendRequest)
	if err != nil {
		a.log.Infof("sendPaymentForRequest: error sending payment %v", err)
		return "", err
	}

	failureReason := lnrpc.PaymentFailureReason_FAILURE_REASON_NONE
	var payment *lnrpc.Payment
	for {
		payment, err = response.Recv()
		if err != nil {
			a.log.Infof("Payment event error received %v", err)
			return "", err
		}
		a.log.Infof("Payment event received %v", payment.Status)
		if payment.Status == lnrpc.Payment_IN_FLIGHT {
			if len(payment.Htlcs) > 0 {
				attempt := payment.Htlcs[len(payment.Htlcs)-1]
				if attempt.Route != nil {
					var hops []string
					for _, hop := range attempt.Route.Hops {
						scid := lnwire.NewShortChanIDFromInt(hop.ChanId)
						hops = append(hops, scid.String())
					}
					a.log.Infof("Route used: %s", strings.Join(hops, "->"))
				}
				if attempt.Failure != nil {
					a.log.Infof("Htlc attempt %d failed with code %v from index %d", attempt.AttemptId, attempt.Failure.Code, attempt.Failure.FailureSourceIndex)
				}
			}
			continue
		}
		if payment.Status != lnrpc.Payment_SUCCEEDED {
			failureReason = payment.FailureReason
		}
		break
	}

	paymenthash, err := hex.DecodeString(payment.PaymentHash)
	if err != nil {
		a.log.Errorf("Failed to decode payment hash after payment succeeded: %+v", payment)
		return "", fmt.Errorf("failed to decode payment hash: %w", err)
	}

	// The payment has completed, but there may still be htlcs that have to be
	// revoked. Wait for revocation before notifying payment success/failure.
	lnclient := a.daemonAPI.APIClient()
	timeout := time.Now().Add(waitHtlcsSettledTimeout)
out:
	for {
		hasPendingHtlcs := false
		channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			a.log.Errorf("ListChannels error: %v", err)
			return "", err
		}

	retry:
		for _, c := range channels.Channels {
			for _, htlc := range c.PendingHtlcs {
				if bytes.Equal(htlc.HashLock, paymenthash) {
					a.log.Debugf("HTLC still pending on chan %v. Waiting to finalize.", c.ChanId)
					hasPendingHtlcs = true
					break retry
				}
			}
		}

		if !hasPendingHtlcs {
			break
		}

		select {
		case <-time.After(waitHtlcsSettledInterval):
		case <-time.After(time.Until(timeout)):
			a.log.Warnf("Timed out waiting for htlcs to finalize. Sending notification anyway.")
			break out
		}
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
	a.syncSentPayments()
	// a.notifyPaymentResult(true, sendRequest.PaymentRequest, paymentHash, "", "")
	return "", nil
}

func xImportMissionControl(payReq *lnrpc.PayReq, routerclient routerrpc.RouterClient) {
	nodeFrom, err := hex.DecodeString(payReq.RouteHints[0].HopHints[0].NodeId)
	if err != nil {
		return
	}
	nodeTo, err := hex.DecodeString(payReq.Destination)
	if err != nil {
		return
	}
	routerclient.XImportMissionControl(context.Background(), &routerrpc.XImportMissionControlRequest{
		Pairs: []*routerrpc.PairHistory{{
			NodeFrom: nodeFrom,
			NodeTo:   nodeTo,
			History: &routerrpc.PairData{
				SuccessTime:    time.Now().UnixNano(),
				SuccessAmtMsat: payReq.NumMsat,
			},
		}}})
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
		a.log.Infof("Generated zero-conf invoice for amount: %v sats", invoice.Amount)

		var channelFeesMsat int64

		if invoiceRequest.OpeningFeeParams == nil {
			// TODO: This branch only exists because there may be existing swaps
			// that use the old fee rate mechanism. Remove this after those
			// swaps have expired.
			channelFeesMsat = amountMsat * lspInfo.ChannelFeePermyriad / 10_000 / 1_000 * 1_000
			if channelFeesMsat < lspInfo.ChannelMinimumFeeMsat {
				channelFeesMsat = lspInfo.ChannelMinimumFeeMsat
			}
			a.log.Infof("zero-conf fee calculation: lsp fee rate (permyriad): %v (minimum %v sats), total fees for channel: %v sats",
				lspInfo.ChannelFeePermyriad, lspInfo.ChannelMinimumFeeMsat/1_000, channelFeesMsat/1_000)

			if amountMsat < channelFeesMsat+1000 {
				return "", 0, fmt.Errorf("amount %v sats should be more than the minimum fees (%v sats)", invoice.Amount, lspInfo.ChannelMinimumFeeMsat/1_000)
			}
		} else {
			// Calculate the channel fee such that it's an integral number of sat.
			channelFeesMsat = amountMsat * int64(invoiceRequest.OpeningFeeParams.Proportional) / 1_000_000 / 1_000 * 1_000
			if channelFeesMsat < int64(invoiceRequest.OpeningFeeParams.MinMsat) {
				channelFeesMsat = int64(invoiceRequest.OpeningFeeParams.MinMsat)
			}
			a.log.Infof("zero-conf fee calculation option: lsp fee rate (proportional): %v (minimum %v sats), total fees for channel: %v sats",
				invoiceRequest.OpeningFeeParams.Proportional, invoiceRequest.OpeningFeeParams.MinMsat/1_000, channelFeesMsat/1_000)

			if amountMsat < channelFeesMsat+1000 {
				return "", 0, fmt.Errorf("amount %v sats should be more than the minimum fees (%v sats)", invoice.Amount, invoiceRequest.OpeningFeeParams.MinMsat/1_000)
			}
		}

		smallAmountMsat = amountMsat - channelFeesMsat
	} else {
		if routingHints, err = a.getLSPRoutingHints(lspInfo); err != nil {
			return "", 0, fmt.Errorf("failed to get LSP routing hints %w", err)
		}
	}

	if len(routingHints) == 0 {
		return "", 0, errors.New("no routing information")
	}

	var payeeInvoice string
	var payeeInvoiceHash []byte
	// check if inner invoice exists
	if invoice.Preimage != nil {
		preImage, err := lntypes.MakePreimage(invoice.Preimage)
		if err != nil {
			return "", 0, fmt.Errorf("failed to create preimage %w", err)
		}

		hash := preImage.Hash()
		payeeInvoiceHash = hash[:]
		existingInvoice, _ := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: payeeInvoiceHash})
		if existingInvoice != nil {
			payeeInvoice = existingInvoice.PaymentRequest
			a.log.Infof("found and reusing existing invoice with given hash")
		}
	}

	if payeeInvoice == "" {
		// create invoice with the lower amount.
		response, err := lnclient.AddInvoice(context.Background(), &lnrpc.Invoice{
			RPreimage: invoice.Preimage,
			Memo:      memo, ValueMsat: smallAmountMsat,
			Expiry: invoice.Expiry, RouteHints: routingHints,
		})
		if err != nil {
			return "", 0, err
		}
		payeeInvoice = response.PaymentRequest
		payeeInvoiceHash = response.RHash
		if err := a.breezDB.AddZeroConfHash(payeeInvoiceHash, []byte(response.PaymentRequest)); err != nil {
			return "", 0, fmt.Errorf("failed to add zero-conf invoice %w", err)
		}
		a.log.Infof("Tracking invoice amount=%v, hash=%v", smallAmountMsat, payeeInvoiceHash)
	}

	// create invoice with the larger amount and send to LSP the details.
	if needOpenChannel {
		var paymentAddress []byte
		payeeInvoice, paymentAddress, err = a.generateInvoiceWithNewAmount(payeeInvoice, amountMsat)
		if err != nil {
			return "", 0, fmt.Errorf("failed to generate LSP invoice %w", err)
		}
		a.log.Infof("Generated payee invoice: %v", payeeInvoice)
		lspInfo := invoiceRequest.LspInfo
		pubKey := lspInfo.LspPubkey

		existingZeroInvoice, err := a.breezDB.FetchZeroConfInvoice(payeeInvoiceHash)
		if err != nil {
			return "", 0, fmt.Errorf("failed to fetch zero-conf invoice %w", err)
		}
		if existingZeroInvoice == nil || string(existingZeroInvoice) != payeeInvoice {
			if err := a.registerPayment(payeeInvoiceHash, paymentAddress, amountMsat, smallAmountMsat, pubKey, lspInfo.Id, invoiceRequest.OpeningFeeParams); err != nil {
				return "", 0, fmt.Errorf("failed to register payment with LSP %w", err)
			}
			if err := a.breezDB.AddZeroConfHash(payeeInvoiceHash, []byte(payeeInvoice)); err != nil {
				return "", 0, fmt.Errorf("failed to add zero-conf invoice %w", err)
			}
		}

		a.log.Infof("Zero-conf payment registered: %v", string(payeeInvoiceHash))
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

	chanDB, chanDBCleanUp, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("channeldbservice.Get(%v): %w", a.cfg.WorkingDir, err)
	}
	defer chanDBCleanUp()

	openChannels, err := chanDB.ChannelStateDB().FetchAllOpenChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch all opened channels %v", err)
	}

	linkUpdater := func(shortID lnwire.ShortChannelID) error {
		return nil
	}

	aliasManager, err := aliasmgr.NewManager(chanDB.Backend, linkUpdater)
	if err != nil {
		return nil, fmt.Errorf("error in aliasmgr.NewManager: %w", err)
	}

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
		hintID, err := a.getChannelLSPHint(openChannels, aliasManager, h)
		if err != nil {
			return nil, fmt.Errorf("failed to get lsp route hint for channel %v: %v", h.ChanId, err)
		}

		hints = append(hints, &lnrpc.RouteHint{
			HopHints: []*lnrpc.HopHint{
				{
					NodeId:                    h.RemotePubkey,
					ChanId:                    hintID,
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

func (a *Service) getChannelLSPHint(openChannels []*channeldb.OpenChannel, aliasManager *aliasmgr.Manager, h *lnrpc.Channel) (uint64, error) {
	hintID := lnwire.NewShortChanIDFromInt(h.ChanId)

	var dbChannel *channeldb.OpenChannel
	for _, c := range openChannels {
		if c.ShortChannelID.ToUint64() == h.ChanId {
			dbChannel = c
			break
		}
	}

	if dbChannel != nil && dbChannel.NegotiatedAliasFeature() {
		var err error
		hintID, err = aliasManager.GetPeerAlias(lnwire.NewChanIDFromOutPoint(dbChannel.FundingOutpoint))
		if err != nil {
			return 0, fmt.Errorf("error in aliasmgr.GetPeerAlias: %w", err)
		}
	}
	return hintID.ToUint64(), nil
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
	netInfoData, err := protojson.Marshal(netInfo)
	if err != nil {
		a.log.Errorf("failed to marshal network info: %v", err)
		return "", err
	}

	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		a.log.Errorf("ListChannels error: %v", err)
		return "", err
	}
	chanData, err := protojson.Marshal(channels)
	if err != nil {
		a.log.Errorf("failed to marshal channels info: %v", err)
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
			"network_info":    string(netInfoData),
			"channels":        string(chanData),
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
	a.log.Infof("GetPaymentRequestHash %v", paymentRequest)
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
	a.log.Infof("GetRelatedInvoice: %s", decodedPayReq.PaymentHash)

	invoiceMemo := a.extractMemo(decodedPayReq)

	lookup, err := lnclient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHashStr: decodedPayReq.PaymentHash})
	if err != nil {
		a.log.Infof("GetRelatedInvoice: LookupInvoice failed.")
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
			if invoice.State == lnrpc.Invoice_SETTLED {
				err := a.waitHtlcsSettled(ctx, invoice)
				if err != nil {
					// NOTE: On error, we'll notify invoice settled anyway.
					// Not returning here.
					a.log.Errorf("Failed wait for htlc settlement: %v", err)
				}

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

func (a *Service) waitHtlcsSettled(ctx context.Context, invoice *lnrpc.Invoice) error {
	lnclient := a.daemonAPI.APIClient()
	routerclient := a.daemonAPI.RouterClient()

	subctx, cancel := context.WithCancel(ctx)
	defer cancel()

	htlcclient, err := routerclient.SubscribeHtlcEvents(
		subctx,
		&routerrpc.SubscribeHtlcEventsRequest{},
	)
	if err != nil {
		return err
	}

	// Get the subscribed event so htlc lookups are reliable.
	for {
		ev, err := htlcclient.Recv()
		if err != nil {
			return err
		}

		if ev.GetSubscribedEvent() != nil {
			break
		}
	}

	a.log.Debugf("Invoice %x: lookup state for %v htlcs", invoice.RHash, len(invoice.Htlcs))
	unsettled := make(map[string]*lnrpc.InvoiceHTLC, len(invoice.Htlcs))
	for _, h := range invoice.Htlcs {
		_, err := lnclient.LookupHtlcResolution(subctx, &lnrpc.LookupHtlcResolutionRequest{
			ChanId:    h.ChanId,
			HtlcIndex: h.HtlcIndex,
		})

		if err == nil {
			// No error means the htlc is finalized (either failed or settled).
			continue
		}

		if !strings.Contains(err.Error(), "htlc unknown") {
			return fmt.Errorf("failed to lookup htlc: %w", err)
		}

		unsettled[fmt.Sprintf("%vx%v", h.ChanId, h.HtlcIndex)] = h
	}

	for len(unsettled) > 0 {
		a.log.Debugf(
			"Invoice %x: Waiting for htlcs to settle. %v remaining.",
			invoice.RHash,
			len(unsettled),
		)
		h, err := htlcclient.Recv()
		if err != nil {
			return fmt.Errorf("htlcclient.Recv(): %w", err)
		}

		f := h.GetFinalHtlcEvent()
		if f == nil {
			continue
		}

		delete(unsettled, fmt.Sprintf("%vx%v", h.IncomingChannelId, h.IncomingHtlcId))
	}

	a.log.Debugf("Invoice %x: All htlcs settled.", invoice.RHash)
	return nil
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
		// We go back up to 36 hours to make sure we sync payments that were pending
		// for a long time before we update the sync time
		if paymentItem.CreationDate <= lastPaymentTime-60*60*36 {
			continue
		}
		a.log.Infof("syncSentPayments adding an outgoing payment")
		a.onNewSentPayment(paymentItem)
	}

	return nil

	//TODO delete history of payment requests after the new payments API stablized.
}

func (a *Service) getPendingPayments(includeInflight bool) ([]*db.PaymentInfo, error) {
	var payments []*db.PaymentInfo
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

		inflightPayments, err := a.getInflightPaymentsMap()
		if err != nil {
			return nil, err
		}
		pendingByHash := make(map[string]*db.PaymentInfo)
		for _, ch := range channelsRes.Channels {
			for _, htlc := range ch.PendingHtlcs {
				pendingItem, err := a.createPendingPayment(htlc, chainInfo.BlockHeight, inflightPayments)
				if err != nil {
					return nil, err
				}

				pendingSoFar, ok := pendingByHash[pendingItem.PaymentHash]
				currentInflight, hasInflight := inflightPayments[pendingItem.PaymentHash]
				if !ok {
					pendingByHash[pendingItem.PaymentHash] = pendingItem
					pendingSoFar = pendingItem

					// we have an in flight payment, let's get all the htlc and sum them.
					if hasInflight {
						pendingSoFar.Fee = 0
						pendingSoFar.Amount = 0
						for _, ht := range currentInflight.Htlcs {
							if ht.Status != lnrpc.HTLCAttempt_FAILED {
								a.log.Infof("pendingPaymets: adding fee %v and amount %v", ht.Route.TotalFees, ht.Route.TotalAmt)
								pendingSoFar.Fee += ht.Route.TotalFees
								pendingSoFar.Amount += (ht.Route.TotalAmt - ht.Route.TotalFees)
							}
						}
						a.log.Infof("pendingPaymets: hasInflight=%v, pendingSoFar.Amount=%v, currentInflight.ValueSat=%v",
							hasInflight, pendingSoFar.Amount, currentInflight.ValueSat)
						if pendingSoFar.Amount == currentInflight.ValueSat {
							pendingSoFar.PendingFull = true
						}
						pendingSoFar.Amount = currentInflight.ValueSat
					}
				}
			}
		}

		if includeInflight {
			// add pending payments that represents in flight outgoing payments
			// without any htlcs.
			for hash, inFlight := range inflightPayments {
				if _, ok := pendingByHash[hash]; !ok {
					hashBytes, err := hex.DecodeString(inFlight.PaymentHash)
					if err != nil {
						return nil, err
					}
					if time.Now().Sub(time.Unix(inFlight.CreationDate, 0)) < time.Second*60 {
						payment, err := a.createPendingPayment(&lnrpc.HTLC{
							HashLock:         hashBytes,
							Incoming:         false,
							ExpirationHeight: chainInfo.BlockHeight + 144,
							Amount:           inFlight.ValueSat},
							chainInfo.BlockHeight, inflightPayments)
						if err != nil {
							return nil, err
						}
						pendingByHash[inFlight.PaymentHash] = payment
					}
				}
			}
		}

		for h, p := range pendingByHash {
			groupKey, groupName, err := a.breezDB.FetchPaymentGroup(h)
			if err != nil {
				return nil, err
			}
			if groupKey != nil {
				p.GroupKey = string(groupKey)
			}
			if groupName != nil {
				p.GroupName = string(groupName)
			}
			payments = append(payments, p)
		}
	}

	return payments, nil
}

func (a *Service) getInflightPaymentsMap() (map[string]*lnrpc.Payment, error) {
	lnclient := a.daemonAPI.APIClient()
	lightningPayments, err := lnclient.ListPayments(context.Background(),
		&lnrpc.ListPaymentsRequest{IncludeIncomplete: true})
	if err != nil {
		return nil, err
	}

	payments := make(map[string]*lnrpc.Payment)
	for _, pending := range lightningPayments.Payments {
		if pending.Status == lnrpc.Payment_IN_FLIGHT {
			a.log.Infof("found inflight payment %v", pending.PaymentHash)
			//mar, _ := json.Marshal(pending)
			//a.log.Infof(string(mar))
			payments[pending.PaymentHash] = pending
		}
	}
	return payments, nil
}

func (a *Service) createPendingPayment(htlc *lnrpc.HTLC, currentBlockHeight uint32,
	inflightPayments map[string]*lnrpc.Payment) (*db.PaymentInfo, error) {

	paymentType := db.SentPayment
	if htlc.Incoming {
		paymentType = db.ReceivedPayment
	}

	var paymentRequest string
	var pendingPayment *lnrpc.Payment
	lnclient := a.daemonAPI.APIClient()

	a.log.Infof("createPendingPayment")
	amount := htlc.Amount
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
		if err != nil {
			return nil, err
		}
		hashStr := hex.EncodeToString(htlc.HashLock)
		pendingPayment, _ = inflightPayments[hashStr]
	}

	minutesToExpire := time.Duration((htlc.ExpirationHeight - currentBlockHeight) * 10)
	paymentData := &db.PaymentInfo{
		Type:                       paymentType,
		Amount:                     amount,
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
		paymentData.IsKeySend = len(pendingPayment.PaymentRequest) == 0
		paymentData.PaymentHash = pendingPayment.PaymentHash
		paymentData.CreationTimestamp = pendingPayment.CreationTimeNs / int64(time.Second)
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

		/* If invoiceMemo.Description is empty check if
		   there's an LNUrlPayInfo with this paymentHash and
		   store that description. Why? Because in LNUrl-Pay,
		   an invoice will only have a descriptionHash in the `h` tag.
		   The client receives the invoice description in a separate request.
		   We save the LNUrlPayInfo as soon as we receive it so it is ok to check the db for it here.
		*/
		if info, err := a.breezDB.FetchLNUrlPayInfo(paymentItem.PaymentHash); err == nil && info != nil {
			if paymentData.Description == "" {
				paymentData.Description = info.InvoiceDescription
				a.log.Infof("onNewSentPayment: No description found in this invoice. Using :%q", paymentData.Description)
			}

			if info.SuccessAction != nil && info.SuccessAction.Tag == "aes" {
				if info.SuccessAction.Message, err = a.DecryptLNUrlPayMessage(paymentItem.PaymentHash, invoiceMemo.Preimage); err != nil {
					a.log.Errorf("onNewSentPayment: Could not decrypt 'aes' lnurl-pay message: %s", err)
				}

			}
		}

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
		groupKey, groupName, err := a.breezDB.FetchPaymentGroup(paymentItem.PaymentHash)
		if err != nil {
			return err
		}

		numHtlcs := len(paymentItem.Htlcs)
		if numHtlcs > 0 && paymentItem.Htlcs[0].Route != nil {
			hops := paymentItem.Htlcs[0].Route.Hops
			if len(hops) > 0 {
				lastHop := hops[len(hops)-1]
				paymentData.Destination = lastHop.PubKey
			}
		}
		if groupKey != nil {
			paymentData.GroupKey = string(groupKey)
		}
		if groupName != nil {
			paymentData.GroupName = string(groupName)
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
		paymentData.Type = db.WithdrawalPayment
		paymentData.RedeemTxID = swap.ClaimTxid
		paymentData.Amount = swap.OnchainAmount - swap.ClaimFee
		paymentData.Fee += paymentItem.Value - swap.OnchainAmount + swap.ClaimFee
	}

	skipped, err := a.breezDB.AddAccountPayment(paymentData, 0, uint64(paymentItem.CreationDate))
	if !skipped {
		a.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_PAYMENT_SENT})
		a.onAccountChanged()
	}
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
	a.log.Infof("got payment zero-conf payreq = %v", string(zeroConfPayreq))
	var zeroConfMemo *data.InvoiceMemo
	if len(zeroConfPayreq) > 0 {
		if zeroConfMemo, err = a.DecodePaymentRequest(string(zeroConfPayreq)); err != nil {
			return err
		}
		a.log.Infof("got payment decoded zero-conf memo amount: %v", zeroConfMemo.Amount)
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
		a.log.Infof("got payment calculated fee: %v", paymentData.Fee)
		if err = a.breezDB.RemoveZeroConfHash(invoice.RHash); err != nil {
			return err
		}
	}

	_, err = a.breezDB.AddAccountPayment(paymentData, invoice.SettleIndex, 0)
	if err != nil {
		a.log.Errorf("Unable to add reveived payment : %v", err)
		return err
	}
	a.onServiceEvent(data.NotificationEvent{
		Type: data.NotificationEvent_INVOICE_PAID,
		Data: []string{invoice.PaymentRequest}})
	a.onAccountChanged()
	return nil
}

func (a *Service) registerPayment(paymentHash, paymentSecret []byte, incomingAmountMsat,
	outgoingAmountMsat int64, lspPubkey []byte, lspID string, params *data.OpeningFeeParams) error {

	destination, err := hex.DecodeString(a.daemonAPI.NodePubkey())
	if err != nil {
		a.log.Infof("hex.DecodeString(%v) error: %v", a.daemonAPI.NodePubkey(), err)
		return fmt.Errorf("hex.DecodeString(%v) error: %w", a.daemonAPI.NodePubkey(), err)
	}
	tag, err := a.generateTag()
	if err != nil {
		a.log.Infof("generateTag() error: %v", err)
		return fmt.Errorf("generateTag() error: %w", err)
	}

	var p *lspd.OpeningFeeParams

	// TODO: This nil check can be removed once all branches use the
	// opening_fee_params flow. This only exists because some swaps may use the
	// old fee structure.
	if params != nil {
		p = &lspd.OpeningFeeParams{
			MinMsat:              params.MinMsat,
			Proportional:         params.Proportional,
			ValidUntil:           params.ValidUntil,
			MaxIdleTime:          params.MaxIdleTime,
			MaxClientToSelfDelay: params.MaxClientToSelfDelay,
			Promise:              params.Promise,
		}
	}
	pi := &lspd.PaymentInformation{
		PaymentHash:        paymentHash,
		PaymentSecret:      paymentSecret,
		Destination:        destination,
		IncomingAmountMsat: incomingAmountMsat,
		OutgoingAmountMsat: outgoingAmountMsat,
		Tag:                tag,
		OpeningFeeParams:   p,
	}
	data, err := proto.Marshal(pi)

	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()

	a.log.Infof("Register Payment pubkey = %v", lspPubkey)
	pubkey, err := btcec.ParsePubKey(lspPubkey)
	if err != nil {
		a.log.Infof("btcec.ParsePubKey(%x) error: %v", lspPubkey, err)
		return fmt.Errorf("btcec.ParsePubKey(%x) error: %w", lspPubkey, err)
	}
	blob, err := btceclegacy.Encrypt(pubkey, data)
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

func (a *Service) generateTag() (string, error) {
	h := sha256.Sum256([]byte(a.cfg.LspToken))
	k := hex.EncodeToString(h[:])
	obj := map[string]interface{}{
		"apiKeyHash": k,
	}

	tag, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}

	return string(tag), nil
}
