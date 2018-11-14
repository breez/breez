package breez

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
)

var (
	connectedToRoutingNode int32
)

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
	return atomic.LoadInt32(&connectedToRoutingNode) == 1
}

func watchRoutingNodeConnection() error {
	log.Infof("watchRoutingNodeConnection started")
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
		accData, _ := calculateAccount()
		if accData.Status == data.Account_WAITING_DEPOSIT {
			createChannelGroup.Do("createChannel", func() (interface{}, error) {
				createChannel(accData.Id)
				return nil, nil
			})
			onAccountChanged()
		} else if accData.Status == data.Account_ACTIVE {
			updateNodeChannelPolicy(accData.Id)
		}

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
