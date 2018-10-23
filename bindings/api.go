package bindings

import (
	"fmt"
	"os"

	"github.com/breez/breez"
	"github.com/breez/breez/data"
	"github.com/golang/protobuf/proto"
)

/*
BreezNotifier is the interface that is used to send notifications to the user of this library
*/
type BreezNotifier interface {
	Notify(notificationEvent []byte)
}

/*
Start the lightning client
*/
func Start(workingDir string, syncJobMode bool, notifier BreezNotifier) (err error) {
	notificationsChan, err := breez.Start(workingDir, syncJobMode)
	if err != nil {
		return err
	}
	go deliverNotifications(notificationsChan, notifier)
	return nil
}

/*
Stop the lightning client
*/
func Stop() error {
	return breez.Stop()
}

/*
Log is a function that uses the breez logger
*/
func Log(msg string, lvl string) {
	breez.Log(msg, lvl)
}

/*
GetAccountInfo is part of the binding inteface which is delegated to breez.GetAccountInfo
*/
func GetAccountInfo() ([]byte, error) {
	return marshalResponse(breez.GetAccountInfo())
}

/*
ConnectAccount is part of the binding inteface which is delegated to breez.ConnectAccount
*/
func ConnectAccount() error {
	return breez.ConnectAccount()
}

/*
IsConnectedToRoutingNode is part of the binding inteface which is delegated to breez.IsConnectedToRoutingNode
*/
func IsConnectedToRoutingNode() bool {
	return breez.IsConnectedToRoutingNode()
}

/*
AddFunds is part of the binding inteface which is delegated to breez.AddFunds
*/
func AddFunds(breezID string) ([]byte, error) {
	return marshalResponse(breez.AddFunds(breezID))
}

/*
GetFundStatus is part of the binding inteface which is delegated to breez.GetFundStatus
*/
func GetFundStatus(notificationToken string) ([]byte, error) {
	return marshalResponse(breez.GetFundStatus(notificationToken))
}

/*
GetLogPath is part of the binding inteface which is delegated to breez.GetLogPath
*/
func GetLogPath() string {
	return breez.GetLogPath()
}

/*
GetPayments is part of the binding inteface which is delegated to breez.GetPayments
*/
func GetPayments() ([]byte, error) {
	return marshalResponse(breez.GetPayments())
}

/*
SendPaymentForRequest is part of the binding inteface which is delegated to breez.SendPaymentForRequest
*/
func SendPaymentForRequest(paymentRequest string) error {
	//return marshalResponse(account.SendPaymentForRequest())
	return breez.SendPaymentForRequest(paymentRequest)
}

/*
PayBlankInvoice is part of the binding inteface which is delegated to breez.PayBlankInvoice
*/
func PayBlankInvoice(payInvoiceRequest []byte) error {
	decodedRequest := &data.PayInvoiceRequest{}
	proto.Unmarshal(payInvoiceRequest, decodedRequest)
	return breez.PayBlankInvoice(decodedRequest.PaymentRequest, decodedRequest.Amount)
}

/*
AddInvoice is part of the binding inteface which is delegated to breez.AddInvoice
*/
func AddInvoice(invoice []byte) (paymentRequest string, err error) {
	decodedInvoiceMemo := &data.InvoiceMemo{}
	proto.Unmarshal(invoice, decodedInvoiceMemo)
	return breez.AddInvoice(decodedInvoiceMemo)
}

/*
DecodePaymentRequest is part of the binding inteface which is delegated to breez.DecodePaymentRequest
*/
func DecodePaymentRequest(paymentRequest string) ([]byte, error) {
	return marshalResponse(breez.DecodePaymentRequest(paymentRequest))
}

/*
GetRelatedInvoice is part of the binding inteface which is delegated to breez.GetRelatedInvoice
*/
func GetRelatedInvoice(paymentRequest string) ([]byte, error) {
	return marshalResponse(breez.GetRelatedInvoice(paymentRequest))
}

/*
SendNonDepositedCoins is part of the binding inteface which is delegated to breez.SendNonDepositedCoins
*/
func SendNonDepositedCoins(sendCoinsRequest []byte) error {
	unmarshaledRequest := data.SendNonDepositedCoinsRequest{}
	proto.Unmarshal(sendCoinsRequest, &unmarshaledRequest)
	return breez.SendNonDepositedCoins(unmarshaledRequest.Address)
}

/*
ValidateAddress is part of the binding inteface which is delegated to breez.ValidateAddress
*/
func ValidateAddress(address string) error {
	return breez.ValidateAddress(address)
}

/*
SendCommand is part of the binding inteface which is delegated to breez.SendPaymentForRequest
*/
func SendCommand(command string) (string, error) {
	return breez.SendCommand(command)
}

func deliverNotifications(notificationsChan chan data.NotificationEvent, notifier BreezNotifier) {
	for {
		notification := <-notificationsChan
		res, err := proto.Marshal(&notification)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in marshaing notification", err)
		}
		notifier.Notify(res)
	}
}

func marshalResponse(message proto.Message, responseError error) (buffer []byte, err error) {
	if responseError != nil {
		return nil, responseError
	}
	res, err := proto.Marshal(message)
	if err != nil {
		return nil, err
	}
	return res, nil
}
