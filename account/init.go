package account

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnotifier"
	"github.com/breez/breez/services"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/btcsuite/btclog"
)

// Service is the account service that controls all aspects of routing node connection
// and user channels as an abstracted account.
type Service struct {
	started           int32
	stopped           int32
	daemonReady       int32
	wg                sync.WaitGroup
	cfg               *config.Config
	breezDB           *db.DB
	breezServices     *services.Client
	log               btclog.Logger
	lnNotifier        *lnnotifier.LNNodeNotifier
	connectedNotifier onlineNotifier
	lightningClient   lnrpc.LightningClient
	onServiceEvent    func(data.NotificationEvent)

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
	lightningClient lnrpc.LightningClient,
	onServiceEvent func(data.NotificationEvent)) *Service {

	return &Service{
		cfg:             cfg,
		breezDB:         breezDB,
		breezServices:   breezServices,
		lightningClient: lightningClient,
		onServiceEvent:  onServiceEvent,
		quitChan:        make(chan struct{}),
	}
}
