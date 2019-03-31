package account

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/breez/breez/lnnode"
)

const (
	syncToChainDefaultPollingInterval = 3 * time.Second
)

// Start starts the account service
func (a *Service) Start() error {
	if atomic.SwapInt32(&a.started, 1) == 1 {
		return errors.New("Account service has already started")
	}

	a.wg.Add(1)
	go a.watchDaemonEvents()
	return nil
}

// Stop stops the account service
func (a *Service) Stop() error {
	if atomic.SwapInt32(&a.started, 1) == 1 {
		return nil
	}
	close(a.quitChan)
	a.wg.Wait()
	return nil
}

func (a *Service) Connect() error {
	return a.connectRoutingNode()
}

func (a *Service) OnResume() {
	a.calculateAccountAndNotify()
}

func (a *Service) daemonRPCReady() bool {
	return atomic.LoadInt32(&a.daemonReady) == 1
}

func (a *Service) watchDaemonEvents() error {
	defer a.wg.Done()

	client, err := a.daemon.SubscribeEvents()
	defer client.Cancel()

	if err != nil {
		return err
	}
	for {
		select {
		case u := <-client.Updates():
			switch notification := u.(type) {
			case lnnode.DaemonReadyEvent:
				atomic.StoreInt32(&a.daemonReady, 1)
				a.wg.Add(2)
				go a.trackOpenedChannel()
				go a.watchPayments()
			case lnnode.PeerConnectionEvent:
				if notification.PubKey == a.cfg.RoutingNodePubKey {
					a.onRoutingNodeConnection(notification.Connected)
				}
			case lnnode.TransactionEvent:
				a.onAccountChanged()
				go a.ensureRoutingChannelOpened()
			case lnnode.ChainSyncedEvent:
				a.connectOnStartup()
			}
		case <-client.Quit():
			return nil
		}
	}
}
