package lnnode

import (
	"errors"
	"fmt"
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

	if err := d.ntfnServer.Start(); err != nil {
		return err
	}

	if err := d.startDaemon(); err != nil {
		return fmt.Errorf("Failed to start daemon: %v", err)
	}

	return nil
}

// Stop is used to stop the lightning network daemon.
func (d *Daemon) Stop() error {
	if atomic.SwapInt32(&d.stopped, 1) == 0 {
		d.stopDaemon()
		d.ntfnServer.Stop()
	}
	d.wg.Wait()
	return nil
}

// RestartDaemon is used to restart a daemon that from some reason failed to start
// or was started and failed at some later point.
func (d *Daemon) RestartDaemon() error {
	if atomic.LoadInt32(&d.started) == 0 {
		return errors.New("Daemon must be started before attempt to restart")
	}
	return d.startDaemon()
}

func (d *Daemon) startDaemon() error {
	d.Lock()
	defer d.Unlock()
	if d.daemonRunning {
		return errors.New("Daemon already running")
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
	d.quitChan = make(chan struct{})
	deps := &Dependencies{
		workingDir:   d.cfg.WorkingDir,
		chainService: chainSevice,
		readyChan:    readyChan,
		chanDB:       chanDB}

	d.wg.Add(2)
	go d.notifyWhenReady(readyChan)
	go func() {
		defer d.wg.Done()

		err := daemon.LndMain(
			[]string{"lightning-libs", "--lnddir",
				deps.workingDir, "--bitcoin." + d.cfg.Network,
			},
			deps,
		)

		if err != nil {
			d.log.Errorf("Breez main function returned with error: %v", err)
		}

		chanDBCleanUp()
		cleanupFn()
		go d.stopDaemon()
	}()
	d.daemonRunning = true
	return nil
}

func (d *Daemon) stopDaemon() {
	d.Lock()
	defer d.Unlock()
	if !d.daemonRunning {
		return
	}
	alive := signal.Alive()
	d.log.Infof("Daemon.stop() called, stopping breez daemon alive=%v", alive)
	if alive {
		signal.RequestShutdown()
	}
	close(d.quitChan)

	d.wg.Wait()
	d.daemonRunning = false
	d.ntfnServer.SendUpdate(DaemonDownEvent{})
}

func (d *Daemon) runLightningDaemon(cfg *config.Config, deps *Dependencies) {

	err := daemon.LndMain(
		[]string{"lightning-libs", "--lnddir",
			deps.workingDir, "--bitcoin." + cfg.Network,
		},
		deps,
	)

	if err != nil {
		d.log.Errorf("Breez failed with error: %v", err)
	}
}

func (d *Daemon) notifyWhenReady(readyChan chan interface{}) {
	defer d.wg.Done()
	select {
	case <-readyChan:
		if err := d.startSubscriptions(); err != nil {
			d.log.Criticalf("Can't start daemon subscriptions, shutting down: %v", err)
			d.stopDaemon()
		}
	case <-d.quitChan:
	}
}
