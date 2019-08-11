package account

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"

	breezservice "github.com/breez/breez/breez"
)

var (
	waitConnectTimeout = time.Second * 60
)

type channelActiveNotifier struct {
	sync.Mutex
	ntfnChan chan struct{}
	isActive bool
}

// newChannelActiveNotifier creates a new channelActiveNotifier
func newChannelActiveNotifier() *channelActiveNotifier {
	return &channelActiveNotifier{
		ntfnChan: make(chan struct{}),
	}
}

func (n *channelActiveNotifier) active() bool {
	n.Lock()
	defer n.Unlock()
	return n.isActive
}

func (n *channelActiveNotifier) setActive(active bool) {

	if n.active() == active {
		return
	}

	n.Lock()
	n.isActive = active

	if !active {
		n.ntfnChan = make(chan struct{})
		n.Unlock()
	} else {
		ntfnChan := n.ntfnChan
		n.Unlock()
		if ntfnChan != nil {
			close(ntfnChan)
		}
	}
}

func (n *channelActiveNotifier) notifyWhenActive() <-chan struct{} {
	n.Lock()
	defer n.Unlock()
	return n.ntfnChan
}

// IsConnectedToRoutingNode returns true if we are connected to the routing node.
//yas func (a *Service) IsConnectedToRoutingNode() bool {
//	return a.daemonAPI.ConnectedToRoutingNode()
//}

func (a *Service) SetLSP(id string) {
	a.LSPId = id
}

func (a *Service) GetLSP() string {
	return a.LSPId
}

// ConnectChannelsPeers connects to all peers associated with a non active channel.
func (a *Service) ConnectChannelsPeers() error {
	lnclient := a.daemonAPI.APIClient()
	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		InactiveOnly: true,
	})
	if err != nil {
		return err
	}

	for _, c := range channels.Channels {
		nodeInfo, err := lnclient.GetNodeInfo(context.Background(), &lnrpc.NodeInfoRequest{PubKey: c.RemotePubkey})
		if err != nil && len(nodeInfo.GetNode().Addresses) > 0 {
			address := nodeInfo.GetNode().Addresses[0]
			a.log.Infof("Connecting to peer %v with address= %v", c.RemotePubkey, address.Addr)
			lnclient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
				Addr: &lnrpc.LightningAddress{
					Pubkey: c.RemotePubkey,
					Host:   address.Addr,
				},
				Perm: true,
			})
		}
	}

	return nil
}

func (a *Service) routingNode(lspID string) (string, string) {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()
	r, err := c.LSPList(ctx, &breezservice.LSPListRequest{Pubkey: a.daemonAPI.NodePubkey()})
	if err != nil {
		a.log.Infof("LSPList returned an error: %v", err)
		return "", ""
	}
	l, ok := r.Lsps[lspID]
	if !ok {
		a.log.Infof("The LSP ID is not in the LSPList: %v", lspID)
		return "", ""
	}
	return l.Host, l.Host
}

func (a *Service) connectLSP() (string, error) {
	if a.LSPId == "" {
		return "", nil
	}
	pubkey, host := a.routingNode(a.LSPId)
	lnclient := a.daemonAPI.APIClient()
	a.log.Infof("Connecting to routing node host: %v, pubKey: %v", host, pubkey)
	_, err := lnclient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: pubkey,
			Host:   host,
		},
		Perm: true,
	})
	return pubkey, err
}

func (a *Service) waitChannelActive() error {
	if a.daemonAPI.HasActiveChannel() {
		return nil
	}
	select {
	case <-a.connectedNotifier.notifyWhenActive():
		return nil
	case <-time.After(waitConnectTimeout):
		return fmt.Errorf("Timeout has exceeded while trying to process your request.")
	}
}

func (a *Service) isConnected(pubkey string) bool {
	lnclient := a.daemonAPI.APIClient()
	peers, err := lnclient.ListPeers(context.Background(), &lnrpc.ListPeersRequest{})
	if err != nil {
		a.log.Errorf("isConnected got error in ListPeers: %v", err)
		return false
	}
	for _, p := range peers.Peers {
		if p.PubKey == pubkey {
			return true
		}
	}
	return false
}

/*
createChannel is responsible for creating a new channel
*/
func (a *Service) openLSPChannel(pubkey string) {
	a.log.Info("openLSPChannel started...")
	lnclient := a.daemonAPI.APIClient()
	createChannelGroup.Do("createChannel", func() (interface{}, error) {
		for {
			lnInfo, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
			if err == nil {
				if a.isConnected(pubkey) {
					a.log.Infof("Attempting to open a channel...")

					channelPoints, _, err := a.getOpenChannels()
					if err != nil {
						a.log.Errorf("openLSPChannel got error in getBreezOpenChannels %v", err)
					}

					if len(channelPoints) > 0 {
						a.log.Infof("openLSPChannel already has a channel with breez, doing nothing")
						return nil, nil
					}
					pendingChannels, err := a.getPendingChannelPoint()
					if err != nil {
						a.log.Errorf("openLSPChannel got error in getPendingBreezChannelPoint %v", err)
					}

					if len(pendingChannels) > 0 {
						a.log.Infof("openLSPChannel already has a pending channel with breez, doing nothing")
						a.onRoutingNodePendingChannel()
						return nil, nil
					}

					c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
					_, err = c.OpenLSPChannel(ctx, &breezservice.OpenLSPChannelRequest{LspId: a.LSPId, Pubkey: lnInfo.IdentityPubkey})
					cancel()
					if err == nil {
						a.onRoutingNodePendingChannel()
						return nil, nil
					}

					a.log.Errorf("Error in attempting to openChannel: %v", err)
				}
			}
			if err != nil {
				a.log.Errorf("Error in openChannel: %v", err)
			}
			time.Sleep(time.Second * 25)
		}
	})
}

func (a *Service) connectOnStartup() {
	channelPoints, _, err := a.getOpenChannels()
	if err != nil {
		a.log.Errorf("connectOnStartup: error in getOpenChannels", err)
		return
	}
	lnclient := a.daemonAPI.APIClient()
	pendingChannels, err := lnclient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		a.log.Errorf("connectOnStartup: error in PendingChannels", err)
		return
	}
	if len(channelPoints) > 0 || len(pendingChannels.PendingOpenChannels) > 0 {
		a.log.Infof("connectOnStartup: already has a channel, ignoring manual connection")
		return
	}

	var pubkey string
	for {
		pubkey, err = a.connectLSP()
		if err == nil {
			break
		}

		a.log.Warnf("Failed to connect to routing node %v", err)
		a.log.Warnf("Still not connected to routing node, retrying in 2 seconds.")
		time.Sleep(time.Duration(2 * time.Second))
	}
	a.openLSPChannel(pubkey)
}
