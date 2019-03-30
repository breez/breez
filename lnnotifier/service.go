package lnnotifier

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

// SubscribeEvents subscribe to various application events
func (n *LNNodeNotifier) SubscribeEvents() (*subscribe.Client, error) {
	return n.ntfnServer.Subscribe()
}

func (n *LNNodeNotifier) Start() error {
	n.wg.Add(1)
	go n.watchDeamonState()
	return nil
}

func (n *LNNodeNotifier) Stop() error {
	n.wg.Wait()
	return nil
}

func (n *LNNodeNotifier) OnResume() {
	n.ntfnServer.SendUpdate(ResumeEvent{})
}

func (n *LNNodeNotifier) watchDeamonState() {
	for {
		select {
		case <-n.lnDaemon.ReadyChan():
			n.startSubscriptions()
		case <-n.lnDaemon.QuitChan():
			n.ntfnServer.SendUpdate(DaemonDownEvent{})
			return
		}
	}
}

func (n *LNNodeNotifier) startSubscriptions() error {
	info, chainErr := n.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if chainErr != nil {
		n.log.Warnf("Failed get chain info", chainErr)
		return chainErr
	}
	if err := n.ntfnServer.SendUpdate(DaemonReadyEvent{IdentityPubkey: info.IdentityPubkey}); err != nil {
		return err
	}
	n.wg.Add(3)
	go n.subscribePeers()
	go n.subscribeTransactions()
	go n.subscribeInvoices()
	return nil
}

func (n *LNNodeNotifier) subscribePeers() error {
	defer n.wg.Done()
	subscription, err := n.lightningClient.SubscribePeers(context.Background(), &lnrpc.PeerSubscription{})
	if err != nil {
		n.log.Errorf("Failed to subscribe peers %v", err)
		return err
	}
	for {
		notification, err := subscription.Recv()
		if err == io.EOF {
			return err
		}
		if err != nil {
			n.log.Errorf("subscribe peers Failed to get notification %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
			continue
		}

		n.log.Infof("Peer event recieved for %v, connected = %v", notification.PubKey, notification.Connected)
		n.ntfnServer.SendUpdate(PeerConnectionEvent{notification})
	}
}

func (n *LNNodeNotifier) subscribeTransactions() {
	defer n.wg.Done()
	stream, err := n.lightningClient.SubscribeTransactions(context.Background(), &lnrpc.GetTransactionsRequest{})
	if err != nil {
		n.log.Criticalf("Failed to call SubscribeTransactions %v, %v", stream, err)
	}
	n.log.Infof("Wallet transactions subscription created")
	for {
		notification, err := stream.Recv()
		n.log.Infof("subscribeTransactions received new transaction")
		if err == io.EOF {
			n.log.Errorf("Failed to call SubscribeTransactions %v, %v", stream, err)
			return
		}
		if err != nil {
			n.log.Errorf("Failed to receive a transaction : %v", err)
			// in case of unexpected error, we will wait a bit so we won't get
			// into infinite loop.
			time.Sleep(2 * time.Second)
		}
		n.log.Infof("watchOnChainState sending account change notification")
		n.ntfnServer.SendUpdate(TransactionEvent{notification})
	}
}

func (n *LNNodeNotifier) subscribeInvoices() {
	defer n.wg.Done()

	stream, err := n.lightningClient.SubscribeInvoices(context.Background(), &lnrpc.InvoiceSubscription{})
	if err != nil {
		n.log.Criticalf("Failed to call SubscribeInvoices %v, %v", stream, err)
	}

	for {
		invoice, err := stream.Recv()
		n.log.Infof("watchPayments - Invoice received by subscription")
		if err != nil {
			n.log.Criticalf("Failed to receive an invoice : %v", err)
			return
		}
		n.ntfnServer.SendUpdate(&InvoiceEvent{invoice})
	}
}

func (n *LNNodeNotifier) syncToChain() error {
	defer n.wg.Done()
	for {
		chainInfo, chainErr := n.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if chainErr != nil {
			n.log.Warnf("Failed get chain info", chainErr)
			return chainErr
		}

		n.log.Infof("Sync to chain interval Synced=%v BlockHeight=%v", chainInfo.SyncedToChain, chainInfo.BlockHeight)
		if chainInfo.SyncedToChain {
			n.log.Infof("Synchronized to chain finshed BlockHeight=%v", chainInfo.BlockHeight)
			break
		}
		time.Sleep(time.Second * 3)
	}
	n.ntfnServer.SendUpdate(&ChainSyncedEvent{})
	return nil
}
