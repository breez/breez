package account

import (
	"context"
	"encoding/hex"
	"io"
	"time"

	"github.com/breez/breez/data"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

func (a *Service) watchCurrentInFlightPayments() error {
	paymentsResp, err := a.daemonAPI.APIClient().ListPayments(context.Background(), &lnrpc.ListPaymentsRequest{IncludeIncomplete: true})
	if err != nil {
		return err
	}
	for _, p := range paymentsResp.Payments {
		if p.Status == lnrpc.Payment_IN_FLIGHT {
			go func() {
				if err := a.trackInFlightPayment(p.PaymentHash); err != nil {
					a.log.Errorf("Failed to watch payment %v, error: %v", p.PaymentHash, err)
				}
			}()
		}
	}
	return nil
}

func (a *Service) trackInFlightPayment(paymentHash string) error {
	hashBytes, err := hex.DecodeString(paymentHash)
	if err != nil {
		return err
	}
	ctx := context.Background()
	trackStream, err := a.daemonAPI.RouterClient().TrackPayment(ctx, &routerrpc.TrackPaymentRequest{PaymentHash: hashBytes})
	if err != nil {
		return err
	}

	for {
		paymentStatus, err := trackStream.Recv()
		if err == io.EOF || ctx.Err() == context.Canceled {
			a.log.Errorf("watchInFlightPayment cancelled, shutting down")
			return err
		}

		a.log.Infof("In flight payment status = %v for hash %v", paymentStatus.State, paymentHash)
		if err != nil {
			a.log.Errorf("watchInFlightPayment Failed to get notification %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
			continue
		}

		if paymentStatus.State != routerrpc.PaymentState_IN_FLIGHT {
			a.notifyPaymentResult(paymentStatus.State == routerrpc.PaymentState_SUCCEEDED, paymentHash)
			a.syncSentPayments()
		}
	}
}

func (a *Service) notifyPaymentResult(succeeded bool, paymentHash string) {
	event := data.NotificationEvent_PAYMENT_FAILED
	if succeeded {
		event = data.NotificationEvent_PAYMENT_SUCCEEDED
	}
	a.onServiceEvent(data.NotificationEvent{
		Type: event,
		Data: []string{paymentHash}})
}
