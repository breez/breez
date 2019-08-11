package bindings

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/breez/breez"
	"github.com/breez/breez/bootstrap"
	"github.com/breez/breez/closedchannels"
	"github.com/breez/breez/data"
	"github.com/breez/breez/doubleratchet"
	breezlog "github.com/breez/breez/log"
	breezSync "github.com/breez/breez/sync"
	"github.com/btcsuite/btclog"
	"github.com/golang/protobuf/proto"
)

var (
	appServices AppServices
	breezApp    *breez.App
	appLogger   Logger
	mu          sync.Mutex
)

// AppServices defined the interface needed in Breez library in order to functional
// right.
type AppServices interface {
	Notify(notificationEvent []byte)
	BackupProviderName() string
	BackupProviderSignIn() (string, error)
}

// Logger is an interface that is used to log to the central log file.
type Logger interface {
	Log(msg string, lvl string)
}

// BreezLogger is the implementation of Logger
type BreezLogger struct {
	log btclog.Logger
}

// BackupService is an service to this libary for backup execution.
type BackupService interface {
	Backup(files string, nodeID, backupID string) error
}

// Log writs to the centeral log file
func (l *BreezLogger) Log(msg string, lvl string) {
	switch lvl {
	case "FINEST":
	case "FINER":
	case "FINE":
		l.log.Tracef(msg)
	case "CONFIG":
		l.log.Debugf(msg)
	case "INFO":
		l.log.Infof(msg)
	case "WARNING":
		l.log.Warnf(msg)
	case "SEVERE":
		l.log.Errorf(msg)
	case "SHOUT":
		l.log.Criticalf(msg)
	default:
		l.log.Infof(msg)
	}
}

/*
JobController is the interface to return when scheuling a job to allow the caller to cancel at
any time
*/
type JobController interface {
	Run() error
	Stop()
}

/*
ChannelsWatcherJobController is the interface to return when scheuling the channels watcher job to
allow the caller to cancel at any time
*/
type ChannelsWatcherJobController interface {
	Run() (bool, error)
	Stop()
}

func getBreezApp() *breez.App {
	mu.Lock()
	defer mu.Unlock()
	return breezApp
}

/*
Init initialize lightning client
*/
func Init(tempDir string, workingDir string, services AppServices) (err error) {
	os.Setenv("TMPDIR", tempDir)
	appServices = services
	appLogger, err = GetLogger(workingDir)
	if err != nil {
		fmt.Println("Error in init ", err)
		return err
	}
	mu.Lock()
	breezApp, err = breez.NewApp(workingDir, services)
	mu.Unlock()
	return err
}

// NeedsBootstrap checks if bootstrap header is needed.
func NeedsBootstrap() bool {
	need, err := getBreezApp().NeedsBootstrap()
	if err != nil {
		fmt.Println("Error in NeedsBootstrap ", err)
		return false
	}
	fmt.Println("Needs Boottrap = ", need)
	return need
}

// BootstrapHeaders bootstrap the chain with existing header files.
func BootstrapHeaders(bootstrapDir string) error {
	return getBreezApp().BootstrapHeaders(bootstrapDir)
}

/*
Start the lightning client
*/
func Start() error {
	err := getBreezApp().Start()
	if err != nil {
		return err
	}
	go deliverNotifications(getBreezApp().NotificationChan(), appServices)
	return nil
}

/*
LastSyncedHeaderTimestamp returns the last header the node is synced to.
*/
func LastSyncedHeaderTimestamp() int64 {
	last, _ := getBreezApp().LastSyncedHeaderTimestamp()
	return last
}

/*
RestartDaemon attempts to restart the daemon service.
*/
func RestartDaemon() error {
	return getBreezApp().RestartDaemon()
}

/*
NewSyncJob starts breez only to reach synchronized state.
The daemon closes itself automatically when reaching this state.
*/
func NewSyncJob(workingDir string) (ChannelsWatcherJobController, error) {
	job, err := breezSync.NewJob(workingDir)
	if err != nil {
		return nil, err
	}
	return job, nil
}

/*
NewClosedChannelsJob starts a job to download the list of closed channels.
The daemon closes itself automatically when reaching this state.
*/
func NewClosedChannelsJob(workingDir string) (JobController, error) {
	job, err := closedchannels.NewJob(workingDir)
	if err != nil {
		return nil, err
	}
	return job, nil
}

/*
GetLogger creates a logger that logs to the same breez central log file
*/
func GetLogger(appDir string) (Logger, error) {
	backend, err := breezlog.GetLogBackend(appDir)
	if err != nil {
		return nil, err
	}
	logger := backend.Logger("BIND")
	return &BreezLogger{logger}, nil
}

/*
Stop the lightning client
*/
func Stop() {
	getBreezApp().Stop()
}

/*
RequestBackup triggers breez RequestBackup
*/
func RequestBackup() {
	getBreezApp().BackupManager.RequestBackup()
}

/*
RestoreBackup is part of the binding inteface which is delegated to breez.RestoreBackup
*/
func RestoreBackup(nodeID string) (err error) {
	if err = getBreezApp().Stop(); err != nil {
		return err
	}
	if _, err = getBreezApp().BackupManager.Restore(nodeID, ""); err != nil {
		return err
	}
	breezApp, err = breez.NewApp(getBreezApp().GetWorkingDir(), appServices)
	return err
}

/*
AvailableSnapshots is part of the binding inteface which is delegated to breez.AvailableSnapshots
*/
func AvailableSnapshots() (string, error) {
	snapshots, err := getBreezApp().BackupManager.AvailableSnapshots()
	if err != nil {
		Log("error in calling AvailableSnapshots: "+err.Error(), "INFO")
		return "", err
	}
	bytes, err := json.Marshal(snapshots)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

/*
DaemonReady returns the status of the daemon
*/
func DaemonReady() bool {
	return getBreezApp().DaemonReady()
}

/*
OnResume just calls the breez.OnResume
*/
func OnResume() {
	getBreezApp().OnResume()
}

/*
Log is a function that uses the breez logger
*/
func Log(msg string, lvl string) {
	appLogger.Log(msg, lvl)
}

/*
GetAccountInfo is part of the binding inteface which is delegated to breez.GetAccountInfo
*/
func GetAccountInfo() ([]byte, error) {
	return marshalResponse(getBreezApp().AccountService.GetAccountInfo())
}

/*
ConnectAccount is part of the binding inteface which is delegated to breez.ConnectAccount
*/
func ConnectAccount() error {
	return getBreezApp().AccountService.ConnectChannelsPeers()
}

/*
AddFundsInit is part of the binding inteface which is delegated to breez.AddFundsInit
*/
func AddFundsInit(breezID string) ([]byte, error) {
	return marshalResponse(getBreezApp().SwapService.AddFundsInit(breezID))
}

/*
GetRefundableSwapAddresses returns all addresses that are refundable, e.g expired and not paid
*/
func GetRefundableSwapAddresses() ([]byte, error) {
	refundableAddresses, err := getBreezApp().SwapService.GetRefundableAddresses()
	if err != nil {
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

	addressList := &data.SwapAddressList{
		Addresses: rpcAddresses,
	}
	fmt.Printf("GetRefundableSwapAddresses returned %v addresses", len(rpcAddresses))
	return marshalResponse(addressList, nil)
}

//Refund transfers the funds in address to the user destination address
func Refund(refundRequest []byte) (string, error) {
	Log("binding: starting refund flow...", "INFO")
	request := &data.RefundRequest{}
	if err := proto.Unmarshal(refundRequest, request); err != nil {
		return "", err
	}
	return getBreezApp().SwapService.Refund(request.Address, request.RefundAddress)
}

/*
GetFundStatus is part of the binding inteface which is delegated to breez.GetFundStatus
*/
func GetFundStatus(notificationToken string) ([]byte, error) {
	return marshalResponse(getBreezApp().SwapService.GetFundStatus(notificationToken))
}

/*
RemoveFund is part of the binding inteface which is delegated to breez.RemoveFund
*/
func RemoveFund(removeFundRequest []byte) ([]byte, error) {
	request := &data.RemoveFundRequest{}
	proto.Unmarshal(removeFundRequest, request)
	return marshalResponse(getBreezApp().SwapService.RemoveFund(request.Amount, request.Address))
}

/*
GetLogPath is part of the binding inteface which is delegated to breez.GetLogPath
*/
func GetLogPath() string {
	return getBreezApp().GetLogPath()
}

/*
GetPayments is part of the binding inteface which is delegated to breez.GetPayments
*/
func GetPayments() ([]byte, error) {
	return marshalResponse(getBreezApp().AccountService.GetPayments())
}

/*
PayBlankInvoice is part of the binding inteface which is delegated to breez.PayBlankInvoice
*/
func SendPaymentForRequest(payInvoiceRequest []byte) ([]byte, error) {
	decodedRequest := &data.PayInvoiceRequest{}
	proto.Unmarshal(payInvoiceRequest, decodedRequest)
	resp, err := getBreezApp().AccountService.SendPaymentForRequest(decodedRequest.PaymentRequest, decodedRequest.Amount)
	if err != nil {
		return nil, err
	}
	return marshalResponse(&data.PaymentResponse{PaymentError: resp.PaymentError, TraceReport: resp.TraceReport}, err)
}

/*
SendPaymentFailureBugReport is part of the binding inteface which is delegated to breez.SendPaymentFailureBugReport
*/
func SendPaymentFailureBugReport(report string) error {
	return getBreezApp().AccountService.SendPaymentFailureBugReport(report)
}

/*
AddInvoice is part of the binding inteface which is delegated to breez.AddInvoice
*/
func AddInvoice(invoice []byte) (paymentRequest string, err error) {
	decodedInvoiceMemo := &data.InvoiceMemo{}
	proto.Unmarshal(invoice, decodedInvoiceMemo)
	return getBreezApp().AccountService.AddInvoice(decodedInvoiceMemo)
}

/*
DecodePaymentRequest is part of the binding inteface which is delegated to breez.DecodePaymentRequest
*/
func DecodePaymentRequest(paymentRequest string) ([]byte, error) {
	return marshalResponse(getBreezApp().AccountService.DecodePaymentRequest(paymentRequest))
}

/*
GetRelatedInvoice is part of the binding inteface which is delegated to breez.GetRelatedInvoice
*/
func GetRelatedInvoice(paymentRequest string) ([]byte, error) {
	return marshalResponse(getBreezApp().AccountService.GetRelatedInvoice(paymentRequest))
}

/*
SendWalletCoins is part of the binding inteface which is delegated to breez.SendWalletCoins
*/
func SendWalletCoins(sendCoinsRequest []byte) (string, error) {
	unmarshaledRequest := data.SendWalletCoinsRequest{}
	proto.Unmarshal(sendCoinsRequest, &unmarshaledRequest)
	return getBreezApp().AccountService.SendWalletCoins(unmarshaledRequest.Address, unmarshaledRequest.Amount, unmarshaledRequest.SatPerByteFee)
}

/*
GetDefaultOnChainFeeRate is part of the binding inteface which is delegated to breez.GetDefaultOnChainFeeRate
*/
func GetDefaultOnChainFeeRate() int64 {
	return getBreezApp().AccountService.GetDefaultSatPerByteFee()
}

/*
ValidateAddress is part of the binding inteface which is delegated to breez.ValidateAddress
*/
func ValidateAddress(address string) error {
	return getBreezApp().AccountService.ValidateAddress(address)
}

/*
SendCommand is part of the binding inteface which is delegated to breez.SendPaymentForRequest
*/
func SendCommand(command string) (string, error) {
	return getBreezApp().SendCommand(command)
}

/*
RegisterReceivePaymentReadyNotification is part of the binding inteface which is delegated to breez.RegisterReceivePaymentReadyNotification
*/
func RegisterReceivePaymentReadyNotification(token string) error {
	return getBreezApp().AccountService.RegisterReceivePaymentReadyNotification(token)
}

/*
RegisterChannelOpenedNotification is part of the binding inteface which is delegated to breez.RegisterChannelOpenedNotification
*/
func RegisterChannelOpenedNotification(token string) error {
	return getBreezApp().AccountService.RegisterChannelOpenedNotification(token)
}

/*
RegisterPeriodicSync is part of the binding inteface which is delegated to breez.RegisterPeriodicSync
*/
func RegisterPeriodicSync(token string) error {
	return getBreezApp().AccountService.RegisterPeriodicSync(token)
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
		secret, pubKey, err = doubleratchet.NewSession(unmarshaledRequest.SessionID, unmarshaledRequest.Expiry)
	} else {
		err = doubleratchet.NewSessionWithRemoteKey(unmarshaledRequest.SessionID, unmarshaledRequest.Secret, unmarshaledRequest.RemotePubKey, unmarshaledRequest.Expiry)
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

func GetPeers() ([]byte, error) {
	var p data.Peers
	peers, isDefault, err := getBreezApp().GetPeers()
	if err != nil {
		return nil, err
	}
	p.Peer = peers
	p.IsDefault = isDefault
	return marshalResponse(&p, nil)
}

func SetPeers(request []byte) error {
	var p data.Peers
	if err := proto.Unmarshal(request, &p); err != nil {
		return err
	}
	err := getBreezApp().SetPeers(p.Peer)
	return err
}

func Rate() ([]byte, error) {
	return marshalResponse(getBreezApp().ServicesClient.Rates())
}

func LSPList() ([]byte, error) {
	return marshalResponse(getBreezApp().ServicesClient.LSPList())
}

func SetLSP(id string) error {
	getBreezApp().AccountService.SetLSP(id)
	return nil
}

func GetLSP() string {
	return getBreezApp().AccountService.GetLSP()
}

func deliverNotifications(notificationsChan chan data.NotificationEvent, appServices AppServices) {
	for {
		notification := <-notificationsChan
		res, err := proto.Marshal(&notification)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in marshaing notification", err)
		}
		appServices.Notify(res)
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
