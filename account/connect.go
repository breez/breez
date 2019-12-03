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
	var nodesToConnect []*lnrpc.NodeInfo

	lnclient := a.daemonAPI.APIClient()
	channels, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		InactiveOnly: true,
	})
	if err != nil {
		return err
	}
	for _, c := range channels.Channels {
		nodeInfo, err := lnclient.GetNodeInfo(context.Background(), &lnrpc.NodeInfoRequest{PubKey: c.RemotePubkey})
		if err != nil {
			a.log.Infof("ConnectChannelsPeers got error trying to fetch node %v", nodeInfo)
			continue
		}
		nodesToConnect = append(nodesToConnect, nodeInfo)
	}

	pendingChannels, err := lnclient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return err
	}
	for _, c := range pendingChannels.PendingOpenChannels {
		nodeInfo, err := lnclient.GetNodeInfo(context.Background(), &lnrpc.NodeInfoRequest{PubKey: c.Channel.RemoteNodePub})
		if err != nil {
			a.log.Infof("ConnectChannelsPeers got error trying to fetch node %v", nodeInfo)
			continue
		}
		nodesToConnect = append(nodesToConnect, nodeInfo)
	}

	for _, n := range nodesToConnect {
		if len(n.GetNode().Addresses) > 0 {
			address := n.GetNode().Addresses[0]
			a.log.Infof("Connecting to peer %v with address= %v", n.GetNode().PubKey, address.Addr)
			lnclient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
				Addr: &lnrpc.LightningAddress{
					Pubkey: n.GetNode().PubKey,
					Host:   address.Addr,
				},
				Perm: true,
			})
		}
	}

	return nil
}

type regularLSP struct {
	lspID string
}

func NewRegularLSP(lspID string) *regularLSP {
	return &regularLSP{lspID: lspID}
}

func (lsp *regularLSP) Connect(a *Service) error {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()
	r, err := c.LSPList(ctx, &breezservice.LSPListRequest{Pubkey: a.daemonAPI.NodePubkey()})
	if err != nil {
		a.log.Infof("LSPList returned an error: %v", err)
		return err
	}
	l, ok := r.Lsps[lsp.lspID]
	if !ok {
		a.log.Infof("The LSP ID is not in the LSPList: %v", lsp.lspID)
		return fmt.Errorf("The LSP ID is not in the LSPList: %v", lsp.lspID)
	}

	return a.ConnectPeer(l.Pubkey, l.Host)
}

func (lsp *regularLSP) OpenChannel(a *Service, pubkey string) error {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()
	_, err := c.OpenLSPChannel(ctx, &breezservice.OpenLSPChannelRequest{LspId: lsp.lspID, Pubkey: pubkey})
	if err != nil {
		return err
	}
	return nil
}

type lsp interface {
	Connect(a *Service) error
	OpenChannel(a *Service, pubkey string) error
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
	l := NewRegularLSP(lspID)
	lsp := lsp(l)
	return a.openChannel(lsp, false)
}

func (a *Service) openChannel(l lsp, force bool) error {
	a.log.Info("openChannel started...")
	_, err, _ := createChannelGroup.Do("createChannel", func() (interface{}, error) {
		err := a.connectAndOpenChannel(l, force)
		a.log.Info("openChannel finished, error = %v", err)
		return nil, err
	})
	return err
}

func (a *Service) connectAndOpenChannel(l lsp, force bool) error {
	err := l.Connect(a)
	if err != nil {
		return err
	}
	var dontOpen bool
	if !force {
		dontOpen, err = a.hasChannel()
		if err != nil {
			return err
		}
	}
	// open channel if no channel exists.
	if !dontOpen {
		lnclient := a.daemonAPI.APIClient()
		lnInfo, err := lnclient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if err != nil {
			return err
		}
		err = l.OpenChannel(a, lnInfo.IdentityPubkey)
		if err != nil {
			return err
		}

		a.onRoutingNodePendingChannel()
	}
	return nil
}

func (a *Service) ConnectPeer(pubkey, host string) error {
	if a.isConnected(pubkey) {
		return nil
	}
	lnclient := a.daemonAPI.APIClient()
	a.log.Infof("Connecting to routing node host: %v, pubKey: %v", host, pubkey)
	_, err := lnclient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
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
