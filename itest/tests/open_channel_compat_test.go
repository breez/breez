package tests

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func Test_open_channel_competability(t *testing.T) {
	t.Logf("Testing Test_open_channel_competability")
	test := newTestFramework(t)
	lndClient := lnrpc.NewLightningClient(test.lndNode)
	bobClient := lnrpc.NewLightningClient(test.bobNode)
	breezClient := lnrpc.NewLightningClient(test.breezNode)

	lndAddress, err := lndClient.NewAddress(context.Background(),
		&lnrpc.NewAddressRequest{Type: lnrpc.AddressType_NESTED_PUBKEY_HASH})
	if err != nil {
		t.Fatalf("failed to get address from subswapper %v", err)
	}
	_, err = breezClient.SendCoins(context.Background(),
		&lnrpc.SendCoinsRequest{Addr: lndAddress.Address, Amount: 10000000})
	if err != nil {
		t.Fatalf("failed to send coins to local client %v", err)
	}
	test.GenerateBlocks(10)
	poll(func() bool {
		balance, err := lndClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
		return err != nil && balance.ConfirmedBalance > 0
	}, time.Second*10)

	bobInfo, err := bobClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		t.Fatalf("failed to get lnd info")
	}

	lndInfo, err := lndClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		t.Fatalf("failed to get lnd info")
	}

	_, err = bobClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Host:   "10.5.0.12",
			Pubkey: lndInfo.IdentityPubkey,
		},
	})
	if err != nil {
		t.Fatalf("failed to connect peer %v", err)
	}

	keyBytes, _ := hex.DecodeString(bobInfo.IdentityPubkey)
	_, err = lndClient.OpenChannel(context.Background(), &lnrpc.OpenChannelRequest{
		Private:            true,
		NodePubkey:         keyBytes,
		LocalFundingAmount: 300000,
		TargetConf:         3,
	})
	if err != nil {
		t.Fatalf("failed to open client")
	}
	time.Sleep(5 * time.Second)

	bobPending, err := bobClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		t.Fatalf("failed to get bob pending channels")
	}
	if len(bobPending.PendingOpenChannels) != 1 {
		t.Fatalf("expected 1 pending channel for bob")
	}

	lndPending, err := lndClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		t.Fatalf("failed to get lnd pending channels")
	}
	if len(lndPending.PendingOpenChannels) != 1 {
		t.Fatalf("expected 1 pending channel for lnd")
	}
	test.GenerateBlocks(10)

	poll(func() bool {
		bobActive, err := bobClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to get bob active channels")
		}
		if len(bobActive.Channels) != 1 {
			return false
		}

		lndActive, err := lndClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to get lnd active channels")
		}
		if len(lndActive.Channels) != 1 {
			return false
		}
		return true

	}, time.Second*10)

}
