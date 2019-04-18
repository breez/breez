package lnnode

import (
	"context"
	"io"
	"time"

	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/subscribe"
)

// DaemonReadyEvent is sent when the daemon is ready for RPC requests
type DaemonReadyEvent struct {
	IdentityPubkey string
}

// DaemonDownEvent is sent when the daemon stops
type DaemonDownEvent struct{}

// PeerConnectionEvent is sent whenever a peer is connected/disconnected.
type PeerConnectionEvent struct {
	*lnrpc.PeerNotification
}

// TransactionEvent is sent when a new transaction is received.
type TransactionEvent struct {
	*lnrpc.Transaction
}

// InvoiceEvent is sent when a new invoice is created/settled.
type InvoiceEvent struct {
	*lnrpc.Invoice
}

// ChainSyncedEvent is sent when the chain gets into synced state.
type ChainSyncedEvent struct{}

// ResumeEvent is sent when the app resumes.
type ResumeEvent struct{}

// BackupNeededEvent is sent whwen the node signals backup is needed.
type BackupNeededEvent struct{}

// SubscribeEvents subscribe to various application events
func (d *Daemon) SubscribeEvents() (*subscribe.Client, error) {
	return d.ntfnServer.Subscribe()
}

func (d *Daemon) startSubscriptions() error {
	var err error
	d.lightningClient, err = newLightningClient(d.cfg)
	if err != nil {
		return err
	}

	info, chainErr := d.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if chainErr != nil {
		d.log.Warnf("Failed get chain info", chainErr)
		return chainErr
	}

	ctx, cancel := context.WithCancel(context.Background())

	d.wg.Add(5)
	go d.subscribePeers(ctx)
	go d.subscribeTransactions(ctx)
	go d.subscribeInvoices(ctx)
	go d.watchBackupEvents(ctx)
	go d.syncToChain(ctx)

	// cancel subscriptions on quit
	go func() {
		<-d.quitChan
		cancel()
	}()

	if err := d.ntfnServer.SendUpdate(DaemonReadyEvent{IdentityPubkey: info.IdentityPubkey}); err != nil {
		return err
	}
	d.log.Infof("Daemon ready! subscriptions started")
	return nil
}

func (d *Daemon) subscribePeers(ctx context.Context) error {
	defer d.wg.Done()

	subscription, err := d.lightningClient.SubscribePeers(ctx, &lnrpc.PeerSubscription{})
	if err != nil {
		d.log.Errorf("Failed to subscribe peers %v", err)
		return err
	}

	d.log.Infof("Peers subscription created")
	for {
		notification, err := subscription.Recv()
		if err == io.EOF {
			d.log.Errorf("subscribePeers cancelled, shutting down")
			return err
		}
		d.log.Infof("Peer event recieved for %v, connected = %v", notification.PubKey, notification.Connected)
		if err != nil {
			d.log.Errorf("subscribe peers Failed to get notification %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
			continue
		}
		d.ntfnServer.SendUpdate(PeerConnectionEvent{notification})
	}
}

func (d *Daemon) subscribeTransactions(ctx context.Context) error {
	defer d.wg.Done()

	stream, err := d.lightningClient.SubscribeTransactions(ctx, &lnrpc.GetTransactionsRequest{})
	if err != nil {
		d.log.Criticalf("Failed to call SubscribeTransactions %v, %v", stream, err)
	}

	d.log.Infof("Wallet transactions subscription created")
	for {
		notification, err := stream.Recv()
		if err == io.EOF {
			d.log.Errorf("subscribeTransactions cancelled, shutting down")
			return err
		}
		d.log.Infof("subscribeTransactions received new transaction")
		if err != nil {
			d.log.Errorf("Failed to receive a transaction : %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
		}
		d.log.Infof("watchOnChainState sending account change notification")
		d.ntfnServer.SendUpdate(TransactionEvent{notification})
	}
}

func (d *Daemon) subscribeInvoices(ctx context.Context) error {
	defer d.wg.Done()

	stream, err := d.lightningClient.SubscribeInvoices(ctx, &lnrpc.InvoiceSubscription{})
	if err != nil {
		d.log.Criticalf("Failed to call SubscribeInvoices %v, %v", stream, err)
		return err
	}

	d.log.Infof("Invoices subscription created")
	for {
		invoice, err := stream.Recv()
		if err == io.EOF {
			d.log.Errorf("subscribeInvoices cancelled, shutting down")
			return err
		}
		if err != nil {
			d.log.Criticalf("Failed to receive an invoice : %v", err)
			return err
		}
		d.log.Infof("watchPayments - Invoice received by subscription")
		d.ntfnServer.SendUpdate(&InvoiceEvent{invoice})
	}
}

func (d *Daemon) watchBackupEvents(ctx context.Context) error {
	defer d.wg.Done()

	stream, err := d.lightningClient.SubscribeBackupEvents(ctx, &lnrpc.BackupEventSubscription{})
	if err != nil {
		d.log.Criticalf("Failed to call SubscribeBackupEvents %v, %v", stream, err)
	}

	d.log.Infof("Backup events subscription created")
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			d.log.Errorf("watchBackupEvents cancelled, shutting down")
			return err
		}
		d.log.Infof("watchBackupEvents received new event")
		if err != nil {
			d.log.Errorf("watchBackupEvents failed to receive a new event: %v, %v", stream, err)
			return err
		}
		d.ntfnServer.SendUpdate(&BackupNeededEvent{})
	}
}

func (d *Daemon) syncToChain(ctx context.Context) error {
	defer d.wg.Done()
	for {
		chainInfo, chainErr := d.lightningClient.GetInfo(ctx, &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			d.log.Warnf("Failed get chain info", chainErr)
			return chainErr
		}

		d.log.Infof("Sync to chain interval Synced=%v BlockHeight=%v", chainInfo.SyncedToChain, chainInfo.BlockHeight)
		if chainInfo.SyncedToChain {
			d.log.Infof("Synchronized to chain finshed BlockHeight=%v", chainInfo.BlockHeight)
			break
		}
		time.Sleep(time.Second * 3)
	}
	d.ntfnServer.SendUpdate(&ChainSyncedEvent{})
	return nil
}
