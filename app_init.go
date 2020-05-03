package breez

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"runtime/debug"
	"sync"

	"github.com/breez/breez/account"
	"github.com/breez/breez/backup"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/breez/breez/swapfunds"
	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/breezbackuprpc"
	"golang.org/x/net/context"
)

// App represents the breez application
type App struct {
	isReady        int32
	started        int32
	stopped        int32
	wg             sync.WaitGroup
	connectionMu   sync.Mutex
	quitChan       chan struct{}
	log            btclog.Logger
	cfg            *config.Config
	breezDB        *db.DB
	releaseBreezDB func() error

	// services passed to breez from the application layer
	//appServices AppServices

	//exposed sub services
	AccountService *account.Service
	BackupManager  *backup.Manager
	SwapService    *swapfunds.Service
	ServicesClient *services.Client

	//non exposed services
	lnDaemon *lnnode.Daemon

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
func NewApp(workingDir string, applicationServices AppServices, startBeforeSync bool) (*App, error) {
	app := &App{
		quitChan:          make(chan struct{}),
		notificationsChan: make(chan data.NotificationEvent),
	}
	debug.SetTraceback("crash")
	logBackend, err := breezlog.GetLogBackend(workingDir)
	if err != nil {
		return nil, err
	}

	app.log = logBackend.Logger("BRUI")

	app.cfg, err = config.GetConfig(workingDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to get config file: %v", err)
	}

	app.ServicesClient, err = services.NewClient(app.cfg)
	if err != nil {
		return nil, fmt.Errorf("Error creating services.Client: %v", err)
	}

	app.log.Infof("New Client")

	app.breezDB, app.releaseBreezDB, err = db.Get(workingDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialze breezDB: %v", err)
	}

	app.log.Infof("New db")

	app.lnDaemon, err = lnnode.NewDaemon(app.cfg, app.breezDB, startBeforeSync)
	if err != nil {
		return nil, fmt.Errorf("Failed to create lnnode.Daemon: %v", err)
	}

	app.log.Infof("New daemon")

	if err := doubleratchet.Start(path.Join(app.cfg.WorkingDir, "sessions_encryption.db")); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf("Failed to start doubleratchet: %v", err)
	}

	app.BackupManager, err = backup.NewManager(
		applicationServices.BackupProviderName(),
		&AuthService{appServices: applicationServices},
		app.onServiceEvent,
		app.prepareBackupInfo,
		app.cfg,
		logBackend.Logger("BCKP"),
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to start backup manager: %v", err)
	}
	app.log.Infof("New backup")

	app.AccountService, err = account.NewService(
		app.cfg,
		app.breezDB,
		app.ServicesClient,
		app.lnDaemon,
		app.onServiceEvent,
	)
	app.log.Infof("New AccountService")
	if err != nil {
		return nil, fmt.Errorf("Failed to create AccountService: %v", err)
	}

	app.SwapService, err = swapfunds.NewService(
		app.cfg,
		app.breezDB,
		app.ServicesClient,
		app.lnDaemon,
		app.AccountService.SendPaymentForRequest,
		app.AccountService.GetAccountLimits,
		app.onServiceEvent,
	)
	app.log.Infof("New SwapService")
	if err != nil {
		return nil, fmt.Errorf("Failed to create SwapService: %v", err)
	}

	app.log.Infof("app initialized")
	return app, nil
}

// extractBackupInfo extracts the information that is needed for the external backup service:
// 1. paths - the files need to be backed up.
// 2. nodeID - the current lightning node id.
func (a *App) prepareBackupInfo() (paths []string, nodeID string, err error) {
	a.log.Infof("extractBackupInfo started")
	lnclient := a.lnDaemon.APIClient()
	if lnclient == nil {
		return nil, "", errors.New("Daemon is not ready")
	}
	backupClient := a.lnDaemon.BreezBackupClient()
	if backupClient == nil {
		return nil, "", errors.New("Daemon is not ready")
	}

	response, err := backupClient.GetBackup(context.Background(), &breezbackuprpc.GetBackupRequest{})
	if err != nil {
		a.log.Errorf("Couldn't get backup: %v", err)
		return nil, "", err
	}
	info, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
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
