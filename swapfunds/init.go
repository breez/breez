package swapfunds

import (
	"sync"

	"github.com/breez/breez/account"
	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/breez/services"
	"github.com/btcsuite/btclog"
)

type Service struct {
	started        int32
	stopped        int32
	wg             sync.WaitGroup
	mu             sync.Mutex
	cfg            *config.Config
	log            btclog.Logger
	breezDB        *db.DB
	daemonAPI      lnnode.API
	breezAPI       services.API
	sendPayment    func(payreq string, amount int64) (*account.PaymentResponse, error)
	onServiceEvent func(data.NotificationEvent)
	quitChan       chan struct{}
}

func NewService(
	cfg *config.Config,
	breezDB *db.DB,
	breezAPI services.API,
	daemonAPI lnnode.API,
	sendPayment func(payreq string, amount int64) (*account.PaymentResponse, error),
	onServiceEvent func(data.NotificationEvent)) (*Service, error) {

	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:            cfg,
		breezDB:        breezDB,
		breezAPI:       breezAPI,
		sendPayment:    sendPayment,
		onServiceEvent: onServiceEvent,
		log:            logBackend.Logger("FUNDS"),
		daemonAPI:      daemonAPI,
		quitChan:       make(chan struct{}),
	}, nil
}
