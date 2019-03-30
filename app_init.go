package breez

import (
	"fmt"
	"io/ioutil"
	"path"
	"sync"

	"github.com/breez/breez/account"
	"github.com/breez/breez/backup"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/breez/breez/swapfunds"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/btcsuite/btclog"
	"golang.org/x/net/context"
)

// App represents the breez application
type App struct {
	isReady      int32
	started      int32
	stopped      int32
	wg           sync.WaitGroup
	connectionMu sync.Mutex
	quitChan     chan struct{}
	log          btclog.Logger
	cfg          *config.Config
	breezDB      *db.DB

	// services passed to breez from the application layer
	//appServices AppServices

	// sub services
	accountService  *account.Service
	backupManager   *backup.Manager
	swapService     *swapfunds.Service
	lnDaemon        *lnnode.Daemon
	servicesClient  *services.Client
	lightningClient lnrpc.LightningClient

	//channel for external binding events
	notificationsChan chan data.NotificationEvent
}

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
	return a.appServices.BackupProviderSignIn()
}

// NewApp create a new application
func NewApp(workingDir string, applicationServices AppServices) (*App, error) {
	app := &App{
		//appServices:       applicationServices,
		notificationsChan: make(chan data.NotificationEvent),
	}

	logBackend, err := breezlog.GetLogBackend(workingDir)
	if err != nil {
		return nil, err
	}
	app.log = logBackend.Logger("BRUI")

	app.cfg, err = config.GetConfig(workingDir)
	if err != nil {
		fmt.Println("Warning initConfig", err)
		return nil, err
	}

	app.lightningClient, err = lnnode.NewLightningClient(app.cfg)
	if err != nil {
		return nil, fmt.Errorf("Error in initializing lightning client: %v", err)
	}

	app.servicesClient, err = services.NewClient(app.cfg)
	if err != nil {
		return nil, fmt.Errorf("Error creating services.Client: %v", err)
	}

	app.breezDB, err = db.OpenDB(path.Join(workingDir, "breez.db"))
	if err != nil {
		return nil, err
	}

	app.lnDaemon, err = lnnode.NewDaemon(app.cfg)
	if err != nil {
		return nil, fmt.Errorf("Error creating lnnode.Daemon: %v", err)
	}

	if err != nil {
		return nil, err
	}

	app.backupManager, err = backup.NewManager(
		applicationServices.BackupProviderName(),
		&AuthService{appServices: applicationServices},
		app.onServiceEvent,
		app.prepareBackupInfo,
		app.cfg,
	)
	if err != nil {
		return nil, err
	}

	app.accountService, err = account.NewService(
		app.cfg,
		app.breezDB,
		app.servicesClient,
		app.lnDaemon,
		app.onServiceEvent,
	)
	if err != nil {
		return nil, err
	}

	app.swapService, err = swapfunds.NewService(
		app.cfg,
		app.breezDB,
		app.servicesClient,
		app.lnDaemon,
		app.accountService.SendPaymentForRequest,
		app.onServiceEvent,
	)
	if err != nil {
		return nil, err
	}

	return app, nil
}

func (a *App) onServiceEvent(event data.NotificationEvent) {
	a.notificationsChan <- event
	if event.Type == data.NotificationEvent_ROUTING_NODE_CONNECTION_CHANGED {
		if a.accountService.IsConnectedToRoutingNode() {
			a.ensureSafeToRunNode()
		}
	}
	if event.Type == data.NotificationEvent_FUND_ADDRESS_CREATED {
		a.backupManager.RequestBackup()
	}
}

// extractBackupInfo extracts the information that is needed for the external backup service:
// 1. paths - the files need to be backed up.
// 2. nodeID - the current lightning node id.
func (a *App) prepareBackupInfo() (paths []string, nodeID string, err error) {
	a.log.Infof("extractBackupInfo started")
	response, err := a.lightningClient.GetBackup(context.Background(), &lnrpc.GetBackupRequest{})
	if err != nil {
		a.log.Errorf("Couldn't get backup: %v", err)
		return nil, "", err
	}
	info, err := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, "", err
	}

	f, err := a.breezdbCopy(a.breezDB)
	if err != nil {
		a.log.Errorf("Couldn't get breez backup file: %v", err)
		return nil, "", err
	}
	files := append(response.Files, f)
	a.log.Infof("extractBackupInfo completd")
	return files, info.IdentityPubkey, nil
}

func (a *App) breezdbCopy(breezDB *db.DB) (string, error) {
	dir, err := ioutil.TempDir("", "backup")
	if err != nil {
		return "", err
	}
	return a.breezDB.BackupDb(dir)
}
