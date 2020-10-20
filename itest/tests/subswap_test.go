package tests

import (
	"context"
	"testing"
	"time"

	"github.com/breez/breez/data"
	"github.com/lightningnetwork/lnd/lnrpc"
)

func TestSubswap(t *testing.T) {
	t.Logf("Testing TestSubswap")
	test := newTestFramework(t)
	// subswap := lnrpc.NewLightningClient(test.subswapNode)
	aliceClient := lnrpc.NewLightningClient(test.aliceNode)
	breezClient := lnrpc.NewLightningClient(test.breezNode)

	// init swapper node
	test.initSwapperNode()
	test.GenerateBlocks(6)

	// send some coins to alice
	add, err := aliceClient.NewAddress(context.Background(),
		&lnrpc.NewAddressRequest{Type: lnrpc.AddressType_NESTED_PUBKEY_HASH})
	if err != nil {
		t.Fatalf("failed to get address from alice %v", err)
	}

	_, err = breezClient.SendCoins(context.Background(),
		&lnrpc.SendCoinsRequest{Addr: add.Address, Amount: 10000000})
	if err != nil {
		t.Fatalf("failed to send coins to alice %v", err)
	}
	test.GenerateBlocks(3)

	// ensure alice has positive balance
	balance, err := aliceClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
	if err != nil {
		t.Fatalf("failed to get alice balance %v", err)
	}
	if balance.ConfirmedBalance == 0 {
		t.Fatalf("expected positive balance")
	}

	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}
	aliceClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Host:   list.Lsps["lspd-secret"].Host,
			Pubkey: list.Lsps["lspd-secret"].Pubkey,
		},
	})

	res, err := test.aliceBreezClient.AddFundInit(context.Background(),
		&data.AddFundInitRequest{
			NotificationToken: "testtoken",
			LspID:             "lspd-secret",
		})
	if err != nil {
		t.Fatalf("error in AddFundInit %v", err)
	}
	_, err = aliceClient.SendCoins(context.Background(), &lnrpc.SendCoinsRequest{
		Addr:       res.Address,
		Amount:     100000,
		TargetConf: 1,
	})
	if err != nil {
		t.Fatalf("error in SendCoins from alice %v", err)
	}
	test.GenerateBlocks(3)
	err = poll(func() bool {
		chanBalance, err := aliceClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
		if err != nil {
			t.Fatalf("error in ChannelBalance from alice %v", err)
		}
		return chanBalance.Balance > 0
	}, time.Second*10)
	if err != nil {
		t.Fatalf("Swap failed, got zero in ChannelBalance from alice")
	}
}
