package breez

import (
	"strings"
	"sync"
	"time"

	breezservice "github.com/breez/breez/breez"
)

var (
	notification                         *notificationRequest
	subscriptionsSync                    sync.Mutex
	notificationTypeChannelOpened        = 1
	notificationTypeReceivePaymentsReady = 2
)

type notificationRequest struct {
	token            string
	notificationType int
}

//RegisterReceivePaymentReadyNotification register in breez server for notification regarding the confirmation
//of an existing pending channel. If there is not channels and no pending channels then this function waits for
//for a channel to be opened.
func RegisterReceivePaymentReadyNotification(token string) error {
	return setUserNotificationRequest(token, notificationTypeReceivePaymentsReady)
}

//RegisterChannelOpenedNotification register in breez server for notification regarding the confirmation
//of an existing pending channel. If there is not channels and no pending channels then this function waits for
//for a channel to be opened.
func RegisterChannelOpenedNotification(token string) error {
	log.Infof("RegisterChannelOpenedNotification *****")
	return setUserNotificationRequest(token, notificationTypeChannelOpened)
}

func setUserNotificationRequest(token string, notificationType int) error {
	log.Infof("setUserNotificationRequest notificationType = %v", notificationType)
	channelPoints, err := getBreezOpenChannelsPoints()
	if err != nil {
		return err
	}
	if len(channelPoints) > 0 {
		return nil
	}

	subscriptionsSync.Lock()
	defer subscriptionsSync.Unlock()
	notification = &notificationRequest{
		token:            token,
		notificationType: notificationType}

	return registerPendingChannelConfirmation()
}

func registerPendingChannelConfirmation() error {

	pendingChannelPoint, err := getPendingBreezChannelPoint()
	if err != nil {
		return err
	}
	if pendingChannelPoint == "" {
		return nil
	}

	c, ctx, cancel := getFundManager()
	defer cancel()

	subscriptionsSync.Lock()
	currentRequest := notification
	subscriptionsSync.Unlock()

	log.Infof("registerPendingChannelConfirmation for token %v and notification type = %v", currentRequest.token, currentRequest.notificationType)

	notificationTypeNeeded := breezservice.RegisterTransactionConfirmationRequest_READY_RECEIVE_PAYMENT
	if currentRequest.notificationType == notificationTypeChannelOpened {
		notificationTypeNeeded = breezservice.RegisterTransactionConfirmationRequest_CHANNEL_OPENED
	}
	_, err = c.RegisterTransactionConfirmation(ctx,
		&breezservice.RegisterTransactionConfirmationRequest{
			NotificationToken: currentRequest.token,
			TxID:              strings.Split(pendingChannelPoint, ":")[0],
			NotificationType:  notificationTypeNeeded,
		})
	if err != nil {
		subscriptionsSync.Lock()
		notification = nil
		subscriptionsSync.Unlock()
	}
	return err
}

func onRoutingNodePendingChannel() {
	registerPendingChannelConfirmation()
	onAccountChanged()
}

func onRoutingNodeOpenedChannel() {
	onAccountChanged()
}

func trackOpenedChannel() {
	ticker := time.NewTicker(time.Second * 10)
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
