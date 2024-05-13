package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/breez/breez/data"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/lnrpc"
)

/*
CloseChannels attempts to cooperatively close all channels, sending the funds to
the specified address.
*/
func (a *Service) CloseChannels(address string) (*data.CloseChannelsReply, error) {
	lnclient := a.daemonAPI.APIClient()
	if !a.daemonRPCReady() {
		return nil, fmt.Errorf("API is not ready")
	}

	a.log.Info("Close channels requested")
	listReq := &lnrpc.ListChannelsRequest{}
	openChannels, err := lnclient.ListChannels(context.Background(), listReq)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve channels: %v", err)
	}

	if len(openChannels.Channels) == 0 {
		return nil, errors.New("You don't have open channels.")
	}

	var channelsToSkip []*lnrpc.Channel
	var channelsToClose []*lnrpc.Channel
	for _, channel := range openChannels.Channels {
		if channel.Active {
			channelsToClose = append(channelsToClose, channel)
		} else {
			channelsToSkip = append(channelsToSkip, channel)
		}
	}

	a.log.Info(
		"Close channels has %d channels to close and %d channels to skip due "+
			"to inactivity.", len(channelsToClose), len(channelsToSkip))

	// result defines the result of closing a channel. The closing
	// transaction ID is populated if a channel is successfully closed.
	// Otherwise, the error that prevented closing the channel is populated.
	type result struct {
		RemotePubKey string
		ChannelPoint string
		ClosingTxid  string
		FailErr      string
	}

	// Launch each channel closure in a goroutine in order to execute them
	// in parallel. Once they're all executed, we will print the results as
	// they come.
	resultChan := make(chan result, len(channelsToClose))
	for _, channel := range channelsToClose {
		go func(channel *lnrpc.Channel) {
			res := result{}
			res.RemotePubKey = channel.RemotePubkey
			res.ChannelPoint = channel.ChannelPoint
			defer func() {
				a.log.Infof("Adding result of channel close %s to response.", res.ChannelPoint)
				resultChan <- res
				a.log.Infof("Done adding result of channel close %s to response.", res.ChannelPoint)
			}()

			// Parse the channel point in order to create the close
			// channel request.
			s := strings.Split(res.ChannelPoint, ":")
			if len(s) != 2 {
				res.FailErr = "Expected channel point with " +
					"format txid:index"
				return
			}
			index, err := strconv.ParseUint(s[1], 10, 32)
			if err != nil {
				res.FailErr = fmt.Sprintf("Unable to parse "+
					"channel point output index: %v", err)
				return
			}

			// Note we're not setting the fee rate, because it makes
			// complicated UI. Let the lightning nodes determine the fee
			// amongst themselves. Force closing channels can be done
			// through the developer console. No need for that here.
			req := &lnrpc.CloseChannelRequest{
				ChannelPoint: &lnrpc.ChannelPoint{
					FundingTxid: &lnrpc.ChannelPoint_FundingTxidStr{
						FundingTxidStr: s[0],
					},
					OutputIndex: uint32(index),
				},
				Force:           false,
				DeliveryAddress: address,
			}

			a.log.Info("About to close channel %s", res.ChannelPoint)
			txid, err := executeChannelClose(lnclient, req)
			if err != nil {
				a.log.Info("Close channel %s failed with %v", res.ChannelPoint, err)
				res.FailErr = fmt.Sprintf("Unable to close "+
					"channel: %v", err)
				return
			}

			a.log.Info("Close channel %s succeeded, txid %s", res.ChannelPoint, txid)
			res.ClosingTxid = txid
		}(channel)
	}

	resp := data.CloseChannelsReply{}
	for _, skippedChannel := range channelsToSkip {
		resp.Channels = append(resp.Channels, &data.CloseChannelResult{
			RemotePubkey: skippedChannel.RemotePubkey,
			ChannelPoint: skippedChannel.ChannelPoint,
			IsSkipped:    true,
		})
	}

	for range channelsToClose {
		a.log.Info("Waiting for channel close to complete")
		res := <-resultChan
		a.log.Info("Got channel close result for %s", res.ChannelPoint)
		resp.Channels = append(resp.Channels, &data.CloseChannelResult{
			RemotePubkey: res.RemotePubKey,
			ChannelPoint: res.ChannelPoint,
			ClosingTxid:  res.ClosingTxid,
			FailErr:      res.FailErr,
		})
	}

	return &resp, nil
}

// executeChannelClose attempts to close the channel from a request. The closing
// transaction ID is sent through `txidChan` as soon as it is broadcasted to the
// network. The block boolean is used to determine if we should block until the
// closing transaction receives all of its required confirmations.
func executeChannelClose(
	client lnrpc.LightningClient,
	req *lnrpc.CloseChannelRequest,
) (string, error) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.CloseChannel(ctx, req)
	if err != nil {
		return "", err
	}

	resp, err := stream.Recv()
	if err == io.EOF {
		return "", fmt.Errorf("unexpected EOF while reading close channel response")
	} else if err != nil {
		return "", err
	}

	var closingHash []byte
	switch update := resp.Update.(type) {
	case *lnrpc.CloseStatusUpdate_ClosePending:
		closingHash = update.ClosePending.Txid
	case *lnrpc.CloseStatusUpdate_ChanClose:
		closingHash = update.ChanClose.ClosingTxid
	}

	txid, err := chainhash.NewHash(closingHash)
	if err != nil {
		return "", err
	}
	return txid.String(), nil
}
