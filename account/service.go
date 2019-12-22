package account

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/breez/breez/lnnode"
	"github.com/lightningnetwork/lnd/lnrpc"
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
	if atomic.SwapInt32(&a.stopped, 1) == 1 {
		return nil
	}
	close(a.quitChan)
	a.wg.Wait()
	a.log.Infof("AccountService shutdown succesfully")
	return nil
}

/*func (a *Service) Connect() error {
	return a.connectRoutingNode()
}*/

func (a *Service) OnResume() {
	a.calculateAccountAndNotify()
}

func (a *Service) daemonRPCReady() bool {
	return atomic.LoadInt32(&a.daemonReady) == 1
}

func (a *Service) watchDaemonEvents() (err error) {
	defer a.wg.Done()

	a.daemonSubscription, err = a.daemonAPI.SubscribeEvents()
	defer a.daemonSubscription.Cancel()

	if err != nil {
		return err
	}
	for {
		select {
		case u := <-a.daemonSubscription.Updates():
			switch update := u.(type) {
			case lnnode.DaemonReadyEvent:
				atomic.StoreInt32(&a.daemonReady, 1)
				a.wg.Add(1)
				go a.watchPayments()
				a.onAccountChanged()
			case lnnode.TransactionEvent:
				a.syncClosedChannels()
				a.onAccountChanged()
			case lnnode.ChannelEvent:
				a.connectedNotifier.setActive(a.daemonAPI.HasActiveChannel())
				if update.Type == lnrpc.ChannelEventUpdate_CLOSED_CHANNEL {
					a.syncClosedChannels()
				}
				a.calculateAccountAndNotify()
			case lnnode.DaemonDownEvent:
				atomic.StoreInt32(&a.daemonReady, 0)
			}
		case <-a.quitChan:
			a.log.Infof("Cancelling daemon events subscription")
			return nil
		}
	}
}
