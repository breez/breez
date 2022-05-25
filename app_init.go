package breez

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"

	"github.com/breez/breez/account"
	"github.com/breez/breez/backup"
	"github.com/breez/breez/chainservice"
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

	lspChanStateSyncer *lspChanStateSync
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

	logger, err := breezlog.GetLogger(workingDir, "BRUI")
	if err != nil {
		return nil, err
	}
	app.log = logger

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
	walletdbPath := app.cfg.WorkingDir + "/data/chain/bitcoin/" + app.cfg.Network + "/wallet.db"
	walletDBInfo, err := os.Stat(walletdbPath)
	if err == nil {
		app.log.Infof("wallet db size is: %v", walletDBInfo.Size())
	}
	if err = compactWalletDB(app.cfg.WorkingDir+"/data/chain/bitcoin/"+app.cfg.Network, app.log); err != nil {
		return nil, err
	}

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

	backupLogger, err := breezlog.GetLogger(workingDir, "BCKP")
	if err != nil {
		return nil, err
	}
	app.BackupManager, err = backup.NewManager(
		applicationServices.BackupProviderName(),
		&AuthService{appServices: applicationServices},
		app.onServiceEvent,
		app.prepareBackupInfo,
		app.cfg,
		backupLogger,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to start backup manager: %v", err)
	}
	app.log.Infof("New backup")

	app.lspChanStateSyncer = newLSPChanStateSync(app)

	app.AccountService, err = account.NewService(
		app.cfg,
		app.breezDB,
		app.ServicesClient,
		app.lnDaemon,
		app.RequestBackup,
		app.lspChanStateSyncer.unconfirmedChannelsInSync,
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
		app.AccountService.SendPaymentForRequestV2,
		app.AccountService.AddInvoice,
		app.ServicesClient.LSPList,
		app.AccountService.GetGlobalMaxReceiveLimit,
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
	chanBackupFile := a.cfg.WorkingDir + "/data/chain/bitcoin/" + a.cfg.Network + "/channel.backup"
	_, err = os.Stat(chanBackupFile)
	if err != nil {
		a.log.Infof("Not adding channel.backup to the backup: %v", err)
	}

	if err == nil {
		files = append(files, chanBackupFile)
		a.log.Infof("adding channel.backup to the backup")
	}
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

func compactWalletDB(walletDBDir string, logger btclog.Logger) error {
	dbName := "wallet.db"
	dbPath := path.Join(walletDBDir, dbName)
	f, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if f.Size() <= 10000000 {
		return nil
	}
	newFile, err := ioutil.TempFile(walletDBDir, "cdb-compact")
	if err != nil {
		return err
	}
	if err = chainservice.BoltCopy(dbPath, newFile.Name(),
		func(keyPath [][]byte, k []byte, v []byte) bool { return false }); err != nil {
		return err
	}
	if err = os.Rename(dbPath, dbPath+".old"); err != nil {
		return err
	}
	if err = os.Rename(newFile.Name(), dbPath); err != nil {
		logger.Criticalf("Error when renaming the new walletdb file: %v", err)
		return err
	}
	logger.Infof("wallet.db was compacted because it's too big")
	return nil
}
