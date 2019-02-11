//Package breez generate protobuf command
//protoc -I data data/messages.proto --go_out=plugins=grpc:data
package breez

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/breez/breez/backup"
	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lightningclient"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/signal"
	"github.com/lightninglabs/neutrino"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

const (
	letsencryptCert = `-----BEGIN CERTIFICATE-----
MIIEkjCCA3qgAwIBAgIQCgFBQgAAAVOFc2oLheynCDANBgkqhkiG9w0BAQsFADA/
MSQwIgYDVQQKExtEaWdpdGFsIFNpZ25hdHVyZSBUcnVzdCBDby4xFzAVBgNVBAMT
DkRTVCBSb290IENBIFgzMB4XDTE2MDMxNzE2NDA0NloXDTIxMDMxNzE2NDA0Nlow
SjELMAkGA1UEBhMCVVMxFjAUBgNVBAoTDUxldCdzIEVuY3J5cHQxIzAhBgNVBAMT
GkxldCdzIEVuY3J5cHQgQXV0aG9yaXR5IFgzMIIBIjANBgkqhkiG9w0BAQEFAAOC
AQ8AMIIBCgKCAQEAnNMM8FrlLke3cl03g7NoYzDq1zUmGSXhvb418XCSL7e4S0EF
q6meNQhY7LEqxGiHC6PjdeTm86dicbp5gWAf15Gan/PQeGdxyGkOlZHP/uaZ6WA8
SMx+yk13EiSdRxta67nsHjcAHJyse6cF6s5K671B5TaYucv9bTyWaN8jKkKQDIZ0
Z8h/pZq4UmEUEz9l6YKHy9v6Dlb2honzhT+Xhq+w3Brvaw2VFn3EK6BlspkENnWA
a6xK8xuQSXgvopZPKiAlKQTGdMDQMc2PMTiVFrqoM7hD8bEfwzB/onkxEz0tNvjj
/PIzark5McWvxI0NHWQWM6r6hCm21AvA2H3DkwIDAQABo4IBfTCCAXkwEgYDVR0T
AQH/BAgwBgEB/wIBADAOBgNVHQ8BAf8EBAMCAYYwfwYIKwYBBQUHAQEEczBxMDIG
CCsGAQUFBzABhiZodHRwOi8vaXNyZy50cnVzdGlkLm9jc3AuaWRlbnRydXN0LmNv
bTA7BggrBgEFBQcwAoYvaHR0cDovL2FwcHMuaWRlbnRydXN0LmNvbS9yb290cy9k
c3Ryb290Y2F4My5wN2MwHwYDVR0jBBgwFoAUxKexpHsscfrb4UuQdf/EFWCFiRAw
VAYDVR0gBE0wSzAIBgZngQwBAgEwPwYLKwYBBAGC3xMBAQEwMDAuBggrBgEFBQcC
ARYiaHR0cDovL2Nwcy5yb290LXgxLmxldHNlbmNyeXB0Lm9yZzA8BgNVHR8ENTAz
MDGgL6AthitodHRwOi8vY3JsLmlkZW50cnVzdC5jb20vRFNUUk9PVENBWDNDUkwu
Y3JsMB0GA1UdDgQWBBSoSmpjBH3duubRObemRWXv86jsoTANBgkqhkiG9w0BAQsF
AAOCAQEA3TPXEfNjWDjdGBX7CVW+dla5cEilaUcne8IkCJLxWh9KEik3JHRRHGJo
uM2VcGfl96S8TihRzZvoroed6ti6WqEBmtzw3Wodatg+VyOeph4EYpr/1wXKtx8/
wApIvJSwtmVi4MFU5aMqrSDE6ea73Mj2tcMyo5jMd6jmeWUHK8so/joWUoHOUgwu
X4Po1QYz+3dszkDqMp4fklxBwXRsW10KXzPMTZ+sOPAveyxindmjkW8lGy+QsRlG
PfZ+G6Z6h7mjem0Y+iWlkYcV4PIWL1iwBi8saCbGS5jN2p8M+X+Q7UNKEkROb3N6
KOqkqm57TH2H3eDJAkSnh6/DNFu0Qg==
-----END CERTIFICATE-----`
)
const (
	syncToChainDefaultPollingInterval = 3 * time.Second
	syncToChainFastPollingInterval    = 1 * time.Second
	waitBestBlockDuration             = 10 * time.Second
)

var (
	// services passed to breez from the application layer
	appServices AppServices

	// backup manager system in breez
	backupManager *backup.Manager

	cfg                          *config.Config
	lightningClient              lnrpc.LightningClient
	breezClientConnection        *grpc.ClientConn
	breezClientConnectionFailure int32
	connectionMu                 sync.Mutex
	notificationsChan            = make(chan data.NotificationEvent)
	appWorkingDir                string
	initialized                  int32
	isReady                      int32
	started                      int32
	quitChan                     chan struct{}
	breezDB                      *db.DB
	log                          = daemon.BackendLog().Logger("BRUI")
)

// AppServices defined the interface needed in Breez library in order to functional
// right.
type AppServices interface {
	BackupProviderName() string
	BackupProviderSignIn() (string, error)
}

// AuthService is a Specific implementation for backup.Manager
type AuthService struct {
	appServices AppServices
}

// SignIn is the interface function implementation needed for backup.Manager
func (a *AuthService) SignIn() (string, error) {
	return appServices.BackupProviderSignIn()
}

/*
Dependencies is an implementation of interface daemon.Dependencies
used for injecting breez deps int LND running as library
*/
type Dependencies struct {
	workingDir   string
	chainService *neutrino.ChainService
	readyChan    chan interface{}
}

/*
ReadyChan returns the channel passed to LND for getting ready signal
*/
func (d *Dependencies) ReadyChan() chan interface{} {
	return d.readyChan
}

/*
LogPipeWriter returns the io.PipeWriter streamed to the log file.
This will be passed as dependency to LND so breez will have a shared log file
*/
func (d *Dependencies) LogPipeWriter() *io.PipeWriter {
	writer, _ := breezlog.GetLogWriter(d.workingDir)
	return writer
}

/*
ChainService returns a neutrino.ChainService to be used from both sync job and
LND running daemon
*/
func (d *Dependencies) ChainService() *neutrino.ChainService {
	return d.chainService
}

func getBreezClientConnection() *grpc.ClientConn {
	log.Infof("getBreezClientConnection - before Ping;")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(breezClientConnection)
	_, err := ic.Ping(ctx, &breezservice.PingRequest{})
	log.Infof("getBreezClientConnection - after Ping; err: %v", err)
	if grpc.Code(err) == codes.DeadlineExceeded {
		connectionMu.Lock()
		defer connectionMu.Unlock()
		breezClientConnection.Close()
		err = dial()
		log.Infof("getBreezClientConnection - new connection; err: %v", err)
	}
	return breezClientConnection
}

func dial() (err error) {
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(letsencryptCert)) {
		return fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	breezClientConnection, err = grpc.Dial(cfg.BreezServer, dialOptions...)

	return
}

func initBreezClientConnection() error {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	return dial()
}

/*
Start is responsible for starting the lightning client and some go routines to track and notify for account changes
*/
func Start() (ntfnChan chan data.NotificationEvent, err error) {
	if atomic.SwapInt32(&started, 1) == 1 {
		return nil, errors.New("Daemon already started")
	}
	quitChan = make(chan struct{})
	fmt.Println("Breez daemon started")
	go func() {
		defer atomic.StoreInt32(&started, 0)
		defer atomic.StoreInt32(&isReady, 0)
		startLightningDaemon(startBreez)
	}()

	return notificationsChan, nil
}

/*
Init is a required function to be called before Start.
Its main purpose is to make breez services available like:
offline queries and doubleratchet encryption.
*/
func Init(workingDir string, services AppServices) (err error) {
	if atomic.SwapInt32(&initialized, 1) == 1 {
		return nil
	}
	appWorkingDir = workingDir
	appServices = services
	logBackend, err := breezlog.GetLogBackend(workingDir)
	if err != nil {
		return err
	}
	log = logBackend.Logger("BRUI")
	if cfg, err = config.GetConfig(workingDir); err != nil {
		fmt.Println("Warning initConfig", err)
		return err
	}
	err = initBreezClientConnection()
	if err != nil {
		fmt.Println("Error connecting to breez", err)
		return err
	}
	breezDB, err = db.OpenDB(path.Join(appWorkingDir, "breez.db"))
	if err != nil {
		return err
	}
	if err = doubleratchet.Start(path.Join(appWorkingDir, "sessions_encryption.db")); err != nil {
		return err
	}

	bProviderName := appServices.BackupProviderName()
	backupManager, err = backup.NewManager(
		bProviderName,
		&AuthService{appServices: appServices},
		notificationsChan,
		appWorkingDir,
	)
	if err != nil {
		return err
	}

	return nil
}

/*
Stop is responsible for stopping the ligtning daemon.
*/
func Stop() {
	breezDB.CloseDB()
	doubleratchet.Stop()
	connectionMu.Lock()
	breezClientConnection.Close()
	connectionMu.Unlock()
	stopLightningDaemon()
	atomic.StoreInt32(&initialized, 0)
}

/*
WaitDaemonShutdown blocks untill daemon shutdown.
*/
func WaitDaemonShutdown() {
	if atomic.LoadInt32(&started) == 0 {
		return
	}
	select {
	case <-quitChan:
	}
}

/*
DaemonReady return the status of the lightningLib daemon
*/
func DaemonReady() bool {
	return atomic.LoadInt32(&isReady) == 1
}

/*
OnResume recalculate things we might missed when we were idle.
*/
func OnResume() {
	if atomic.LoadInt32(&isReady) == 1 {
		calculateAccountAndNotify()
		if isConnectedToRoutingNode() {
			settlePendingTransfers()
		}
	}
}

func startLightningDaemon(onReady func()) {
	readyChan := make(chan interface{})
	go func() {
		select {
		case <-readyChan:
			atomic.StoreInt32(&isReady, 1)

			//initialize lightning client
			if err := initLightningClient(); err != nil {
				stopLightningDaemon()
				return
			}
			onReady()
		case <-quitChan:
			return
		}
	}()
	var err error
	chainSevice, cleanupFn, err := chainservice.NewService(appWorkingDir)
	if err != nil {
		fmt.Println("Error starting breez", err)
		stopLightningDaemon()
		return
	}
	defer cleanupFn()
	deps := &Dependencies{workingDir: appWorkingDir, chainService: chainSevice, readyChan: readyChan}
	err = daemon.LndMain([]string{"lightning-libs", "--lnddir", appWorkingDir, "--bitcoin." + cfg.Network}, deps)
	if err != nil {
		fmt.Println("Error starting breez", err)
	}
	stopLightningDaemon()
}

func stopLightningDaemon() {
	alive := signal.Alive()
	log.Infof("stopLightningDaemon called, stopping breez daemon alive=%v", alive)
	if alive {
		signal.RequestShutdown()
	}
	close(quitChan)
	if backupManager != nil {
		backupManager.Stop()
	}
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_LIGHTNING_SERVICE_DOWN}
}

func startBreez() {
	//start the go routings
	backupManager.Start(lightningClient, breezDB)
	go trackOpenedChannel()
	go watchRoutingNodeConnection()
	go watchPayments()
	watchFundTransfers()
	go func() {
		onAccountChanged()
		err := syncToChain(syncToChainDefaultPollingInterval)
		if err != nil {
			log.Errorf("Failed to sync chain %v", err)
		}
		go connectOnStartup()
		go watchOnChainState()
	}()

	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_READY}
}

func initLightningClient() error {
	var clientError error
	macaroonDir := strings.Join([]string{appWorkingDir, "data", "chain", "bitcoin", cfg.Network}, "/")
	lightningClient, clientError = lightningclient.NewLightningClient(appWorkingDir, macaroonDir)
	if clientError != nil {
		log.Errorf("Error in creating client", clientError)
		notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_INITIALIZATION_FAILED}
		return clientError
	}
	return nil
}

func connectOnStartup() {
	channelPoints, err := getBreezOpenChannelsPoints()
	if err != nil {
		log.Errorf("connectOnStartup: error in getBreezOpenChannelsPoints", err)
		return
	}
	pendingChannels, err := lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		log.Errorf("connectOnStartup: error in PendingChannels", err)
		return
	}
	if len(channelPoints) > 0 || len(pendingChannels.PendingOpenChannels) > 0 {
		log.Infof("connectOnStartup: already has a channel, ignoring manual connection")
		return
	}

	connectRoutingNode()
}
