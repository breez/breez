package lnnode

import (
	"sync"

	"github.com/breez/breez/config"
	breezlog "github.com/breez/breez/log"
	"github.com/btcsuite/btclog"
)

// DamonState is the type for daemon notifications
type DaemonState int32

const (
	// DaemonReady is a notification trigerred when the damon is ready to receive
	// RPC requests
	DaemonReady DaemonState = iota

	// DaemonStopped is a notification trigerred when the damon is stopped
	DaemonStopped
)

// Daemon contains data regarding the lightning daemon.
type Daemon struct {
	cfg           *config.Config
	started       int32
	ready         int32
	stopped       int32
	wg            sync.WaitGroup
	log           btclog.Logger
	stateNotifier chan DaemonState
	quitChan      chan interface{}
}

// NewDaemon is used to create a new daemon that wraps a lightning
// network daemon.
func NewDaemon(cfg *config.Config, stateNotifier chan DaemonState) (*Daemon, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &Daemon{
		cfg:           cfg,
		log:           logBackend.Logger("DAEM"),
		stateNotifier: stateNotifier,
		quitChan:      make(chan interface{}),
	}, nil
}
