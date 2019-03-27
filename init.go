//Package breez generate protobuf command
//protoc -I data data/messages.proto --go_out=plugins=grpc:data
package breez

import (
	"errors"
	"fmt"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/breez/breez/backup"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lightningclient"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
	"google.golang.org/grpc"
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

	lnDaemon                     *lnnode.Daemon
	servicesClient               *services.Client
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
Start is responsible for starting the lightning client and some go routines to track and notify for account changes
*/
func Start() (ntfnChan chan data.NotificationEvent, err error) {
	if atomic.SwapInt32(&started, 1) == 1 {
		return nil, errors.New("Breez already started")
	}

	quitChan = make(chan struct{})
	lightningClient, err = lightningclient.NewLightningClient(appWorkingDir, cfg.Network)
	if err != nil {
		return nil, fmt.Errorf("Error in initializing lightning client: %v", err)
	}

	if err := lnDaemon.Start(); err != nil {
		return nil, fmt.Errorf("Error starting lnnode.Daemon: %v", err)
	}
	log.Infof("Breez daemon started")
	go watchDeamonState()
	return notificationsChan, nil
}

func watchDeamonState() {
	for {
		select {
		case <-lnDaemon.ReadyChan():
			atomic.StoreInt32(&isReady, 1)
			startBreezServices()
		case <-lnDaemon.QuitChan():
			atomic.StoreInt32(&started, 0)
			atomic.StoreInt32(&isReady, 0)
			close(quitChan)
			quitChan = nil
			notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_LIGHTNING_SERVICE_DOWN}
			return
		}
	}
}

/*
Init is a required function to be called before Start.
Its main purpose is to make breez services available like:
offline queries and doubleratchet encryption.
*/
func Init(workingDir string, applicationServices AppServices) (err error) {
	if atomic.SwapInt32(&initialized, 1) == 1 {
		return nil
	}
	appWorkingDir = workingDir
	appServices = applicationServices
	logBackend, err := breezlog.GetLogBackend(workingDir)
	if err != nil {
		return err
	}
	log = logBackend.Logger("BRUI")
	if cfg, err = config.GetConfig(workingDir); err != nil {
		fmt.Println("Warning initConfig", err)
		return err
	}
	servicesClient, err = services.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("Error creating services.Client: %v", err)
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
		prepareBackupInfo,
		cfg,
		appWorkingDir,
	)
	if err != nil {
		return err
	}

	lnDaemon, err = lnnode.NewDaemon(cfg)
	if err != nil {
		return fmt.Errorf("Error creating lnnode.Daemon: %v", err)
	}

	if err := servicesClient.Start(); err != nil {
		return fmt.Errorf("Error starting servicesClient: %v", err)
	}

	return nil
}

/*
Stop is responsible for stopping the ligtning daemon.
*/
func Stop() {
	breezDB.CloseDB()
	backupManager.Stop()
	doubleratchet.Stop()
	connectionMu.Lock()
	breezClientConnection.Close()
	connectionMu.Unlock()
	lnDaemon.Stop()
	atomic.StoreInt32(&initialized, 0)
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

func startBreezServices() {
	//start the go routings
	backupManager.Start()
	if !ensureSafeToRunNode() {
		notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_NODE_CONFLICT}
		lnDaemon.Stop()
		return
	}
	go watchBackupEvents()
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
