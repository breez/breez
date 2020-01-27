package account

import (
	"context"
	"time"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/db"
	"github.com/lightningnetwork/lnd/lnrpc"
)

func (a *Service) syncClosedChannels() error {
	a.log.Infof("syncClosedChannels")
	lnclient := a.daemonAPI.APIClient()
	closedChannels, err := lnclient.ClosedChannels(context.Background(), &lnrpc.ClosedChannelsRequest{})
	if err != nil {
		return err
	}
	for _, c := range closedChannels.Channels {
		if err := a.onClosedChannel(c); err != nil {
			return err
		}
	}
	pendingChannels, err := lnclient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		return err
	}
	for _, c := range pendingChannels.WaitingCloseChannels {
		if err := a.onWaitingClosedChannel(c); err != nil {
			return err
		}
	}
	for _, c := range pendingChannels.PendingClosingChannels {
		if err := a.onPendingClosedChannel(c); err != nil {
			return err
		}
	}
	for _, c := range pendingChannels.PendingForceClosingChannels {
		if err := a.onPendingForceClosedChannel(c); err != nil {
			return err
		}
	}
	return nil
}

func (a *Service) onWaitingClosedChannel(waitingClose *lnrpc.PendingChannelsResponse_WaitingCloseChannel) error {
	a.log.Infof("onWaitingClosedChannel %v", waitingClose.Channel.ChannelPoint)
	if waitingClose.Channel.LocalBalance == 0 {
		a.log.Infof("closed channel skipped due to zero amount")
		return nil
	}
	paymentData := &db.PaymentInfo{
		Type:                db.ClosedChannelPayment,
		ClosedChannelPoint:  waitingClose.Channel.ChannelPoint,
		ClosedChannelStatus: db.WaitingClose,
		Amount:              waitingClose.Channel.LocalBalance,
		Fee:                 0,
		CreationTimestamp:   time.Now().Unix(),
		Description:         "Closed Channel",
	}
	return a.breezDB.AddChannelClosedPayment(paymentData)
}

func (a *Service) onPendingClosedChannel(pendingChannel *lnrpc.PendingChannelsResponse_ClosedChannel) error {
	a.log.Infof("onPendingClosedChannel %v", pendingChannel.Channel.ChannelPoint)
	if pendingChannel.Channel.LocalBalance == 0 {
		a.log.Infof("closed channel skipped due to zero amount")
		return nil
	}
	paymentData := &db.PaymentInfo{
		Type:                db.ClosedChannelPayment,
		ClosedChannelPoint:  pendingChannel.Channel.ChannelPoint,
		ClosedChannelStatus: db.PendingClose,
		ClosedChannelTxID:   pendingChannel.ClosingTxid,
		Amount:              pendingChannel.Channel.LocalBalance,
		Fee:                 0,
		CreationTimestamp:   time.Now().Unix(),
		Description:         "Closed Channel",
	}
	return a.breezDB.AddChannelClosedPayment(paymentData)
}

func (a *Service) onPendingForceClosedChannel(forceClosed *lnrpc.PendingChannelsResponse_ForceClosedChannel) error {
	a.log.Infof("onPendingForceClosedChannel %v", forceClosed.Channel.ChannelPoint)
	if forceClosed.Channel.LocalBalance == 0 {
		a.log.Infof("closed channel skipped due to zero amount")
		return nil
	}
	paymentData := &db.PaymentInfo{
		Type:                    db.ClosedChannelPayment,
		ClosedChannelPoint:      forceClosed.Channel.ChannelPoint,
		ClosedChannelStatus:     db.PendingClose,
		ClosedChannelTxID:       forceClosed.ClosingTxid,
		PendingExpirationHeight: forceClosed.MaturityHeight,
		Amount:                  forceClosed.Channel.LocalBalance,
		Fee:                     0,
		CreationTimestamp:       time.Now().Unix(),
		Description:             "Closed Channel",
	}
	return a.breezDB.AddChannelClosedPayment(paymentData)
}

func (a *Service) onClosedChannel(closeSummary *lnrpc.ChannelCloseSummary) error {
	a.log.Infof("onClosedChannel %v", closeSummary.ChannelPoint)
	if closeSummary.SettledBalance == 0 {
		a.log.Infof("closed channel skipped due to zero amount")
		return nil
	}
	closeTime, err := a.getBlockTime(int64(closeSummary.CloseHeight))
	if err != nil {
		return err
	}
	paymentData := &db.PaymentInfo{
		Type:                db.ClosedChannelPayment,
		ClosedChannelPoint:  closeSummary.ChannelPoint,
		ClosedChannelStatus: db.ConfirmedClose,
		ClosedChannelTxID:   closeSummary.ClosingTxHash,
		Amount:              closeSummary.SettledBalance,
		Fee:                 0,
		CreationTimestamp:   closeTime,
		Description:         "Closed Channel",
	}
	return a.breezDB.AddChannelClosedPayment(paymentData)
}

func (a *Service) getBlockTime(height int64) (int64, error) {
	cs, cleanup, err := chainservice.Get(a.cfg.WorkingDir, a.breezDB)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	hash, err := cs.GetBlockHash(height)
	if err != nil {
		return 0, err
	}
	header, err := cs.GetBlockHeader(hash)
	if err != nil {
		return 0, err
	}
	return header.Timestamp.Unix(), nil
}
