package lnnode

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/signal"
	"github.com/btcsuite/btclog"
)

// DamonState is the type for daemon notifications
type DamonState int32

const (
	// DaemonReady is a notification trigerred when the damon is ready to receive
	// RPC requests
	DaemonReady DamonState = iota

	// DaemonStopped is a notification trigerred when the damon is stopped
	DaemonStopped
)

// Daemon contains data regarding the lightning daemon.
type Daemon struct {
	started       int32
	ready         int32
	stopped       int32
	wg            sync.WaitGroup
	log           btclog.Logger
	stateNotifier chan DamonState
	quitChan      chan interface{}
}

// NewDaemon is used to create a new daemon that wraps a lightning
// network daemon.
func NewDaemon(logger btclog.Logger, stateNotifier chan DamonState) (*Daemon, error) {
	return &Daemon{
		log:           logger,
		stateNotifier: stateNotifier,
		quitChan:      make(chan interface{}),
	}, nil
}

// Start is used to start the lightning network daemon.
func (d *Daemon) Start(appWorkingDir string, cfg *config.Config) error {
	if atomic.SwapInt32(&d.started, 1) == 1 {
		return errors.New("Daemon already started")
	}

	readyChan := make(chan interface{})
	chanDB, chanDBCleanUp, err := channeldbservice.NewService(appWorkingDir)
	if err != nil {
		d.log.Errorf("Error starting breez", err)
		return err
	}
	chainSevice, cleanupFn, err := chainservice.NewService(appWorkingDir)
	if err != nil {
		chanDBCleanUp()
		d.log.Errorf("Error starting breez", err)
		return err
	}
	deps := &Dependencies{
		workingDir:   appWorkingDir,
		chainService: chainSevice,
		readyChan:    readyChan,
		chanDB:       chanDB}

	d.wg.Add(1)
	go func() {
		d.runLightningDaemon(cfg, deps)
		chanDBCleanUp()
		cleanupFn()
		d.wg.Done()
	}()

	return nil
}

// Stop is used to stop the lightning network daemon.
func (d *Daemon) Stop() error {
	if atomic.SwapInt32(&d.stopped, 1) == 0 {
		alive := signal.Alive()
		d.log.Infof("stopLightningDaemon called, stopping breez daemon alive=%v", alive)
		if alive {
			signal.RequestShutdown()
		}
	}
	d.wg.Wait()
	return nil
}

func (d *Daemon) runLightningDaemon(cfg *config.Config, deps *Dependencies) {
	go d.notifyReady(deps.readyChan)

	err := daemon.LndMain(
		[]string{"lightning-libs", "--lnddir",
			deps.workingDir, "--bitcoin." + cfg.Network},
		deps,
	)

	if err != nil {
		d.log.Errorf("Error starting breez", err)
	}
	signal.RequestShutdown()
	close(d.quitChan)
	d.stateNotifier <- DaemonStopped
}

func (d *Daemon) notifyReady(readyChan chan interface{}) {
	defer d.wg.Done()

	select {
	case <-readyChan:
		atomic.StoreInt32(&d.ready, 1)
		d.stateNotifier <- DaemonReady
	case <-d.quitChan:
	}
}
