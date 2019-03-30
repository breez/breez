package account

import (
	"context"
	"math"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/golang/protobuf/proto"
	"golang.org/x/sync/singleflight"

	breezservice "github.com/breez/breez/breez"
)

const (
	maxPaymentAllowedSat = math.MaxUint32 / 1000
	endpointTimeout      = 5
)

var (
	createChannelGroup singleflight.Group
)

// EnableAccount controls whether the account will be enabled or disabled.
// When disbled, no attempt will be made to open a channel with breez node.
func (a *Service) EnableAccount(enabled bool) error {
	if err := a.breezDB.EnableAccount(enabled); err != nil {
		a.log.Infof("Error in enabling account (enabled = %v) %v", enabled, err)
		return err
	}
	if enabled {
		go a.ensureRoutingChannelOpened()
	}

	a.onAccountChanged()
	return nil
}

/*
GetAccountInfo is responsible for retrieving some general account details such as balance, status, etc...
*/
func (a *Service) GetAccountInfo() (*data.Account, error) {
	accBuf, err := a.breezDB.FetchAccount()
	if err != nil {
		return nil, err
	}
	account := &data.Account{}
	if accBuf != nil {
		err = proto.Unmarshal(accBuf, account)
	}
	return account, err
}

func (a *Service) updateNodeChannelPolicy(pubkey string) {
	for {
		if a.IsConnectedToRoutingNode() {
			c, ctx, cancel := a.breezServices.NewFundManager()
			_, err := c.UpdateChannelPolicy(ctx, &breezservice.UpdateChannelPolicyRequest{PubKey: pubkey})
			cancel()
			if err == nil {
				return
			}
			a.log.Errorf("Error in updateChannelPolicy: %v", err)
		}
		time.Sleep(time.Second * 5)
	}
}

/*
createChannel is responsible for creating a new channel
*/
func (a *Service) ensureRoutingChannelOpened() {
	a.log.Info("ensureRoutingChannelOpened started...")
	createChannelGroup.Do("createChannel", func() (interface{}, error) {
		for {
			enabled, err := a.breezDB.AccountEnabled()
			if err != nil {
				return nil, err
			}
			if !enabled {
				return nil, nil
			}
			lnInfo, err := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
			if err == nil {
				if a.IsConnectedToRoutingNode() {
					channelPoints, _, err := a.getBreezOpenChannels()
					if err != nil {
						a.log.Errorf("ensureRoutingChannelOpened got error in getBreezOpenChannels %v", err)
					}

					if len(channelPoints) > 0 {
						a.log.Infof("ensureRoutingChannelOpened already has a channel with breez, doing nothing")
						return nil, nil
					}
					pendingChannels, err := a.getPendingBreezChannelPoint()
					if err != nil {
						a.log.Errorf("ensureRoutingChannelOpened got error in getPendingBreezChannelPoint %v", err)
					}

					if len(pendingChannels) > 0 {
						a.log.Infof("ensureRoutingChannelOpened already has a pending channel with breez, doing nothing")
						a.onRoutingNodePendingChannel()
						return nil, nil
					}

					c, ctx, cancel := a.breezServices.NewFundManager()
					_, err = c.OpenChannel(ctx, &breezservice.OpenChannelRequest{PubKey: lnInfo.IdentityPubkey})
					cancel()
					if err == nil {
						a.onRoutingNodePendingChannel()
						return nil, nil
					}
				}
			}
			if err != nil {
				a.log.Errorf("Error in openChannel: %v", err)
			}
			time.Sleep(time.Second * 5)
		}
	})
}

func (a *Service) getAccountStatus(walletBalance *lnrpc.WalletBalanceResponse) (data.Account_AccountStatus, error) {
	channelPoints, _, err := a.getBreezOpenChannels()
	if err != nil {
		return -1, err
	}
	if len(channelPoints) > 0 {
		return data.Account_ACTIVE, nil
	}

	pendingChannels, err := a.lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return -1, err
	}
	if len(pendingChannels.PendingOpenChannels) > 0 {
		return data.Account_PROCESSING_BREEZ_CONNECTION, nil
	}
	if len(pendingChannels.PendingClosingChannels) > 0 || len(pendingChannels.PendingForceClosingChannels) > 0 {
		return data.Account_PROCESSING_WITHDRAWAL, nil
	}

	if walletBalance.UnconfirmedBalance > 0 {
		return data.Account_WAITING_DEPOSIT_CONFIRMATION, nil
	}
	return data.Account_WAITING_DEPOSIT, nil
}

func (a *Service) getRecievePayLimit() (maxReceive, maxPay, maxReserve int64, err error) {
	channels, err := a.lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		return 0, 0, 0, err
	}

	pendingChannels, err := a.lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return 0, 0, 0, err
	}

	var maxAllowedToReceive int64
	var maxAllowedToPay int64
	var maxChanReserve int64

	processChannel := func(canReceive, canPay, chanReserve int64) {
		if canReceive < 0 {
			canReceive = 0
		}
		if maxAllowedToReceive < canReceive {
			maxAllowedToReceive = canReceive
		}

		if canPay < 0 {
			canPay = 0
		}
		if maxAllowedToPay < canPay {
			maxAllowedToPay = canPay
		}
		if maxChanReserve < chanReserve {
			maxChanReserve = chanReserve
		}
	}

	for _, b := range channels.Channels {
		thisChannelCanReceive := b.RemoteBalance - b.RemoteChanReserve
		thisChannelCanPay := b.LocalBalance - b.LocalChanReserve
		processChannel(thisChannelCanReceive, thisChannelCanPay, b.LocalChanReserve)
	}

	for _, b := range pendingChannels.PendingOpenChannels {
		processChannel(0, 0, b.Channel.LocalChanReserve)
	}

	return maxAllowedToReceive, maxAllowedToPay, maxChanReserve, nil
}

func (a *Service) getRoutingNodeFeeRate(ourKey string) (int64, error) {
	chanIDs, _, err := a.getBreezOpenChannels()
	if err != nil {
		a.log.Errorf("Failed to get breez channels %v", err)
		return 0, err
	}

	if len(chanIDs) == 0 {
		return 0, nil
	}

	edge, err := a.lightningClient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{ChanId: chanIDs[0]})
	if err != nil {
		a.log.Errorf("Failed to get breez channel info %v", err)
		return 0, err
	}

	if ourKey == edge.Node1Pub && edge.Node2Policy != nil {
		return edge.Node2Policy.FeeBaseMsat / 1000, nil
	} else if edge.Node1Policy != nil {
		return edge.Node1Policy.FeeBaseMsat / 1000, nil
	}
	return 0, nil
}

func (a *Service) getBreezOpenChannels() ([]uint64, []string, error) {
	var channelPoints []string
	var channelIds []uint64
	channels, err := a.lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return nil, nil, err
	}

	for _, c := range channels.Channels {
		if c.RemotePubkey == a.cfg.RoutingNodePubKey {
			channelPoints = append(channelPoints, c.ChannelPoint)
			channelIds = append(channelIds, c.ChanId)
			a.log.Infof("Channel Point with Breez node = %v", c.ChannelPoint)
		}
	}
	return channelIds, channelPoints, nil
}

func (a *Service) getPendingBreezChannelPoint() (string, error) {
	pendingChannels, err := a.lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return "", err
	}

	if len(pendingChannels.PendingOpenChannels) == 0 {
		return "", nil
	}

	for _, c := range pendingChannels.PendingOpenChannels {
		if c.Channel.RemoteNodePub == a.cfg.RoutingNodePubKey {
			return c.Channel.ChannelPoint, nil
		}
	}

	return "", nil
}

func (a *Service) calculateAccount() (*data.Account, error) {
	lnInfo, err := a.lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	channelBalance, err := a.lightningClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return nil, err
	}

	walletBalance, err := a.lightningClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return nil, err
	}

	accStatus, err := a.getAccountStatus(walletBalance)
	if err != nil {
		return nil, err
	}

	maxAllowedToReceive, maxAllowedToPay, maxChanReserve, err := a.getRecievePayLimit()
	if err != nil {
		return nil, err
	}

	routingNodeFeeRate, err := a.getRoutingNodeFeeRate(lnInfo.IdentityPubkey)
	if err != nil {
		a.log.Infof("Failed to get routing node fee %v", err)
	}
	a.log.Infof("Routing node fee rate = %v", routingNodeFeeRate)

	enabled, err := a.breezDB.AccountEnabled()
	if err != nil {
		return nil, err
	}

	onChainBalance := walletBalance.ConfirmedBalance
	return &data.Account{
		Id:                  lnInfo.IdentityPubkey,
		Balance:             channelBalance.Balance,
		MaxAllowedToReceive: maxAllowedToReceive,
		MaxAllowedToPay:     maxAllowedToPay,
		MaxPaymentAmount:    maxPaymentAllowedSat,
		MaxChanReserve:      maxChanReserve,
		Status:              accStatus,
		WalletBalance:       onChainBalance,
		RoutingNodeFee:      routingNodeFeeRate,
		Enabled:             enabled,
	}, nil
}

//We need to put some dealy on this bacause there is a gap between transaction hit LND and the other side effects that happen
//like channel updates, balance updates etc...
func (a *Service) onAccountChanged() {
	time.Sleep(2 * time.Second)
	a.calculateAccountAndNotify()
}

func (a *Service) calculateAccountAndNotify() (*data.Account, error) {
	acc, err := a.calculateAccount()
	if err != nil {
		a.log.Errorf("Failed to calculate account %v", err)
	}
	accBuf, err := proto.Marshal(acc)
	if err != nil {
		a.log.Errorf("failed to marshal account, change event wasn't propagated")
		return nil, err
	}
	a.breezDB.SaveAccount(accBuf)
	a.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_ACCOUNT_CHANGED})
	return acc, nil
}
