package swapfunds

import (
	"errors"
	"sync/atomic"

	"github.com/breez/breez/lnnotifier"
)

func (s *Service) Start() error {
	if atomic.SwapInt32(&s.started, 1) == 1 {
		return errors.New("Service already started")
	}

	s.wg.Add(1)
	go s.watchDaemonEvents()

	return nil
}

func (s *Service) Stop() error {
	if atomic.SwapInt32(&s.stopped, 1) == 1 {
		return errors.New("Service already started")
	}
	s.daemonEventsClient.Cancel()
	close(s.quitChan)
	s.wg.Wait()
	return nil
}

func (s *Service) watchDaemonEvents() (err error) {
	s.daemonEventsClient, err = s.lnNotifier.SubscribeEvents()

	if err != nil {
		s.log.Errorf("watchDaemonEvents exit with error %v", err)
		return err
	}

	var routingNodeConnected bool
	for {
		select {
		case u := <-s.daemonEventsClient.Updates():
			switch notification := u.(type) {
			case lnnotifier.DaemonReadyEvent:
				s.onDaemonReady()
			case lnnotifier.PeerConnectionEvent:
				if notification.PubKey == s.cfg.RoutingNodePubKey {
					routingNodeConnected = notification.Connected
					if routingNodeConnected {
						s.settlePendingTransfers()
					}
				}
			case lnnotifier.TransactionEvent:
				s.onTransaction()
			case lnnotifier.DaemonDownEvent:
				return nil
			case lnnotifier.ResumeEvent:
				if routingNodeConnected {
					s.settlePendingTransfers()
				}
			}
		case <-s.daemonEventsClient.Quit():
			return nil
		}
	}
}
