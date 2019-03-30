package breez

//protoc -I data data/messages.proto --go_out=plugins=grpc:data

import (
	"context"
	"errors"
	"path"
	"sync/atomic"

	"github.com/breez/breez/data"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lnnode"
	"github.com/breez/lightninglib/lnrpc"
)

//Service is the interface to be implemeted by all breez services
type Service interface {
	Start() error
	Stop() error
}

/*
Start is responsible for starting the lightning client and some go routines to track and notify for account changes
*/
func (a *App) Start() error {
	if atomic.SwapInt32(&a.started, 1) == 1 {
		return errors.New("Breez already started")
	}

	a.quitChan = make(chan struct{})

	if err := doubleratchet.Start(path.Join(a.cfg.WorkingDir, "sessions_encryption.db")); err != nil {
		return err
	}

	services := []Service{
		a.lnDaemon,
		a.servicesClient,
		a.swapService,
		a.accountService,
		a.backupManager,
	}

	for i, s := range services {
		if err := s.Start(); err != nil {
			return err
		}
	}

	if !a.ensureSafeToRunNode() {
		return errors.New("not safe to run a restored node")
	}

	a.wg.Add(2)
	go a.watchBackupEvents()
	go a.watchDaemonEvents()

	return nil
}

/*
Stop is responsible for stopping the ligtning daemon.
*/
func (a *App) Stop() error {
	if atomic.SwapInt32(&a.stopped, 1) == 1 {
		return errors.New("App already stopped")
	}
	close(a.quitChan)

	a.backupManager.Stop()
	doubleratchet.Stop()
	a.swapService.Stop()
	a.accountService.Stop()
	a.servicesClient.Stop()
	a.lnDaemon.Stop()
	a.breezDB.CloseDB()

	a.wg.Wait()
	return nil
}

/*
DaemonReady return the status of the lightningLib daemon
*/
func (a *App) DaemonReady() bool {
	return atomic.LoadInt32(&a.isReady) == 1
}

// NotificationChan returns a channel that receives notification events
func (a *App) NotificationChan() chan data.NotificationEvent {
	return a.notificationsChan
}

/*
OnResume recalculate things we might missed when we were idle.
*/
func (a *App) OnResume() {
	if atomic.LoadInt32(&a.isReady) == 1 {
		a.accountService.OnResume()
	}
}

func (a *App) RestartDaemon() error {
	return a.lnDaemon.Start()
}

func (a *App) startAppServices() error {
	if err := a.accountService.Start(); err != nil {
		return err
	}
	return nil
}

func (a *App) watchDaemonEvents() error {
	defer a.wg.Done()

	client, err := a.lnDaemon.SubscribeEvents()
	defer client.Cancel()

	if err != nil {
		return err
	}
	for {
		select {
		case u := <-client.Updates():
			switch notification := u.(type) {
			case lnnode.DaemonReadyEvent:
				atomic.StoreInt32(&a.isReady, 1)
				a.notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_READY}
			case lnnode.DaemonDownEvent:
				atomic.StoreInt32(&a.isReady, 0)
				a.notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_LIGHTNING_SERVICE_DOWN}
			}
		case <-client.Quit():
			return nil
		}
	}
}

func (a *App) ensureSafeToRunNode() bool {
	info, err := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		a.log.Errorf("ensureSafeToRunNode failed, continue anyway %v", err)
		return true
	}
	safe, err := a.backupManager.IsSafeToRunNode(info.IdentityPubkey)
	if err != nil {
		a.log.Errorf("ensureSafeToRunNode failed, continue anyway %v", err)
		return true
	}
	if !safe {
		a.log.Errorf("ensureSafeToRunNode detected remote restore! stopping breez since it is not safe to run")
		a.notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_NODE_CONFLICT}
		a.lnDaemon.Stop()
		return false
	}
	a.log.Infof("ensureSafeToRunNode succeed, safe to run node: %v", info.IdentityPubkey)
	return true
}

func (a *App) watchBackupEvents() {
	stream, err := a.lightningClient.SubscribeBackupEvents(context.Background(), &lnrpc.BackupEventSubscription{})
	if err != nil {
		a.log.Criticalf("Failed to call SubscribeBackupEvents %v, %v", stream, err)
	}
	a.log.Infof("Backup events subscription created")
	for {
		_, err := stream.Recv()
		a.log.Infof("watchBackupEvents received new event")
		if err != nil {
			a.log.Errorf("watchBackupEvents failed to receive a new event: %v, %v", stream, err)
			return
		}
		a.RequestBackup()
	}
}
