package lnnode

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	breezlog "github.com/breez/breez/log"
	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/breezbackuprpc"
	"github.com/lightningnetwork/lnd/lnrpc/submarineswaprpc"
	"github.com/lightningnetwork/lnd/subscribe"
)

// API represents the lnnode exposed functions that are accessible for
// breez services to use.
// It is mainly enable the service to subscribe to various daemon events
// and get an APIClient to query the daemon directly via RPC.
type API interface {
	SubscribeEvents() (*subscribe.Client, error)
	ConnectedToRoutingNode() bool
	HasChannelWithRoutingNode() bool
	NodePubkey() string
	APIClient() lnrpc.LightningClient
	SubSwapClient() submarineswaprpc.SubmarineSwapperClient
	BreezBackupClient() breezbackuprpc.BreezBackuperClient
}

// Daemon contains data regarding the lightning daemon.
type Daemon struct {
	sync.Mutex
	cfg                       *config.Config
	breezDB                   *db.DB
	started                   int32
	stopped                   int32
	daemonRunning             bool
	connectedToRoutingNode    bool
	hasChannelWithRoutingNode bool
	nodePubkey                string
	wg                        sync.WaitGroup
	log                       btclog.Logger
	lightningClient           lnrpc.LightningClient
	subswapClient             submarineswaprpc.SubmarineSwapperClient
	breezBackupClient         breezbackuprpc.BreezBackuperClient
	ntfnServer                *subscribe.Server
	quitChan                  chan struct{}
}

// NewDaemon is used to create a new daemon that wraps a lightning
// network daemon.
func NewDaemon(cfg *config.Config, db *db.DB) (*Daemon, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Daemon{
		cfg:        cfg,
		breezDB:    db,
		ntfnServer: subscribe.NewServer(),
		log:        logBackend.Logger("DAEM"),
	}, nil
}
