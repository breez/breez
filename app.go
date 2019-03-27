package breez

import (
	"sync"

	"github.com/breez/breez/backup"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/btcsuite/btclog"
	"google.golang.org/grpc"
)

type App struct {
	// services passed to breez from the application layer
	appServices AppServices

	// backup manager system in breez
	backupManager *backup.Manager

	cfg                          *config.Config
	lightningClient              lnrpc.LightningClient
	breezClientConnection        *grpc.ClientConn
	breezClientConnectionFailure int32
	connectionMu                 sync.Mutex
	notificationsChan            chan data.NotificationEvent
	appWorkingDir                string
	initialized                  int32
	isReady                      int32
	started                      int32
	quitChan                     chan struct{}
	breezDB                      *db.DB
	lnDaemon                     *lnnode.Daemon
	log                          btclog.Logger
}

func (a *App) Start() error {
	return nil
}

func (a *App) Stop() error {
	return nil
}

func (a *App) startAppServices() error {
	return nil
}

func (a *App) stopAppServices() error {
	return nil
}
