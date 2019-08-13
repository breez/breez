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

func (a *Service) routingNode(lspID string) (string, string, error) {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()
	r, err := c.LSPList(ctx, &breezservice.LSPListRequest{Pubkey: a.daemonAPI.NodePubkey()})
	if err != nil {
		a.log.Infof("LSPList returned an error: %v", err)
		return "", "", err
	}
	l, ok := r.Lsps[lspID]
	if !ok {
		a.log.Infof("The LSP ID is not in the LSPList: %v", lspID)
		return "", "", fmt.Errorf("The LSP ID is not in the LSPList: %v", lspID)
	}
	return l.Pubkey, l.Host, nil
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

/*
OpenLSPChannel is responsible for creating a new channel with the LSP
*/
func (a *Service) OpenLSPChannel(lspID string) error {
	a.log.Info("openLSPChannel started...")
	_, err, _ := createChannelGroup.Do("createChannel", func() (interface{}, error) {
		for {

			err := a.connectAndOpenChannel(lspID)
			if err == nil {
				return nil, nil
			}

			a.log.Errorf("Error in openChannel: %v, retrying in 25 seconds...", err)
			time.Sleep(time.Second * 25)
		}
	})
	return err
}

func (a *Service) connectAndOpenChannel(lspID string) error {
	if err := a.connectLSP(lspID); err != nil {		
		return err
	}

	hasChan, err := a.hasChannel()
	if err != nil {		
		return err
	}

	// open channel if no channel exists.
	if !hasChan {
		lnclient := a.daemonAPI.APIClient()
		lnInfo, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if err != nil {			
			return err
		}

		c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
		defer cancel()
		_, err = c.OpenLSPChannel(ctx, &breezservice.OpenLSPChannelRequest{LspId: lspID, Pubkey: lnInfo.IdentityPubkey})
		if err != nil {			
			return err
		}

		a.onRoutingNodePendingChannel()
	}

	return nil
}

func (a *Service) connectLSP(lspID string) error {
	pubkey, host, err := a.routingNode(lspID)
	if err != nil {
		return err
	}
	if a.isConnected(pubkey) {
		return nil
	}
	lnclient := a.daemonAPI.APIClient()
	a.log.Infof("Connecting to routing node host: %v, pubKey: %v", host, pubkey)
	_, err = lnclient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: pubkey,
			Host:   host,
		},
		Perm: true,
	})
	if err != nil {
		return err
	}

	return nil
}

func (a *Service) hasChannel() (bool, error) {
	channelPoints, _, err := a.getOpenChannels()
	if err != nil {
		return false, fmt.Errorf("openLSPChannel got error in getBreezOpenChannels %v", err)
	}

	if len(channelPoints) > 0 {
		a.log.Infof("openLSPChannel already has a channel with breez, doing nothing")
		return true, nil
	}
	pendingChannels, err := a.getPendingChannelPoint()
	if err != nil {
		return false, fmt.Errorf("openLSPChannel got error in getPendingBreezChannelPoint %v", err)
	}

	if len(pendingChannels) > 0 {
		a.onRoutingNodePendingChannel()
		a.log.Infof("openLSPChannel already has a pending channel with breez, doing nothing")
		return true, nil
	}

	return false, nil
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
