package breez

import (
	"context"
	"io"
	"math"
	"sync/atomic"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/golang/protobuf/proto"

	breezservice "github.com/breez/breez/breez"
)

const (
	maxPaymentAllowedSat = math.MaxUint32 / 1000
	endpointTimeout      = 30
)

var (
	connectedToRoutingNode int32
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

/*
IsConnectedToRoutingNode returns the connection status to the routing node
*/
func IsConnectedToRoutingNode() bool {
	return atomic.LoadInt32(&connectedToRoutingNode) == 1
}

/*
createChannel is responsible for creating a new channel
*/
func createChannel(pubkey string) error {
	c := breezservice.NewFundManagerClient(breezClientConnection)
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	_, err := c.OpenChannel(ctx, &breezservice.OpenChannelRequest{PubKey: pubkey})
	if err != nil {
		log.Errorf("Error in openChannel: %v", err)
		return err
	}

	return nil
}

func getAccountStatus(walletBalance *lnrpc.WalletBalanceResponse) (data.Account_AccountStatus, error) {
	channelPoints, err := getOpenChannelsPoints()
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

func getOpenChannelsPoints() ([]string, error) {
	var channelPoints []string
	channels, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return nil, err
	}

	for _, c := range channels.Channels {
		channelPoints = append(channelPoints, c.ChannelPoint)
		log.Infof("Channel Point with Breez node = %v", c.ChannelPoint)
	}
	return channelPoints, nil
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

	nonDepositableBalance := walletBalance.ConfirmedBalance

	//In case we have funds in our wallet and the funding transaction is still didn't braodcasted and the channel is not opened yet
	//reserve up to "maxBtcFundingAmount" and the rest are "non depositable"
	if accStatus == data.Account_WAITING_DEPOSIT {
		if nonDepositableBalance-maxBtcFundingAmount > 0 {
			nonDepositableBalance -= maxBtcFundingAmount
		}
	}

	return &data.Account{
		Id:                    lnInfo.IdentityPubkey,
		Balance:               channelBalance.Balance,
		MaxAllowedToReceive:   maxAllowedToReceive,
		MaxAllowedToPay:       maxAllowedToPay,
		MaxPaymentAmount:      maxPaymentAllowedSat,
		Status:                accStatus,
		NonDepositableBalance: nonDepositableBalance,
	}, nil
}

//We need to put some dealy on this bacause there is a gap between transaction hit LND and the other side effects that happen
//like channel updates, balance updates etc...
func onAccountChanged() {
	time.Sleep(2 * time.Second)
	acc, err := calculateAccount()
	if err != nil {
		log.Errorf("Failed to calculate account %v", err)
	}
	accBuf, err := proto.Marshal(acc)
	if err != nil {
		log.Errorf("failed to marshal account, change event wasn't propagated")
		return
	}
	saveAccount(accBuf)
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_ACCOUNT_CHANGED}
}

func watchRoutingNodeConnection() error {
	log.Infof("watchRoutingNodeConnection started")
	subscription, err := lightningClient.SubscribePeers(context.Background(), &lnrpc.PeerSubscription{})
	if err == io.EOF {
		return err
	}
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
			createChannel(accData.Id)
			onAccountChanged()
		}
	}
}
