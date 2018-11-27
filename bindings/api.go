package bindings

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/breez/breez"
	"github.com/breez/breez/bootstrap"
	"github.com/breez/breez/data"
	"github.com/breez/breez/doubleratchet"
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
func Start(workingDir string, tempDir string, notifier BreezNotifier) (err error) {
	os.Setenv("TMPDIR", tempDir)
	notificationsChan, err := breez.Start(workingDir, false)
	if err != nil {
		return err
	}
	go deliverNotifications(notificationsChan, notifier)
	return nil
}

/*
StartSyncJob starts breez only to reach synchronized state.
The daemon closes itself automatically when reaching this state.
*/
func StartSyncJob(workingDir string) error {
	_, err := breez.Start(workingDir, true)
	return err
}

/*
Stop the lightning client
*/
func Stop() {
	breez.Stop()
}

/*
WaitDaemonShutdown blocks untill the daemon shutdown
*/
func WaitDaemonShutdown() {
	breez.WaitDaemonShutdown()
}

/*
DaemonReady returns the status of the daemon
*/
func DaemonReady() bool {
	return breez.DaemonReady()
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
AddFundsInit is part of the binding inteface which is delegated to breez.AddFundsInit
*/
func AddFundsInit(breezID string) ([]byte, error) {
	return marshalResponse(breez.AddFundsInit(breezID))
}

/*
GetRefundableSwapAddresses returns all addresses that are refundable, e.g expired and not paid
*/
func GetRefundableSwapAddresses() ([]byte, error) {
	fmt.Println("GetRefundableSwapAddresses in api")
	refundableAddresses, err := breez.GetRefundableAddresses()
	if err != nil {
		fmt.Println("GetRefundableSwapAddresses in api returned error from breez")
		return nil, err
	}
	var rpcAddresses []*data.SwapAddressInfo
	for _, a := range refundableAddresses {
		rpcAddresses = append(rpcAddresses, &data.SwapAddressInfo{
			Address:                 a.Address,
			PaymentHash:             hex.EncodeToString(a.PaymentHash),
			ConfirmedAmount:         a.ConfirmedAmount,
			ConfirmedTransactionIds: a.ConfirmedTransactionIds,
			PaidAmount:              a.PaidAmount,
			LockHeight:              a.LockHeight,
			ErrorMessage:            a.ErrorMessage,
			LastRefundTxID:          a.LastRefundTxID,
		})
	}

	fmt.Println("GetRefundableSwapAddresses creating address list")
	addressList := &data.SwapAddressList{
		Addresses: rpcAddresses,
	}
	fmt.Printf("GetRefundableSwapAddresses return result %v", addressList)
	return marshalResponse(addressList, nil)
}

//Refund transfers the funds in address to the user destination address
func Refund(refundRequest []byte) (string, error) {
	request := &data.RefundRequest{}
	if err := proto.Unmarshal(refundRequest, request); err != nil {
		return "", err
	}
	return breez.Refund(request.Address, request.RefundAddress)
}

/*
GetFundStatus is part of the binding inteface which is delegated to breez.GetFundStatus
*/
func GetFundStatus(notificationToken string) ([]byte, error) {
	return marshalResponse(breez.GetFundStatus(notificationToken))
}

/*
RemoveFund is part of the binding inteface which is delegated to breez.RemoveFund
*/
func RemoveFund(removeFundRequest []byte) ([]byte, error) {
	request := &data.RemoveFundRequest{}
	proto.Unmarshal(removeFundRequest, request)
	return marshalResponse(breez.RemoveFund(request.Amount, request.Address))
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
AddStandardInvoice is part of the binding inteface which is delegated to breez.AddStandardInvoice
*/
func AddStandardInvoice(invoice []byte) (paymentRequest string, err error) {
	decodedStandardInvoiceMemo := &data.InvoiceMemo{}
	proto.Unmarshal(invoice, decodedStandardInvoiceMemo)
	return breez.AddStandardInvoice(decodedStandardInvoiceMemo)
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

/*
RegisterReceivePaymentReadyNotification is part of the binding inteface which is delegated to breez.RegisterReceivePaymentReadyNotification
*/
func RegisterReceivePaymentReadyNotification(token string) error {
	return breez.RegisterReceivePaymentReadyNotification(token)
}

/*
CreateRatchetSession is part of the binding inteface which is delegated to breez.CreateRatchetSession
*/
func CreateRatchetSession(request []byte) ([]byte, error) {
	var err error
	var secret, pubKey string

	unmarshaledRequest := &data.CreateRatchetSessionRequest{}
	if err := proto.Unmarshal(request, unmarshaledRequest); err != nil {
		return nil, err
	}

	//if has secret then we are initiators
	if unmarshaledRequest.Secret == "" {
		secret, pubKey, err = doubleratchet.NewSession(unmarshaledRequest.SessionID)
	} else {
		err = doubleratchet.NewSessionWithRemoteKey(unmarshaledRequest.SessionID, unmarshaledRequest.Secret, unmarshaledRequest.RemotePubKey)
	}

	if err != nil {
		return nil, err
	}
	return marshalResponse(&data.CreateRatchetSessionReply{SessionID: unmarshaledRequest.SessionID, Secret: secret, PubKey: pubKey}, nil)
}

/*
RatchetSessionInfo is part of the binding inteface which is delegated to breez.RatchetSessionInfo
*/
func RatchetSessionInfo(sessionID string) ([]byte, error) {
	var reply *data.RatchetSessionInfoReply
	sessionDetails := doubleratchet.RatchetSessionInfo(sessionID)
	if sessionDetails == nil {
		reply = &data.RatchetSessionInfoReply{
			SessionID: "",
			Initiated: false,
		}
	} else {
		reply = &data.RatchetSessionInfoReply{
			SessionID: sessionDetails.SessionID,
			Initiated: sessionDetails.Initiated,
			UserInfo:  sessionDetails.UserInfo,
		}
	}
	return marshalResponse(reply, nil)
}

/*
RatchetSessionSetInfo is part of the binding inteface which is delegated to breez.RatchetSessionSetInfo
*/
func RatchetSessionSetInfo(request []byte) error {
	unmarshaledRequest := &data.RatchetSessionSetInfoRequest{}
	if err := proto.Unmarshal(request, unmarshaledRequest); err != nil {
		return err
	}
	return doubleratchet.RatchetSessionSetInfo(unmarshaledRequest.SessionID, unmarshaledRequest.UserInfo)
}

/*
RatchetEncrypt is part of the binding inteface which is delegated to breez.RatchetEncrypt
*/
func RatchetEncrypt(request []byte) (string, error) {
	unmarshaledRequest := &data.RatchetEncryptRequest{}
	if err := proto.Unmarshal(request, unmarshaledRequest); err != nil {
		return "", err
	}

	return doubleratchet.RatchetEncrypt(unmarshaledRequest.SessionID, unmarshaledRequest.Message)
}

/*
RatchetDecrypt is part of the binding inteface which is delegated to breez.RatchetDecrypt
*/
func RatchetDecrypt(request []byte) (string, error) {
	unmarshaledRequest := &data.RatchetDecryptRequest{}
	if err := proto.Unmarshal(request, unmarshaledRequest); err != nil {
		return "", err
	}

	return doubleratchet.RatchetDecrypt(unmarshaledRequest.SessionID, unmarshaledRequest.EncryptedMessage)
}

// BootstrapFiles is part of the binding inteface which is delegated to bootstrap.PutFiles
func BootstrapFiles(request []byte) error {
	req := &data.BootstrapFilesRequest{}
	if err := proto.Unmarshal(request, req); err != nil {
		return err
	}

	return bootstrap.PutFiles(req.GetWorkingDir(), req.GetFullPaths())
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
