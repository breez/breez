package account

import (
	"context"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"

	breezservice "github.com/breez/breez/breez"
)

var (
	waitConnectTimeout = time.Second * 60
)

type onlineNotifier struct {
	sync.Mutex
	ntfnChan chan struct{}
	isOnline bool
}

// NewOnlineNotifier creates a new onlineNotifier
func newOnlineNotifier() *onlineNotifier {
	return &onlineNotifier{
		ntfnChan: make(chan struct{}),
	}
}

func (n *onlineNotifier) connected() bool {
	n.Lock()
	defer n.Unlock()
	return n.isOnline
}

func (n *onlineNotifier) setOffline() {
	n.Lock()
	defer n.Unlock()
	n.ntfnChan = make(chan struct{})
	n.isOnline = false
}

func (n *onlineNotifier) setOnline() {
	n.Lock()
	// prevent calling multiple times to setOnline and causing panic of closing a closed
	// channel.
	var ntfnChan chan struct{}
	if !n.isOnline {
		ntfnChan = n.ntfnChan
	}
	n.isOnline = true
	n.Unlock()
	if ntfnChan != nil {
		close(ntfnChan)
	}
}

func (n *onlineNotifier) notifyWhenOnline() <-chan struct{} {
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
	return nil
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
			enabled, err := a.breezDB.AccountEnabled()
			if err != nil {
				return nil, err
			}
			if !enabled {
				return nil, nil
			}
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
