package breez

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
)

var (
	connectedToRoutingNode int32
	waitConnectTimeout     = time.Second * 20
	nodeOnlineNotifier     onlineNotifier
)

type onlineNotifier struct {
	sync.Mutex
	ntfnChan chan struct{}
}

func (n *onlineNotifier) setOffline() {
	n.Lock()
	defer n.Unlock()
	n.ntfnChan = make(chan struct{})
}

func (n *onlineNotifier) setOnline() {
	n.Lock()
	ntfnChan := n.ntfnChan
	n.Unlock()
	if ntfnChan != nil {
		close(ntfnChan)
	}
}

func (n *onlineNotifier) notifyWhenOnline() <-chan struct{} {
	return n.ntfnChan
}

/*
ConnectAccount force connect to the routing node.
This is meant to be used from the mobile platform when the network and device state allows it.
*/
func ConnectAccount() error {
	return connectRoutingNode()
}

/*
IsConnectedToRoutingNode returns the connection status to the routing node
*/
func IsConnectedToRoutingNode() bool {
	return isConnectedToRoutingNode()
}

func isConnectedToRoutingNode() bool {
	return atomic.LoadInt32(&connectedToRoutingNode) == 1
}

func watchRoutingNodeConnection() error {
	log.Infof("watchRoutingNodeConnection started")
	nodeOnlineNotifier.setOffline()
	subscription, err := lightningClient.SubscribePeers(context.Background(), &lnrpc.PeerSubscription{})
	if err != nil {
		log.Errorf("Failed to subscribe peers %v", err)
		return err
	}
	for {
		notification, err := subscription.Recv()
		if err == io.EOF {
			return err
		}
		if err != nil {
			log.Errorf("subscribe peers Failed to get notification %v", err)
			continue
		}

		log.Infof("Peer event recieved for %v, connected = %v", notification.PubKey, notification.Connected)
		if notification.PubKey == cfg.RoutingNodePubKey {
			onRoutingNodeConnectionChanged(notification.Connected)
		}
	}
}

func onRoutingNodeConnectionChanged(connected bool) {
	var connectedFlag int32
	if connected {
		connectedFlag = 1
	}
	atomic.StoreInt32(&connectedToRoutingNode, connectedFlag)
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_ROUTING_NODE_CONNECTION_CHANGED}

	// BREEZ-377: When there is no channel request one from Breez
	if connected {
		nodeOnlineNotifier.setOnline()
		accData, _ := calculateAccount()
		go updateNodeChannelPolicy(accData.Id)
		ensureRoutingChannelOpened()
		ensureSafeToRunNode()
	} else {
		nodeOnlineNotifier.setOffline()
	}
}

func connectRoutingNode() error {
	log.Infof("Connecting to routing node host: %v, pubKey: %v", cfg.RoutingNodeHost, cfg.RoutingNodePubKey)
	_, err := lightningClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: cfg.RoutingNodePubKey,
			Host:   cfg.RoutingNodeHost,
		},
		Perm: true,
	})
	return err
}

func disconnectRoutingNode() error {
	log.Infof("Disconnecting from routing node host: %v, pubKey: %v", cfg.RoutingNodeHost, cfg.RoutingNodePubKey)
	_, err := lightningClient.DisconnectPeer(context.Background(), &lnrpc.DisconnectPeerRequest{
		PubKey: cfg.RoutingNodePubKey,
	})
	return err
}

func waitRoutingNodeConnected() error {
	select {
	case <-nodeOnlineNotifier.notifyWhenOnline():
		return nil
	case <-time.After(waitConnectTimeout):
		return fmt.Errorf("Timeout has exceeded while trying to process your request.")
	}
}
