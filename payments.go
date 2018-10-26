package breez

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"

	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/golang/protobuf/proto"
	"golang.org/x/sync/singleflight"
)

var blankInvoiceGroup singleflight.Group

/*
GetPayments is responsible for retrieving the payment were made in this account
*/
func GetPayments() (*data.PaymentsList, error) {
	rawPayments, err := fetchAllAccountPayments()
	if err != nil {
		return nil, err
	}
	var paymentsList []*data.Payment
	for _, pBuf := range rawPayments {
		paymentItem := &data.Payment{}
		proto.Unmarshal(pBuf, paymentItem)
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
func SendPaymentForRequest(paymentRequest string) error {
	decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return err
	}
	if err := savePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return err
	}
	response, err := lightningClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest})
	if err != nil {
		return err
	}
	if len(response.PaymentError) > 0 {
		return errors.New(response.PaymentError)
	}

	syncSentPayments()
	return nil
}

/*
PayBlankInvoice send a P2P payment to the amount and the details specified in the bolt 11 payment request
*/
func PayBlankInvoice(paymentRequest string, amountSatoshi int64) error {
	decodedReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return err
	}
	if err := savePaymentRequest(decodedReq.PaymentHash, []byte(paymentRequest)); err != nil {
		return err
	}
	response, err := lightningClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{PaymentRequest: paymentRequest, Amt: amountSatoshi})
	if err != nil {
		return err
	}
	if len(response.PaymentError) > 0 {
		return errors.New(response.PaymentError)
	}
	onAccountChanged()
	return nil
}

/*
AddInvoice encapsulate a given invoice information in a payment request
*/
func AddInvoice(invoice *data.InvoiceMemo) (paymentRequest string, err error) {
	memo, err := proto.Marshal(invoice)
	if err != nil {
		return "", err
	}
	response, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: string(memo), Private: true, Expiry: 180, Value: invoice.Amount})
	if err != nil {
		return "", err
	}
	log.Infof("Generated Invoice: %v", response.PaymentRequest)
	return response.PaymentRequest, nil
}

/*
AddStandardInvoice encapsulate a given amount and description in a payment request
*/
func AddStandardInvoice(amount int64, description string) (paymentRequest string, err error) {
	response, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: description, Private: true, Value: amount})
	if err != nil {
		return "", err
	}
	log.Infof("Generated Invoice: %v", response.PaymentRequest)
	return response.PaymentRequest, nil
}

/*
generateBlankInvoiceWithRetry calls generateBlankInvoice in 10 second intervals until it succeeds
*/
func generateBlankInvoiceWithRetry() {
	blankInvoiceGroup.Do("blankinvoice", func() (interface{}, error) {
		for {
			res, err := generateBlankInvoice()
			if err == nil {
				return res, nil
			}
			log.Errorf("Error generating invoice, retrying...", err)
			time.Sleep(10 * time.Second)
		}
	})
}

/*
generateBlankInvoice issues a blank invoice with no amount and saves it to disk. This is later used by the NFC subsystem for P2P payments
*/
func generateBlankInvoice() (string, error) {
	f, err := os.Create(path.Join(appWorkingDir, "blank_invoice.txt"))
	if err != nil {
		return "", err
	}

	defer f.Close()

	channels, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		log.Errorf("generateBlankInvoice: failed to call ListChannels: %v", err)
		return "", err
	}

	if len(channels.Channels) < 1 {
		return "", errors.New("no channels to route through")
	}

	if !channels.Channels[0].Active {
		return "", errors.New("channel is not active")
	}

	response, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Private: true, Expiry: 60 * 60 * 24 * 30})
	if err != nil {
		log.Criticalf("Failed to call AddInvoice %v", err)
		return "", err
	}

	lookup, err := lightningClient.LookupInvoice(context.Background(), &lnrpc.PaymentHash{RHash: response.RHash})
	if err != nil {
		log.Criticalf("Failed to call LookupInvoice %v", err)
		return "", err
	}

	if len(lookup.RouteHints) < 1 {
		return "", errors.New("not enough route hints")
	}

	n, err := f.WriteString(response.PaymentRequest)
	if err != nil {
		return "", err
	}

	log.Infof("Wrote %d bytes of blank invoice!\n", n)
	log.Infof("generateBlankInvoice - paymentRequest = %v", response.PaymentRequest)

	if f.Sync() != nil {
		log.Criticalf("Couldn't flush blank invoice to disk!")
		return "", errors.New("couldn't flush to disk")
	}

	return response.PaymentRequest, nil

}

/*
DecodeInvoice is used by the payer to decode the payment request and read the invoice details.
*/
func DecodePaymentRequest(paymentRequest string) (*data.InvoiceMemo, error) {
	decodedPayReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return nil, err
	}
	invoiceMemo := &data.InvoiceMemo{}
	if err := proto.Unmarshal([]byte(decodedPayReq.Description), invoiceMemo); err != nil {
		// In case we cannot unmarshal the description we are probably dealing with a standard invoice
		invoiceMemo.Description = decodedPayReq.Description
		invoiceMemo.Amount = decodedPayReq.NumSatoshis
	}

	return invoiceMemo, nil
}

/*
GetRelatedInvoice is used by the payee to fetch the related invoice of its sent payment request so he can see if it is settled.
*/
func GetRelatedInvoice(paymentRequest string) (*data.Invoice, error) {
	decodedPayReq, err := lightningClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{PayReq: paymentRequest})
	if err != nil {
		return nil, err
	}

	invoiceMemo := &data.InvoiceMemo{}
	if err := proto.Unmarshal([]byte(decodedPayReq.Description), invoiceMemo); err != nil {
		return nil, err
	}

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

func watchPayments() {
	syncSentPayments()
	_, lastInvoiceSettledIndex := fetchPaymentsSyncInfo()
	log.Infof("last invoice settled index ", lastInvoiceSettledIndex)
	stream, err := lightningClient.SubscribeInvoices(context.Background(), &lnrpc.InvoiceSubscription{SettleIndex: lastInvoiceSettledIndex})
	if err != nil {
		log.Criticalf("Failed to call SubscribeInvoices %v, %v", stream, err)
	}

	go func() {
		for {
			invoice, err := stream.Recv()
			log.Infof("watchPayments - Invoice received by subscription")
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Criticalf("Failed to receive an invoice : %v", err)
			}
			if invoice.Settled {
				if invoice.Value == 0 {
					os.Remove(path.Join(appWorkingDir, "blank_invoice.txt"))
					go generateBlankInvoiceWithRetry()
				}
				log.Infof("watchPayments adding a received payment")
				onNewReceivedPayment(invoice)
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
	lastPaymentTime, _ := fetchPaymentsSyncInfo()
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

func onNewSentPayment(paymentItem *lnrpc.Payment) error {
	paymentRequest, err := fetchPaymentRequest(paymentItem.PaymentHash)
	if err != nil {
		return err
	}
	var invoiceMemo *data.InvoiceMemo
	if paymentRequest != nil && len(paymentRequest) > 0 {
		if invoiceMemo, err = DecodePaymentRequest(string(paymentRequest)); err != nil {
			return err
		}
	}

	paymentData := &data.Payment{
		Type:              data.Payment_SENT,
		Amount:            paymentItem.Value,
		InvoiceMemo:       invoiceMemo,
		CreationTimestamp: paymentItem.CreationDate,
	}
	buf, err := proto.Marshal(paymentData)
	if err != nil {
		return err
	}

	err = addAccountPayment(buf, 0, uint64(paymentItem.CreationDate))
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

	paymentType := data.Payment_RECEIVED
	if invoiceMemo.TransferRequest {
		paymentType = data.Payment_DEPOSIT
	}
	
	paymentData := &data.Payment{
		Type:              paymentType,
		Amount:            invoice.AmtPaidSat,
		InvoiceMemo:       invoiceMemo,
		CreationTimestamp: invoice.SettleDate,
	}
	buf, err := proto.Marshal(paymentData)
	if err != nil {
		return err
	}

	err = addAccountPayment(buf, invoice.SettleIndex, 0)
	if err != nil {
		log.Criticalf("Unable to add reveived payment : %v", err)
		return err
	}
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_INVOICE_PAID}
	onAccountChanged()
	return nil
}
