package breez

import (
	"context"
	"math"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/golang/protobuf/proto"
	"golang.org/x/sync/singleflight"

	breezservice "github.com/breez/breez/breez"
)

const (
	maxPaymentAllowedSat = math.MaxUint32 / 1000
	endpointTimeout      = 2
)

var (
	createChannelGroup singleflight.Group
)

/*
GetAccountInfo is responsible for retrieving some general account details such as balance, status, etc...
*/
func GetAccountInfo() (*data.Account, error) {
	accBuf, err := fetchAccount()
	if err != nil {
		return nil, err
	}
	account := &data.Account{}
	if accBuf != nil {
		err = proto.Unmarshal(accBuf, account)
	}
	return account, err
}

func updateNodeChannelPolicy(pubkey string) {
	for {
		if IsConnectedToRoutingNode() {
			c, ctx, cancel := getFundManager()
			_, err := c.UpdateChannelPolicy(ctx, &breezservice.UpdateChannelPolicyRequest{PubKey: pubkey})
			cancel()
			if err == nil {
				return
			}
			log.Errorf("Error in updateChannelPolicy: %v", err)
		}
		time.Sleep(time.Second * 5)
	}
}

/*
createChannel is responsible for creating a new channel
*/
func ensureRoutingChannelOpened() {
	log.Info("ensureRoutingChannelOpened started...")
	createChannelGroup.Do("createChannel", func() (interface{}, error) {
		for {
			lnInfo, err := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
			if err == nil {
				if IsConnectedToRoutingNode() {
					channelPoints, err := getBreezOpenChannelsPoints()
					if err != nil {
						log.Errorf("ensureRoutingChannelOpened got error in getBreezOpenChannelsPoints %v", err)
					}

					if len(channelPoints) > 0 {
						log.Infof("ensureRoutingChannelOpened already has a channel with breez, doing nothing")
						return nil, nil
					}
					pendingChannels, err := getPendingBreezChannelPoint()
					if err != nil {
						log.Errorf("ensureRoutingChannelOpened got error in getPendingBreezChannelPoint %v", err)
					}

					if len(pendingChannels) > 0 {
						log.Infof("ensureRoutingChannelOpened already has a pending channel with breez, doing nothing")
						return nil, nil
					}

					c, ctx, cancel := getFundManager()
					_, err = c.OpenChannel(ctx, &breezservice.OpenChannelRequest{PubKey: lnInfo.IdentityPubkey})
					cancel()
					if err == nil {
						onRoutingNodePendingChannel()
						return nil, nil
					}
				}
			}
			if err != nil {
				log.Errorf("Error in openChannel: %v", err)
			}
			time.Sleep(time.Second * 5)
		}
	})
}

func getAccountStatus(walletBalance *lnrpc.WalletBalanceResponse) (data.Account_AccountStatus, error) {
	channelPoints, err := getBreezOpenChannelsPoints()
	if err != nil {
		return -1, err
	}
	if len(channelPoints) > 0 {
		return data.Account_ACTIVE, nil
	}

	pendingChannels, err := lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
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

func getRecievePayLimit() (maxReceive int64, maxPay int64, err error) {
	channels, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return 0, 0, err
	}

	var maxAllowedToReceive int64
	var maxAllowedToPay int64
	for _, b := range channels.Channels {
		accountMinAmount := b.Capacity / 100
		if accountMinAmount < int64(lnwallet.DefaultDustLimit()) {
			accountMinAmount = int64(lnwallet.DefaultDustLimit())
		}
		thisChannelCanReceive := b.RemoteBalance - accountMinAmount
		if thisChannelCanReceive < 0 {
			thisChannelCanReceive = 0
		}
		if maxAllowedToReceive < thisChannelCanReceive {
			maxAllowedToReceive = thisChannelCanReceive
		}

		thisChannelCanPay := b.LocalBalance - accountMinAmount
		if thisChannelCanPay < 0 {
			thisChannelCanPay = 0
		}
		if maxAllowedToPay < thisChannelCanPay {
			maxAllowedToPay = thisChannelCanPay
		}
	}

	return maxAllowedToReceive, maxAllowedToPay, nil
}

func getRoutingNodeFeeRate(ourKey string) (int64, error) {
	chanIDs, err := getBreezOpenChannelsPoints()
	if err != nil {
		log.Errorf("Failed to get breez channels %v", err)
		return 0, err
	}

	if len(chanIDs) == 0 {
		return 0, nil
	}

	edge, err := lightningClient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{ChanId: chanIDs[0]})
	if err != nil {
		log.Errorf("Failed to get breez channel info %v", err)
		return 0, err
	}

	if ourKey == edge.Node1Pub && edge.Node2Policy != nil {
		return edge.Node2Policy.FeeBaseMsat / 1000, nil
	} else if edge.Node1Policy != nil {
		return edge.Node1Policy.FeeBaseMsat / 1000, nil
	}
	return 0, nil
}

func getBreezOpenChannelsPoints() ([]uint64, error) {
	var channelPoints []uint64
	channels, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return nil, err
	}

	for _, c := range channels.Channels {
		if c.RemotePubkey == cfg.RoutingNodePubKey {
			channelPoints = append(channelPoints, c.ChanId)
			log.Infof("Channel Point with Breez node = %v", c.ChannelPoint)
		}
	}
	return channelPoints, nil
}

func getPendingBreezChannelPoint() (string, error) {
	pendingChannels, err := lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return "", err
	}

	if len(pendingChannels.PendingOpenChannels) == 0 {
		return "", nil
	}

	for _, c := range pendingChannels.PendingOpenChannels {
		if c.Channel.RemoteNodePub == cfg.RoutingNodePubKey {
			return c.Channel.ChannelPoint, nil
		}
	}

	return "", nil
}

func calculateAccount() (*data.Account, error) {
	lnInfo, err := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, err
	}

	channelBalance, err := lightningClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
	if err != nil {
		return nil, err
	}

	walletBalance, err := lightningClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
	if err != nil {
		return nil, err
	}

	accStatus, err := getAccountStatus(walletBalance)
	if err != nil {
		return nil, err
	}

	maxAllowedToReceive, maxAllowedToPay, err := getRecievePayLimit()
	if err != nil {
		return nil, err
	}

	routingNodeFeeRate, err := getRoutingNodeFeeRate(lnInfo.IdentityPubkey)
	if err != nil {
		return nil, err
	}
	log.Infof("Routing node fee rate = %v", routingNodeFeeRate)

	onChainBalance := walletBalance.ConfirmedBalance
	return &data.Account{
		Id:                  lnInfo.IdentityPubkey,
		Balance:             channelBalance.Balance,
		MaxAllowedToReceive: maxAllowedToReceive,
		MaxAllowedToPay:     maxAllowedToPay,
		MaxPaymentAmount:    maxPaymentAllowedSat,
		Status:              accStatus,
		WalletBalance:       onChainBalance,
		RoutingNodeFee:      routingNodeFeeRate,
	}, nil
}

//We need to put some dealy on this bacause there is a gap between transaction hit LND and the other side effects that happen
//like channel updates, balance updates etc...
func onAccountChanged() {
	time.Sleep(2 * time.Second)
	calculateAccountAndNotify()
}

func calculateAccountAndNotify() (*data.Account, error) {
	acc, err := calculateAccount()
	if err != nil {
		log.Errorf("Failed to calculate account %v", err)
	}
	accBuf, err := proto.Marshal(acc)
	if err != nil {
		log.Errorf("failed to marshal account, change event wasn't propagated")
		return nil, err
	}
	saveAccount(accBuf)
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_ACCOUNT_CHANGED}
	return acc, nil
}
