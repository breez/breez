package account

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/subscribe"
)

// Service is the account service that controls all aspects of routing node connection
// and user channels as an abstracted account.
type Service struct {
	started            int32
	stopped            int32
	daemonReady        int32
	wg                 sync.WaitGroup
	mu                 sync.Mutex
	cfg                *config.Config
	breezDB            *db.DB
	daemonSubscription *subscribe.Client
	breezAPI           services.API
	log                btclog.Logger
	daemonAPI          lnnode.API
	connectedNotifier  *onlineNotifier
	onServiceEvent     func(data.NotificationEvent)
	LSPId              string

	notification *notificationRequest

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
	breezAPI services.API,
	daemonAPI lnnode.API,
	onServiceEvent func(data.NotificationEvent)) (*Service, error) {

	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:               cfg,
		log:               logBackend.Logger("ACCNT"),
		connectedNotifier: newOnlineNotifier(),
		daemonAPI:         daemonAPI,
		breezDB:           breezDB,
		breezAPI:          breezAPI,
		onServiceEvent:    onServiceEvent,
		quitChan:          make(chan struct{}),
	}, nil
}
