package lnnode

import (
	"sync"

	"github.com/breez/breez/config"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/lightninglib/subscribe"
	"github.com/btcsuite/btclog"
)

// Daemon contains data regarding the lightning daemon.
type Daemon struct {
	cfg          *config.Config
	started      int32
	ready        int32
	stopped      int32
	wg           sync.WaitGroup
	log          btclog.Logger
	notifier     subscribe.Server
	rpcReadyChan chan interface{}
	quitChan     chan interface{}
}

//Events

// RPCReadyNotification sent when the daeon is ready to receive RPC requests.
type RPCReadyNotification struct{}

// DaemonShutdwonNotification sent when the daemon has shut down.
type DaemonShutdwonNotification struct{}

// PeerConnectionChangedNotification is sent when a peer is connected/disconected.
type PeerConnectionChangedNotification struct {
	pubKey    string
	connected bool
}

// NewDaemon is used to create a new daemon that wraps a lightning
// network daemon.
func NewDaemon(cfg *config.Config) (*Daemon, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Daemon{
		cfg:          cfg,
		log:          logBackend.Logger("DAEM"),
		notifier:     *subscribe.NewServer(),
		rpcReadyChan: make(chan interface{}),
		quitChan:     make(chan interface{}),
	}, nil
}
