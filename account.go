package breez

import (
	"context"
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
	endpointTimeout = 30
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

/*
AddFunds is responsible for topping up an existing channel
*/
func AddFunds(notificationToken string) (string, error) {
	invoiceData := &data.InvoiceMemo{TransferRequest: true}
	memo, err := proto.Marshal(invoiceData)
	if err != nil {
		return "", err
	}

	invoice, err := lightningClient.AddInvoice(context.Background(), &lnrpc.Invoice{Memo: string(memo), Private: true, Expiry: 60 * 60 * 24 * 30})
	if err != nil {
		log.Criticalf("Failed to call AddInvoice %v", err)
		return "", err
	}

	c := breezservice.NewFundManagerClient(breezClientConnection)
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()

	r, err := c.AddFund(ctx, &breezservice.AddFundRequest{NotificationToken: notificationToken, PaymentRequest: invoice.PaymentRequest})
	if err != nil {
		log.Errorf("Error in AddFund: %v", err)
		return "", err
	}

	err = saveFundingAddress(r.Address)
	if err != nil {
		return "", err
	}
	return r.Address, nil
}

/*
GetFundStatus gets a notification token and does two things:
1. Register for notifications on all saved addresses
2. Fetch the current status for the saved addresses from the server
*/
func GetFundStatus(notificationToken string) (*data.FundStatusReply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	defer cancel()
	c := breezservice.NewFundManagerClient(breezClientConnection)
	addresses := fetchAllFundingAddresses()
	if len(addresses) == 0 {
		return &data.FundStatusReply{Status: data.FundStatusReply_NO_FUND}, nil
	}

	statusesMap, err := c.AddFundStatus(ctx, &breezservice.AddFundStatusRequest{NotificationToken: notificationToken, Addresses: addresses})
	if err != nil {
		return nil, err
	}

	var confirmedAddresses []string
	var hasWaitingConfirmation bool
	for address, status := range statusesMap.Statuses {
		if status.Confirmed {
			confirmedAddresses = append(confirmedAddresses, address)
		} else {
			hasWaitingConfirmation = true
		}
	}

	err = removeFundingAddresses(confirmedAddresses)
	if err != nil {
		return nil, err
	}

	status := data.FundStatusReply_NO_FUND
	if hasWaitingConfirmation {
		status = data.FundStatusReply_WAITING_CONFIRMATION
	} else if len(confirmedAddresses) > 0 {
		status = data.FundStatusReply_CONFIRMED
	}

	return &data.FundStatusReply{Status: status}, nil
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

func getAccountLimits() (remoteBalance int64, maxAllowedToReceive int64, maxAllowedPaymentAmount int64, er error) {
	channels, err := lightningClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		er = err
		return
	}

	for _, b := range channels.Channels {
		acountMinAmount := b.Capacity / 100
		if acountMinAmount < int64(lnwallet.DefaultDustLimit()) {
			acountMinAmount = int64(lnwallet.DefaultDustLimit())
		}
		thisChannelCanReceive := b.RemoteBalance - acountMinAmount
		if thisChannelCanReceive < 0 {
			thisChannelCanReceive = 0
		}
		if maxAllowedToReceive < thisChannelCanReceive {
			maxAllowedToReceive = thisChannelCanReceive
		}
		remoteBalance += b.RemoteBalance
	}

	maxAllowedPaymentAmount = maxPaymentAllowedSat

	return
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

	remoteBalance, maxAllowedToReceive, maxPaymentAmount, err := getAccountLimits()
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
		RemoteBalance:         remoteBalance,
		MaxAllowedToReceive:   maxAllowedToReceive,
		MaxPaymentAmount:      maxPaymentAmount,
		Status:                accStatus,
		WalletBalance:         walletBalance.ConfirmedBalance,
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
	if err != nil {
		log.Errorf("Failed to subscribe peers %v", err)
		return err
	}
	for {
		notification, err := subscription.Recv()
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
