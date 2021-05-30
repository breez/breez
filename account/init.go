package account

import (
	"fmt"
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/btcsuite/btcd/chaincfg"
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
	onServiceEvent     func(data.NotificationEvent)
	requestBackup      func()
	lnurlWithdrawing   string
	activeParams       *chaincfg.Params
	lspReadyPayment    func() (bool, error)

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
	requestBackup func(),
	lspReadyPayment func() (bool, error),
	onServiceEvent func(data.NotificationEvent)) (*Service, error) {

	logger, err := breezlog.GetLogger(cfg.WorkingDir, "ACCNT")
	if err != nil {
		return nil, err
	}

	var activeParams *chaincfg.Params

	if cfg.Network == "testnet" {
		activeParams = &chaincfg.TestNet3Params
	} else if cfg.Network == "simnet" {
		activeParams = &chaincfg.SimNetParams
	} else if cfg.Network == "mainnet" {
		activeParams = &chaincfg.MainNetParams
	} else {
		return nil, fmt.Errorf("unknown network type: %v", cfg.Network)
	}

	return &Service{
		cfg:             cfg,
		log:             logger,
		daemonAPI:       daemonAPI,
		breezDB:         breezDB,
		breezAPI:        breezAPI,
		onServiceEvent:  onServiceEvent,
		quitChan:        make(chan struct{}),
		activeParams:    activeParams,
		requestBackup:   requestBackup,
		lspReadyPayment: lspReadyPayment,
	}, nil
}
