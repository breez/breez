package breez

//protoc -I data data/messages.proto --go_out=plugins=grpc:data

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lnnode"
	"github.com/coreos/bbolt"
	"github.com/lightninglabs/neutrino/filterdb"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
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

	a.log.Info("app.start before bootstrap")
	if err := chainservice.Bootstrap(a.cfg.WorkingDir); err != nil {
		a.log.Info("app.start bootstrap error %v", err)
		return err
	}

	services := []Service{
		a.lnDaemon,
		a.ServicesClient,
		a.SwapService,
		a.AccountService,
		a.BackupManager,
	}

	for _, s := range services {
		if err := s.Start(); err != nil {
			return err
		}
	}

	a.wg.Add(1)
	go a.watchDaemonEvents()

	return nil
}

/*
Stop is responsible for stopping the ligtning daemon.
*/
func (a *App) Stop() error {
	if atomic.SwapInt32(&a.stopped, 1) == 1 {
		return nil
	}

	close(a.quitChan)
	a.BackupManager.Stop()
	a.SwapService.Stop()
	a.AccountService.Stop()
	a.ServicesClient.Stop()
	a.lnDaemon.Stop()
	doubleratchet.Stop()
	a.releaseBreezDB()

	a.wg.Wait()
	a.log.Infof("BreezApp shutdown successfully")
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
		a.AccountService.OnResume()
		a.SwapService.SettlePendingTransfers()
	}
}

func (a *App) RestartDaemon() error {
	return a.lnDaemon.RestartDaemon()
}

// Restore is the breez API for restoring a specific nodeID using the configured
// backup backend provider.
func (a *App) Restore(nodeID string, key []byte) error {
	a.log.Infof("Restore nodeID = %v", nodeID)
	if err := a.releaseBreezDB(); err != nil {
		return err
	}
	defer func() {
		a.breezDB, a.releaseBreezDB, _ = db.Get(a.cfg.WorkingDir)
	}()
	_, err := a.BackupManager.Restore(nodeID, key)
	return err
}

/*
GetLogPath returns the log file path.
*/
func (a *App) GetLogPath() string {
	return a.cfg.WorkingDir + "/logs/bitcoin/" + a.cfg.Network + "/lnd.log"
}

// GetWorkingDir returns the working dir.
func (a *App) GetWorkingDir() string {
	return a.cfg.WorkingDir
}

func (a *App) startAppServices() error {
	if err := a.AccountService.Start(); err != nil {
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
			switch u.(type) {
			case lnnode.DaemonReadyEvent:
				atomic.StoreInt32(&a.isReady, 1)
				go a.ensureSafeToRunNode()
				go a.notify(data.NotificationEvent{Type: data.NotificationEvent_READY})
			case lnnode.DaemonDownEvent:
				atomic.StoreInt32(&a.isReady, 0)
				go a.notify(data.NotificationEvent{Type: data.NotificationEvent_LIGHTNING_SERVICE_DOWN})
			case lnnode.BackupNeededEvent:
				a.BackupManager.RequestCommitmentChangedBackup()
			case lnnode.ChannelEvent:
				if a.lnDaemon.HasActiveChannel() {
					go a.ensureSafeToRunNode()
				}
			case lnnode.ChainSyncedEvent:
				chainService, cleanupFn, err := chainservice.Get(a.cfg.WorkingDir, a.breezDB)
				if err != nil {
					a.log.Errorf("failed to get chain service on sync event")
					break
				}
				err = chainService.FilterDB.PurgeFilters(filterdb.RegularFilter)
				var app *App
				a.log.Errorf("purge compact filters finished error = %v", err)
				app.CheckVersion()
				cleanupFn()
			}
		case <-client.Quit():
			return nil
		}
	}
}

func (a *App) ensureSafeToRunNode() bool {
	lnclient := a.lnDaemon.APIClient()
	info, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		a.log.Errorf("ensureSafeToRunNode failed, continue anyway %v", err)
		return true
	}
	safe, err := a.BackupManager.IsSafeToRunNode(info.IdentityPubkey)
	if err != nil {
		a.log.Errorf("ensureSafeToRunNode failed, continue anyway %v", err)
		return true
	}
	if !safe {
		a.log.Errorf("ensureSafeToRunNode detected remote restore! stopping breez since it is not safe to run")
		go a.notify(data.NotificationEvent{Type: data.NotificationEvent_BACKUP_NODE_CONFLICT})
		a.lnDaemon.Stop()
		return false
	}
	a.log.Infof("ensureSafeToRunNode succeed, safe to run node: %v", info.IdentityPubkey)
	return true
}

func (a *App) onServiceEvent(event data.NotificationEvent) {
	a.notify(event)
	if event.Type == data.NotificationEvent_FUND_ADDRESS_CREATED ||
		event.Type == data.NotificationEvent_LSP_CHANNEL_OPENED {
		a.BackupManager.RequestBackup()
	}
}

func (a *App) notify(event data.NotificationEvent) {
	a.notificationsChan <- event
}

func (a *App) SetPeers(peers []string) error {
	return a.breezDB.SetPeers(peers)
}

func (a *App) GetPeers() (peers []string, isDefault bool, err error) {
	return a.breezDB.GetPeers(a.cfg.JobCfg.ConnectedPeers)
}

func (a *App) SetTxSpentURL(txSpentURL string) error {
	return a.breezDB.SetTxSpentURL(txSpentURL)
}

func (a *App) GetTxSpentURL() (txSpentURL string, isDefault bool, err error) {
	return a.breezDB.GetTxSpentURL(a.cfg.TxSpentURL)
}

func (a *App) ClosedChannels() (int, error) {
	return a.lnDaemon.ClosedChannels()
}

func (a *App) LastSyncedHeaderTimestamp() (int64, error) {
	return a.breezDB.FetchLastSyncedHeaderTimestamp()
}

func (a *App) DeleteNonTLVNodesFromGraph() error {
	chanDB, chanDBCleanUp, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		a.log.Errorf("channeldbservice.Get(%v): %v", a.cfg.WorkingDir, err)
		return fmt.Errorf("channeldbservice.Get(%v): %w", a.cfg.WorkingDir, err)
	}
	defer chanDBCleanUp()
	graph := chanDB.ChannelGraph()

	cids := make(map[uint64]struct{})
	nodes := 0
	err = chanDB.DB.View(func(tx *bbolt.Tx) error {
		return graph.ForEachNode(tx, func(tx *bbolt.Tx, lightningNode *channeldb.LightningNode) error {
			if lightningNode.Features.HasFeature(lnwire.TLVOnionPayloadOptional) {
				return nil
			}
			nodes++
			return lightningNode.ForEachChannel(tx, func(tx *bbolt.Tx,
				channelEdgeInfo *channeldb.ChannelEdgeInfo,
				_ *channeldb.ChannelEdgePolicy,
				_ *channeldb.ChannelEdgePolicy) error {
				cids[channelEdgeInfo.ChannelID] = struct{}{}
				return nil
			})
		})
	})
	if err != nil {
		a.log.Errorf("DeleteNodeFromGraph->ForEachNodeChannel error = %v", err)
		return fmt.Errorf("ForEachNodeChannel: %w", err)
	}
	a.log.Infof("About to delete %v channels from %v non tlv nodes.", len(cids), nodes)
	var chanIDs []uint64
	for cid := range cids {
		chanIDs = append(chanIDs, cid)
	}
	err = graph.DeleteChannelEdges(chanIDs...)
	if err != nil {
		a.log.Errorf("DeleteNodeFromGraph->DeleteChannelEdges error = %v", err)
		return fmt.Errorf("DeleteChannelEdges: %w", err)
	}

	err = graph.PruneGraphNodes()
	if err != nil {
		a.log.Errorf("DeleteNodeFromGraph->PruneGraphNodes error = %v", err)
		return fmt.Errorf("PruneGraphNodes(): %w", err)
	}
	a.log.Infof("Deleted %v channels from %v non tlv nodes.", len(chanIDs), nodes)
	return nil
}
