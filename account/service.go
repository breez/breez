package account

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/breez/breez/lnnotifier"
)

const (
	syncToChainDefaultPollingInterval = 3 * time.Second
)

// Start starts the account service
func (a *Service) Start() error {
	if atomic.SwapInt32(&a.started, 1) == 1 {
		return errors.New("Account service has already started")
	}

	a.wg.Add(3)
	go a.watchDaemonEvents()
	go a.trackOpenedChannel()
	go a.watchPayments()
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

func (a *Service) daemonRPCReady() bool {
	return atomic.LoadInt32(&a.daemonReady) == 1
}

func (a *Service) watchDaemonEvents() error {
	defer a.wg.Done()

	client, err := a.lnNotifier.SubscribeEvents()
	defer client.Cancel()

	if err != nil {
		return err
	}
	for {
		select {
		case u := <-client.Updates():
			switch notification := u.(type) {
			case lnnotifier.DaemonReadyEvent:
				atomic.StoreInt32(&a.daemonReady, 1)
			case lnnotifier.PeerConnectionEvent:
				if notification.PubKey == a.cfg.RoutingNodePubKey {
					a.onRoutingNodeConnection(notification.Connected)
				}
			case lnnotifier.TransactionEvent:
				a.onAccountChanged()
				go a.ensureRoutingChannelOpened()
			case lnnotifier.ChainSyncedEvent:
				a.connectOnStartup()
			case lnnotifier.ResumeEvent:
				a.calculateAccountAndNotify()
			}
		case <-client.Quit():
			return nil
		}
	}
}
