package breez

import (
	"strings"
	"sync"
	"time"

	breezservice "github.com/breez/breez/breez"
)

var (
	openChanneNotificationTokens []string
	subscriptionsSync            sync.Mutex
)

//RegisterReceivePaymentReadyNotification register in breez server for notification regarding the confirmation
//of an existing pending channel. If there is not channels and no pending channels then this function waits for
//for a channel to be opened by only adding the register logic to the paymentReadySubscriptions
func RegisterReceivePaymentReadyNotification(token string) error {
	channelPoints, err := getBreezOpenChannelsPoints()
	if err != nil {
		return err
	}
	if len(channelPoints) > 0 {
		return nil
	}

	registered, err := registerPendingChannelConfirmation(token)
	if !registered {
		//we have no channel and no pending channel with routing node, just save the token for later
		subscriptionsSync.Lock()
		defer subscriptionsSync.Unlock()
		openChanneNotificationTokens = append(openChanneNotificationTokens, token)
	}

	return nil
}

func trackOpenedChannel() {
	ticker := time.NewTicker(time.Minute * 1)
	for {
		select {
		case <-ticker.C:
			channelPoints, err := getBreezOpenChannelsPoints()
			if err == nil && len(channelPoints) > 0 {
				ticker.Stop()
				onRoutingNodeOpenedChannel()
			}
		case <-quitChan:
			ticker.Stop()
			return
		}
	}
}

func registerPendingChannelConfirmation(token string) (bool, error) {

	pendingChannelPoint, err := getPendingBreezChannelPoint()
	if err != nil {
		return false, err
	}
	if pendingChannelPoint == "" {
		return false, nil
	}

	c, ctx, cancel := getFundManager()
	defer cancel()
	_, err = c.RegisterTransactionConfirmation(ctx,
		&breezservice.RegisterTransactionConfirmationRequest{
			NotificationToken: token,
			TxID:              strings.Split(pendingChannelPoint, ":")[0],
			NotificationType:  breezservice.RegisterTransactionConfirmationRequest_READY_RECEIVE_PAYMENT,
		})
	return err != nil, err
}

func onRoutingNodePendingChannel() {
	subscriptionsSync.Lock()
	defer subscriptionsSync.Unlock()
	for _, token := range openChanneNotificationTokens {
		registerPendingChannelConfirmation(token)
	}
	openChanneNotificationTokens = nil
	onAccountChanged()
}

func onRoutingNodeOpenedChannel() {
	onAccountChanged()
}
