package lnnode

import (
	"errors"
	"sync/atomic"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/signal"
)

// Start is used to start the lightning network daemon.
func (d *Daemon) Start() error {
	if atomic.SwapInt32(&d.started, 1) == 1 {
		return errors.New("Daemon already started")
	}

	readyChan := make(chan interface{})
	chanDB, chanDBCleanUp, err := channeldbservice.NewService(d.cfg.WorkingDir)
	if err != nil {
		return err
	}
	chainSevice, cleanupFn, err := chainservice.NewService(d.cfg.WorkingDir)
	if err != nil {
		chanDBCleanUp()
		return err
	}
	deps := &Dependencies{
		workingDir:   d.cfg.WorkingDir,
		chainService: chainSevice,
		readyChan:    readyChan,
		chanDB:       chanDB}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		d.runLightningDaemon(d.cfg, deps)
		chanDBCleanUp()
		cleanupFn()
	}()

	return nil
}

// Stop is used to stop the lightning network daemon.
func (d *Daemon) Stop() error {
	d.stop()
	d.wg.Wait()
	return nil
}

func (d *Daemon) stop() {
	if atomic.SwapInt32(&d.stopped, 1) == 0 {
		alive := signal.Alive()
		d.log.Infof("stopLightningDaemon called, stopping breez daemon alive=%v", alive)
		if alive {
			signal.RequestShutdown()
		}
		close(d.quitChan)
	}
}

func (d *Daemon) runLightningDaemon(cfg *config.Config, deps *Dependencies) {
	d.wg.Add(1)
	go d.notifyWhenReady(deps.readyChan)

	err := daemon.LndMain(
		[]string{"lightning-libs", "--lnddir",
			deps.workingDir, "--bitcoin." + cfg.Network,
		},
		deps,
	)

	if err != nil {
		d.log.Errorf("Breez failed with error: %v", err)
	}
	d.stateNotifier <- DaemonStopped
	d.stop()
}

func (d *Daemon) notifyWhenReady(readyChan chan interface{}) {
	defer d.wg.Done()

	select {
	case <-readyChan:
		atomic.StoreInt32(&d.ready, 1)
		d.stateNotifier <- DaemonReady
	case <-d.quitChan:
	}
}
