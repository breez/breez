package lnnode

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/lightningnetwork/lnd"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/breezbackuprpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnrpc/submarineswaprpc"
	"github.com/lightningnetwork/lnd/signal"
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

// HasActiveChannel returns true if the node has at least one active channel.
func (d *Daemon) HasActiveChannel() bool {
	lnclient := d.APIClient()
	if lnclient == nil {
		return false
	}
	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		ActiveOnly: true,
	})
	if err != nil {
		d.log.Errorf("Error in HasActiveChannel() > ListChannels(): %v", err)
		return false
	}
	return len(channels.Channels) > 0
}

// NodePubkey returns the identity public key of the lightning node.
func (d *Daemon) NodePubkey() string {
	d.Lock()
	defer d.Unlock()
	return d.nodePubkey
}

// Stop is used to stop the lightning network daemon.
func (d *Daemon) Stop() error {
	if atomic.SwapInt32(&d.stopped, 1) == 0 {
		d.stopDaemon()
		d.ntfnServer.Stop()
	}
	d.wg.Wait()
	d.log.Infof("Daemon shutdown successfully")
	return nil
}

// APIClient returns the interface to query the daemon.
func (d *Daemon) APIClient() lnrpc.LightningClient {
	d.Lock()
	defer d.Unlock()
	return d.lightningClient
}

func (d *Daemon) SubSwapClient() submarineswaprpc.SubmarineSwapperClient {
	d.Lock()
	defer d.Unlock()
	return d.subswapClient
}

func (d *Daemon) BreezBackupClient() breezbackuprpc.BreezBackuperClient {
	d.Lock()
	defer d.Unlock()
	return d.breezBackupClient
}

func (d *Daemon) RouterClient() routerrpc.RouterClient {
	d.Lock()
	defer d.Unlock()
	return d.routerClient
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

	d.quitChan = make(chan struct{})
	readyChan := make(chan interface{})

	d.wg.Add(2)
	go d.notifyWhenReady(readyChan)
	d.daemonRunning = true

	// Run the daemon
	go func() {
		defer func() {
			defer d.wg.Done()
			go d.stopDaemon()
		}()

		chanDB, chanDBCleanUp, err := channeldbservice.Get(d.cfg.WorkingDir)
		if err != nil {
			d.log.Errorf("failed to create channeldbservice", err)
			return
		}

		chainSevice, cleanupFn, err := chainservice.Get(d.cfg.WorkingDir, d.breezDB)
		if err != nil {
			chanDBCleanUp()
			d.log.Errorf("failed to create chainservice", err)
			return
		}
		deps := &Dependencies{
			workingDir:   d.cfg.WorkingDir,
			chainService: chainSevice,
			readyChan:    readyChan,
			chanDB:       chanDB}
		params := []string{"lightning-libs",
			"--lnddir", deps.workingDir,
			"--bitcoin." + d.cfg.Network,
		}
		err = lnd.Main(lnd.ListenerCfg{}, params, deps)

		if err != nil {
			d.log.Errorf("Breez main function returned with error: %v", err)
		}

		chanDBCleanUp()
		cleanupFn()
	}()
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
	d.log.Infof("Daemon sent down event")
}

func (d *Daemon) notifyWhenReady(readyChan chan interface{}) {
	defer d.wg.Done()
	select {
	case <-readyChan:
		if err := d.startSubscriptions(); err != nil {
			d.log.Criticalf("Can't start daemon subscriptions, shutting down: %v", err)
			go d.stopDaemon()
		}
	case <-d.quitChan:
	}
}
