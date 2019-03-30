package swapfunds

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnotifier"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/subscribe"
	"github.com/btcsuite/btclog"
)

type Service struct {
	started            int32
	stopped            int32
	wg                 sync.WaitGroup
	mu                 sync.Mutex
	cfg                *config.Config
	log                btclog.Logger
	daemonEventsClient *subscribe.Client
	breezDB            *db.DB
	lnNotifier         *lnnotifier.LNNodeNotifier
	lightningClient    lnrpc.LightningClient
	breezServices      *services.Client
	sendPayment        func(payreq string, amount int64) error
	onServiceEvent     func(data.NotificationEvent)
	accountPubkey      string
	quitChan           chan struct{}
}

func NewService(
	cfg *config.Config,
	breezDB *db.DB,
	breezServices *services.Client,
	lightningClient lnrpc.LightningClient,
	sendPayment func(payreq string, amount int64) error,
	onServiceEvent func(data.NotificationEvent),
	lnNotifier *lnnotifier.LNNodeNotifier) (*Service, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:             cfg,
		lightningClient: lightningClient,
		breezDB:         breezDB,
		breezServices:   breezServices,
		sendPayment:     sendPayment,
		onServiceEvent:  onServiceEvent,
		log:             logBackend.Logger("FUNDS"),
		lnNotifier:      lnNotifier,
		quitChan:        make(chan struct{}),
	}, nil
}
