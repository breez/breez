package account

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/subscribe"
	"github.com/btcsuite/btclog"
)

// Service is the account service that controls all aspects of routing node connection
// and user channels as an abstracted account.
type Service struct {
	started            int32
	stopped            int32
	daemonReady        int32
	wg                 sync.WaitGroup
	cfg                *config.Config
	breezDB            *db.DB
	daemonSubscription *subscribe.Client
	breezServices      *services.Client
	log                btclog.Logger
	daemon             *lnnode.Daemon
	connectedNotifier  *onlineNotifier
	lightningClient    lnrpc.LightningClient
	onServiceEvent     func(data.NotificationEvent)

	subscriptionsSync sync.Mutex
	notification      *notificationRequest

	quitChan chan struct{}
}

type notificationRequest struct {
	token            string
	notificationType int
}

// NewService creates a new account service
func NewService(
	cfg *config.Config,
	breezDB *db.DB,
	breezServices *services.Client,
	daemon *lnnode.Daemon,
	onServiceEvent func(data.NotificationEvent)) (*Service, error) {

	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:               cfg,
		log:               logBackend.Logger("ACCNT"),
		connectedNotifier: newOnlineNotifier(),
		daemon:            daemon,
		breezDB:           breezDB,
		breezServices:     breezServices,
		onServiceEvent:    onServiceEvent,
		quitChan:          make(chan struct{}),
	}, nil
}
