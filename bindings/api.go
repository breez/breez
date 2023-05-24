package bindings

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"sync"

	"github.com/breez/boltz"
	"github.com/breez/breez"
	"github.com/breez/breez/bootstrap"
	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/closedchannels"
	"github.com/breez/breez/data"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/drophintcache"
	"github.com/breez/breez/dropwtx"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	breezSync "github.com/breez/breez/sync"
	"github.com/breez/breez/tor"
	"github.com/btcsuite/btclog"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lnrpc"
)

const (
	forceRescan        = "FORCE_RESCAN"
	forceBootstrap     = "FORCE_BOOTSTRAP"
	disabledTxSpentURL = "<DISABLED>"
)

var (
	appServices   AppServices
	breezApp      *breez.App
	appLogger     Logger
	cachedLSPList *data.LSPList
	mu            sync.Mutex

	ErrorForceRescan    = fmt.Errorf("Force rescan")
	ErrorForceBootstrap = fmt.Errorf("Force bootstrap")
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
	if err != nil || appLogger == nil {
		fmt.Println("Error in init ", err)
		return err
	}
	appLogger.Log("Breez initialization started", "INFO")
	startBeforeSync := true
	shouldForceRescan := false
	shouldForceBootstrap := false
	if _, err := os.Stat(path.Join(workingDir, forceRescan)); err == nil {
		appLogger.Log(fmt.Sprintf("%v present. Run Drop", forceRescan), "INFO")
		shouldForceRescan = true
	}
	if _, err := os.Stat(path.Join(workingDir, forceBootstrap)); err == nil {
		appLogger.Log(fmt.Sprintf("%v present. Deleting neutrino files", forceBootstrap), "INFO")
		shouldForceBootstrap = true
	}
	if shouldForceBootstrap || shouldForceRescan {
		err = dropwtx.Drop(workingDir)
		appLogger.Log(fmt.Sprintf("Drop result: %v", err), "INFO")
		err = drophintcache.Drop(workingDir)
		appLogger.Log(fmt.Sprintf("Drop hint cache result: %v", err), "INFO")
		if err == nil {
			err = os.Remove(path.Join(workingDir, forceRescan))
			appLogger.Log(fmt.Sprintf("Removed file: %v result: %v", forceRescan, err), "INFO")
		}
		if shouldForceBootstrap {
			err = chainservice.ResetChainService(workingDir)
			appLogger.Log(fmt.Sprintf("Delete result: %v", err), "INFO")
			if err == nil {
				err = os.Remove(path.Join(workingDir, forceBootstrap))
				startBeforeSync = false
				appLogger.Log(fmt.Sprintf("Removed file: %v result: %v", forceBootstrap, err), "INFO")
			}
		}

		// drop last sweeper tx
		db, close, _ := channeldbservice.Get(workingDir)
		defer close()
		err := kvdb.Update(db.Backend, func(tx kvdb.RwTx) error {
			b := tx.ReadWriteBucket([]byte("sweeper-last-tx"))
			return b.Delete([]byte("last-tx"))
		}, func() {})
		if err != nil {
			fmt.Printf("failed to drop last sweeper tx: %v", err)
			return err
		}
	}
	mu.Lock()
	breezApp, err = breez.NewApp(workingDir, services, startBeforeSync)
	mu.Unlock()
	if err != nil {
		appLogger.Log("Breez initialization failed: %v", "INFO")
	} else {
		appLogger.Log("Breez initialization finished", "INFO")
	}
	return err
}

// SetBackupProvider sets a new backup provider backend.
func SetBackupProvider(providerName, authData string) error {
	return getBreezApp().BackupManager.SetBackupProvider(providerName, authData)
}

// SetBackupEncryptionKey sets the security key to the backup manager so it
// can be used in encrypting backup files.
func SetBackupEncryptionKey(key []byte, encryptionType string) error {
	encKey := append([]byte(nil), key...)
	return getBreezApp().BackupManager.SetEncryptionKey(encKey, encryptionType)
}

/*
Start the lightning client
*/
func Start(torConfig []byte) error {
	_torConfig := &data.TorConfig{}
	if err := proto.Unmarshal(torConfig, _torConfig); err != nil {
		return err
	}

	Log(fmt.Sprintf("api.go: Start: _torConfig: %+v", *_torConfig), "INFO")
	err := getBreezApp().Start(_torConfig)
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
	if _, err := os.Stat(path.Join(workingDir, forceRescan)); err == nil {
		return nil, ErrorForceRescan
	}
	if _, err := os.Stat(path.Join(workingDir, forceBootstrap)); err == nil {
		return nil, ErrorForceBootstrap
	}
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
	if _, err := os.Stat(path.Join(workingDir, forceRescan)); err == nil {
		return nil, ErrorForceRescan
	}
	if _, err := os.Stat(path.Join(workingDir, forceBootstrap)); err == nil {
		return nil, ErrorForceBootstrap
	}
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
	logger, err := breezlog.GetLogger(appDir, "BIND")
	if err != nil {
		return nil, err
	}
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
	getBreezApp().BackupManager.RequestFullBackup()
}

func RequestAppDataBackup() {
	getBreezApp().BackupManager.RequestAppDataBackup()
}

func BackupFiles() (string, error) {
	return getBreezApp().BackupFiles()
}

/*
DownloadBackup is part of the binding inteface which is delegated to breez.DownloadBackup
*/
func DownloadBackup(nodeID string) ([]byte, error) {
	files, err := getBreezApp().BackupManager.Download(nodeID)
	if err != nil {
		return nil, err
	}
	return marshalResponse(&data.DownloadBackupResponse{Files: files}, nil)
}

/*
RestoreBackup is part of the binding inteface which is delegated to breez.RestoreBackup
*/
func RestoreBackup(nodeID string, encryptionKey []byte) (err error) {
	oldProvider := getBreezApp().BackupManager.GetProvider()
	if err = getBreezApp().Stop(); err != nil {
		Log("error in calling RestoreBackup: "+err.Error(), "INFO")
		return err
	}
	encKey := append([]byte(nil), encryptionKey...)
	_, err = getBreezApp().BackupManager.Restore(nodeID, encKey)
	if err != nil {
		Log("error in calling BackupManager.Restore: "+err.Error(), "INFO")
	}
	var newAppErr error
	breezApp, newAppErr = breez.NewApp(getBreezApp().GetWorkingDir(), appServices, false)
	if newAppErr != nil {
		Log("error in calling breez.NewAp: "+newAppErr.Error(), "INFO")
	}
	breezApp.BackupManager.SetProvider(oldProvider)
	return err
}

/*
AvailableSnapshots is part of the binding inteface which is delegated to breez.AvailableSnapshots
*/
func AvailableSnapshots() (string, error) {
	Log("Calling Availible Snapshots", "INFO")
	snapshots, err := getBreezApp().BackupManager.AvailableSnapshots()
	if err != nil {
		Log("error in calling AvailableSnapshots: %v"+err.Error(), "INFO")
		return "", err
	}
	if len(snapshots) < 1 {
		return "", errors.New("empty")
	}
	bytes, err := json.Marshal(snapshots)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func TestBackupAuth(provider, authData string) error {
	manager := getBreezApp().BackupManager
	if err := manager.SetBackupProvider(provider, authData); err != nil {
		return errors.New("Failed to set backup provider.")
	}
	p := manager.GetProvider()
	Log(fmt.Sprintf("manager provider is: %v", p), "INFO")
	return p.TestAuth()
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
	app := getBreezApp()
	if app != nil {
		app.OnResume()
	}
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
EnableAccount is part of the binding inteface which is delegated to breez.EnableAccount
*/
func EnableAccount(enabled bool) error {
	return getBreezApp().AccountService.EnableAccount(enabled)
}

/*
AddFundsInit is part of the binding inteface which is delegated to breez.AddFundsInit
*/
func AddFundsInit(initRequest []byte) ([]byte, error) {
	request := &data.AddFundInitRequest{}
	if err := proto.Unmarshal(initRequest, request); err != nil {
		return nil, err
	}
	return marshalResponse(getBreezApp().SwapService.AddFundsInit(
		request.NotificationToken, request.LspID, request.OpeningFeeParams))
}

// RefundFees transfers the funds in address to the user destination address
func RefundFees(refundRequest []byte) (int64, error) {
	Log("binding: starting refund fees flow...", "INFO")
	request := &data.RefundRequest{}
	if err := proto.Unmarshal(refundRequest, request); err != nil {
		return 0, err
	}
	return getBreezApp().SwapService.RefundFees(request.Address, request.RefundAddress, request.TargetConf, request.SatPerByte)
}

// Refund transfers the funds in address to the user destination address
func Refund(refundRequest []byte) (string, error) {
	Log("binding: starting refund flow...", "INFO")
	request := &data.RefundRequest{}
	if err := proto.Unmarshal(refundRequest, request); err != nil {
		return "", err
	}
	return getBreezApp().SwapService.Refund(request.Address, request.RefundAddress, request.TargetConf, request.SatPerByte)
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
	return nil, nil
}

func PopulateChannelPolicy() {
	getBreezApp().PopulateChannelPolicy()
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
SendPaymentForRequest is part of the binding inteface which is delegated to breez.SendPaymentForRequest
*/
func SendPaymentForRequest(payInvoiceRequest []byte) ([]byte, error) {
	decodedRequest := &data.PayInvoiceRequest{}
	proto.Unmarshal(payInvoiceRequest, decodedRequest)

	var errorStr string
	traceReport, err := getBreezApp().AccountService.SendPaymentForRequest(
		decodedRequest.PaymentRequest, decodedRequest.Amount, decodedRequest.Fee)
	if err != nil {
		errorStr = err.Error()
	}
	return marshalResponse(&data.PaymentResponse{TraceReport: traceReport, PaymentError: errorStr}, nil)
}

/*
SendSpontaneousPayment is part of the binding inteface which is delegated to breez.SendSpontaneousPayment
*/
func SendSpontaneousPayment(spontaneousPayment []byte) ([]byte, error) {
	decodedRequest := &data.SpontaneousPaymentRequest{}
	proto.Unmarshal(spontaneousPayment, decodedRequest)

	var errorStr string
	traceReport, err := getBreezApp().AccountService.SendSpontaneousPayment(
		decodedRequest.DestNode, decodedRequest.Description, decodedRequest.Amount,
		decodedRequest.FeeLimitMsat, decodedRequest.GroupKey, decodedRequest.GroupName, decodedRequest.Tlv)

	if err != nil {
		errorStr = err.Error()
	}
	return marshalResponse(&data.PaymentResponse{TraceReport: traceReport, PaymentError: errorStr}, nil)
}

//SpontaneousPaymentRequest

/*
SendPaymentFailureBugReport is part of the binding inteface which is delegated to breez.SendPaymentFailureBugReport
*/
func SendPaymentFailureBugReport(report string) error {
	return getBreezApp().AccountService.SendPaymentFailureBugReport(report)
}

/*
AddInvoice is part of the binding inteface which is delegated to breez.AddInvoice
*/
func AddInvoice(invoice []byte) ([]byte, error) {
	decodedRequest := &data.AddInvoiceRequest{}
	proto.Unmarshal(invoice, decodedRequest)
	payreq, fee, err := getBreezApp().AccountService.AddInvoice(decodedRequest)
	return marshalResponse(&data.AddInvoiceReply{PaymentRequest: payreq, LspFee: fee}, err)
}

func SetNonBlockingUnconfirmedSwaps() error {
	return getBreezApp().SwapService.SetNonBlockingUnconfirmed()
}

/*
DecodePaymentRequest is part of the binding inteface which is delegated to breez.DecodePaymentRequest
*/
func DecodePaymentRequest(paymentRequest string) ([]byte, error) {
	return marshalResponse(getBreezApp().AccountService.DecodePaymentRequest(paymentRequest))
}

/*
GetPaymentRequestHash is part of the binding inteface which is delegated to breez.GetPaymentRequestHash
*/
func GetPaymentRequestHash(paymentRequest string) (string, error) {
	return getBreezApp().AccountService.GetPaymentRequestHash(paymentRequest)
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
	return getBreezApp().AccountService.SendWalletCoins(unmarshaledRequest.Address, unmarshaledRequest.SatPerByteFee)
}

/*
GetDefaultOnChainFeeRate is part of the binding inteface which is delegated to breez.GetDefaultOnChainFeeRate
*/
func GetDefaultOnChainFeeRate() (int64, error) {
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

func TestPeer(peer string) error {

	Log(fmt.Sprintf("api.go: TestPeer: %+v", peer), "INFO")
	return getBreezApp().TestPeer(peer)
}

func DeleteGraph() error {
	return getBreezApp().DeleteGraph()
}

func GraphURL() (string, error) {
	return getBreezApp().GraphUrl()
}

func GetTxSpentURL() ([]byte, error) {
	var t data.TxSpentURL
	txSpentURL, isDefault, err := getBreezApp().GetTxSpentURL()
	if err != nil {
		return nil, err
	}
	t.IsDefault = isDefault
	if txSpentURL == disabledTxSpentURL {
		t.Disabled = true
	} else {
		t.URL = txSpentURL
	}
	return marshalResponse(&t, nil)
}

func SetTxSpentURL(request []byte) error {
	var t data.TxSpentURL
	if err := proto.Unmarshal(request, &t); err != nil {
		return err
	}
	URL := t.URL
	if t.IsDefault {
		URL = ""
	}
	if t.Disabled {
		URL = disabledTxSpentURL
	}
	return getBreezApp().SetTxSpentURL(URL)
}

func TestTxSpentURL(txSpentURL string) error {
	return lnnode.TestTxSpentURL(txSpentURL)
}

func HasClosedChannels() (bool, error) {
	c, err := getBreezApp().ClosedChannels()
	return c > 0, err
}

func Rate() ([]byte, error) {
	return marshalResponse(getBreezApp().ServicesClient.Rates())
}

func ReceiverNode() (string, error) {
	return getBreezApp().ServicesClient.ReceiverNode()
}

func LSPList() ([]byte, error) {
	return marshalResponse(getBreezApp().ServicesClient.LSPList())
}

func LSPActivity() ([]byte, error) {
	var lspList *data.LSPList
	mu.Lock()
	lspList = cachedLSPList
	mu.Unlock()
	var err error
	if lspList == nil {
		lspList, err = getBreezApp().ServicesClient.LSPList()
		if err != nil {
			return nil, err
		}
		mu.Lock()
		cachedLSPList = lspList
		mu.Unlock()
	}
	return marshalResponse(getBreezApp().AccountService.LSPActivity(lspList))
}

func ConnectToLSP(id string) error {
	return getBreezApp().AccountService.OpenLSPChannel(id)
}

func ConnectToLSPPeer(id string) error {
	return getBreezApp().AccountService.ConnectLSPPeer(id)
}

func ConnectToLnurl(lnurl string) error {
	return getBreezApp().AccountService.OpenLnurlChannel(lnurl)
}

func CheckLSPClosedChannelMismatch(request []byte) ([]byte, error) {
	var s data.CheckLSPClosedChannelMismatchRequest
	if err := proto.Unmarshal(request, &s); err != nil {
		return nil, err
	}

	return marshalResponse(getBreezApp().CheckLSPClosedChannelMismatch(&s))
}

func ResetClosedChannelChainInfo(request []byte) ([]byte, error) {
	var r data.ResetClosedChannelChainInfoRequest
	if err := proto.Unmarshal(request, &r); err != nil {
		return nil, err
	}

	return marshalResponse(getBreezApp().ResetClosedChannelChainInfo(&r))
}

func ConnectDirectToLnurl(channel []byte) error {
	var c data.LNURLChannel
	if err := proto.Unmarshal(channel, &c); err != nil {
		return err
	}
	return getBreezApp().AccountService.OpenDirectLnurlChannel(c.K1, c.Callback, c.Uri)
}

func FetchLnurl(lnurl string) ([]byte, error) {
	result, err := marshalResponse(getBreezApp().AccountService.HandleLNURL(lnurl))
	Log(fmt.Sprintf("FetchLnurl: %v", result), "INFO")
	return result, err
}

func FinishLNURLAuth(request []byte) (string, error) {
	var authData data.LNURLAuth
	if err := proto.Unmarshal(request, &authData); err != nil {
		return "", err
	}
	return getBreezApp().AccountService.FinishLNURLAuth(&authData)
}

func WithdrawLnurl(bolt11 string) error {
	return getBreezApp().AccountService.FinishLNURLWithdraw(bolt11)
}

func FinishLNURLPay(request []byte) (result []byte, err error) {

	var d data.LNURLPayResponse1
	if err = proto.Unmarshal(request, &d); err != nil {
		return nil, errors.New("FinishLNURLPay: Failed to unmarshal data.")
	}

	result, err = marshalResponse(getBreezApp().AccountService.FinishLNURLPay(&d))
	if err != nil {
		Log(fmt.Sprintf("FinishLNURLPay error: %s", err), "WARNING")
		return nil, err // FIXME TEST Is this actually returning an error that the client can use?
	}

	Log(fmt.Sprintf("FinishLNURLPay returned: %d bytes", len(result)), "INFO")
	return result, nil
}

func NewReverseSwap(request []byte) (string, error) {
	var swapRequest data.ReverseSwapRequest
	if err := proto.Unmarshal(request, &swapRequest); err != nil {
		return "", err
	}
	h, err := getBreezApp().SwapService.NewReverseSwap(swapRequest.Amount, swapRequest.FeesHash, swapRequest.Address)
	if err != nil {
		var badRequest *boltz.BadRequestError
		if errors.As(err, &badRequest) {
			err = errors.New(badRequest.Error())
		} else {
			var urlError *url.Error
			if errors.As(err, &urlError) {
				err = errors.New(urlError.Error())
			}
		}
	}
	return h, err
}

func MaxReverseSwapAmount() (int64, error) {
	pubKey, err := boltz.GetNodePubkey()
	if err != nil {
		return 0, err
	}

	var routeHints []*lnrpc.RouteHint
	routingNode := getBreezApp().SwapService.ReverseRoutingNode()
	Log("RoutingNode: "+hex.EncodeToString(routingNode), "SEVERE")
	if routingNode != nil {
		if bHints, err := boltz.GetRoutingHints(routingNode); err == nil {
			Log("Bolz Routing Hints: "+fmt.Sprintf("%#v", bHints), "SEVERE")
			for _, bh := range bHints {
				var hopHints []*lnrpc.HopHint
				for _, hh := range bh.HopHintsList {
					chanID, _ := strconv.ParseUint(hh.ChanID, 10, 64)
					hopHints = append(hopHints, &lnrpc.HopHint{
						NodeId:                    hh.NodeID,
						ChanId:                    chanID,
						FeeBaseMsat:               hh.FeeBaseMsat,
						FeeProportionalMillionths: hh.FeeProportionalMillionths,
					})
				}
				routeHints = append(routeHints, &lnrpc.RouteHint{HopHints: hopHints})
			}
		}
	}

	v, err := getBreezApp().AccountService.GetMaxAmount(pubKey, routeHints, routingNode)
	if err != nil {
		return 0, err
	}
	return int64(v) / 1000, nil
}

func ReverseSwapInfo() ([]byte, error) {
	rsi, err := boltz.GetReverseSwapInfo()
	if err != nil {
		return nil, err
	}
	return proto.Marshal(&data.ReverseSwapInfo{
		Min: rsi.Min, Max: rsi.Max,
		Fees: &data.ReverseSwapFees{
			Percentage: rsi.Fees.Percentage, Lockup: rsi.Fees.Lockup, Claim: rsi.Fees.Claim},
		FeesHash: rsi.FeesHash})
}

func SetReverseSwapClaimFee(request []byte) error {
	var r data.ReverseSwapClaimFee
	if err := proto.Unmarshal(request, &r); err != nil {
		return err
	}
	return getBreezApp().SwapService.SetReverseSwapClaimFee(r.Hash, r.Fee)
}

func FetchReverseSwap(hash string) ([]byte, error) {
	return marshalResponse(getBreezApp().SwapService.FetchReverseSwap(hash))
}

func ReverseSwapClaimFeeEstimates(claimAddress string) ([]byte, error) {
	cf, err := getBreezApp().SwapService.ClaimFeeEstimates(claimAddress)
	return marshalResponse(&data.ClaimFeeEstimates{Fees: cf}, err)
}

func PayReverseSwap(request []byte) error {
	var r data.ReverseSwapPaymentRequest
	if err := proto.Unmarshal(request, &r); err != nil {
		return err
	}
	return getBreezApp().SwapService.PayReverseSwap(
		r.Hash,
		r.PushNotificationDetails.DeviceId,
		r.PushNotificationDetails.Title,
		r.PushNotificationDetails.Body,
		r.Fee,
	)
}

func ReverseSwapPayments() ([]byte, error) {
	return marshalResponse(getBreezApp().SwapService.ReverseSwapPayments())
}

func UnconfirmedReverseSwapClaimTransaction() (string, error) {
	return getBreezApp().SwapService.UnconfirmedReverseSwapClaimTransaction()
}

func ResetUnconfirmedReverseSwapClaimTransaction() error {
	return getBreezApp().SwapService.ResetUnconfirmedReverseSwapClaimTransaction()
}

func CheckVersion() error {
	return getBreezApp().CheckVersion()
}

func SweepAllCoinsTransactions(address string) ([]byte, error) {
	return marshalResponse(
		getBreezApp().AccountService.SweepAllCoinsTransactions(address),
	)
}

func SyncGraphFromFile(sourceFilePath string) error {
	Log("SyncGraphFromFile started", "INFO")
	err := bootstrap.SyncGraphDB(getBreezApp().GetWorkingDir(), sourceFilePath)
	if err == bootstrap.ErrMissingPolicyError {
		Log("SyncGraphFromFile missing policy, populating channel policy", "INFO")
		getBreezApp().PopulateChannelPolicy()
		err = bootstrap.SyncGraphDB(getBreezApp().GetWorkingDir(), sourceFilePath)
	}
	Log("SyncGraphFromFile finished", "INFO")
	chandb, clear, err := channeldbservice.Get(getBreezApp().GetWorkingDir())
	if err != nil {
		return err
	}
	defer clear()
	return chandb.ChannelGraph().ReloadCache()
}

func PublishTransaction(tx []byte) error {
	return getBreezApp().AccountService.PublishTransaction(tx)
}

func SetTorActive(enable bool) (err error) {
	return getBreezApp().SetTorActive(enable)
}

func SetBackupTorConfig(torConfig []byte) error {
	Log("Calling SetBackupTorConfig on backupmanager", "INFO")
	_torConfig := &data.TorConfig{}
	if err := proto.Unmarshal(torConfig, _torConfig); err != nil {
		return err
	}
	config := &tor.TorConfig{
		Socks:   _torConfig.Socks,
		Http:    _torConfig.Http,
		Control: _torConfig.Control,
	}
	getBreezApp().BackupManager.SetTorConfig(config)
	return nil
}

func GetTorActive() bool {
	return getBreezApp().GetTorActive()
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
