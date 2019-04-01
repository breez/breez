package swapfunds

import (
	"errors"
	"sync/atomic"

	"github.com/breez/breez/lnnode"
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
	close(s.quitChan)
	s.wg.Wait()
	s.log.Infof("SwapService shutdown succesfully")
	return nil
}

func (s *Service) watchDaemonEvents() (err error) {
	defer s.wg.Done()

	client, err := s.daemonAPI.SubscribeEvents()
	if err != nil {
		s.log.Errorf("watchDaemonEvents exit with error %v", err)
		return err
	}
	defer client.Cancel()

	var routingNodeConnected bool
	for {
		select {
		case u := <-client.Updates():
			switch notification := u.(type) {
			case lnnode.DaemonReadyEvent:
				s.onDaemonReady()
			case lnnode.PeerConnectionEvent:
				if notification.PubKey == s.cfg.RoutingNodePubKey {
					routingNodeConnected = notification.Connected
					if routingNodeConnected {
						s.settlePendingTransfers()
					}
				}
			case lnnode.TransactionEvent:
				s.onTransaction()
			case lnnode.DaemonDownEvent:
				return nil
			case lnnode.ResumeEvent:
				if routingNodeConnected {
					s.settlePendingTransfers()
				}
			}
		case <-s.quitChan:
			return nil
		}
	}
}
