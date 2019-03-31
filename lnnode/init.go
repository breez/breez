package lnnode

import (
	"sync"

	"github.com/breez/breez/config"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/subscribe"
	"github.com/btcsuite/btclog"
)

// Daemon contains data regarding the lightning daemon.
type Daemon struct {
	sync.Mutex
	cfg             *config.Config
	started         int32
	stopped         int32
	daemonRunning   bool
	wg              sync.WaitGroup
	log             btclog.Logger
	lightningClient lnrpc.LightningClient
	ntfnServer      *subscribe.Server
	quitChan        chan struct{}
}

// NewDaemon is used to create a new daemon that wraps a lightning
// network daemon.
func NewDaemon(cfg *config.Config) (*Daemon, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Daemon{
		cfg:        cfg,
		ntfnServer: subscribe.NewServer(),
		log:        logBackend.Logger("DAEM"),
	}, nil
}
