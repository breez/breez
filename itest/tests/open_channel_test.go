package tests

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/breez/breez/data"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		fmt.Printf("error in setup %v", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestZeroConfSimple(t *testing.T) {
	test := newTestFramework(t)
	bobClient := lnrpc.NewLightningClient(test.bobNode)
	breezClient := lnrpc.NewLightningClient(test.breezNode)
	aliceClient := lnrpc.NewLightningClient(test.aliceNode)

	openBreezChannel(t, test, test.bobBreezClient, test.bobNode)
	invoice, err := bobClient.AddInvoice(context.Background(), &lnrpc.Invoice{
		Value: 500000,
	})
	if err != nil {
		t.Fatalf("failed to create bob invoice %v", err)
	}
	payRes, err := breezClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{
		PaymentRequest: invoice.PaymentRequest,
	})
	if err != nil {
		t.Fatalf("failed to send payment from Breez to Bob %v", err)
	}
	if payRes.PaymentError != "" {
		t.Fatalf("failed to send payment from Breez to Bob %v", payRes.PaymentError)
	}

	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %w", err)
	}
	aliceClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Host:   list.Lsps["lspd-secret"].Host,
			Pubkey: list.Lsps["lspd-secret"].Pubkey,
		},
	})

	reply, err := test.aliceBreezClient.AddInvoice(context.Background(), &data.AddInvoiceRequest{
		InvoiceDetails: &data.InvoiceMemo{
			Description: "Zero conf",
			Amount:      100000,
		},
		LspInfo: list.Lsps["lspd-secret"],
	})
	if err != nil {
		t.Fatalf("failed to generate alice invoice %v", err)
	}
	res, err := bobClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{
		PaymentRequest: reply.PaymentRequest,
	})
	if err != nil {
		t.Fatalf("failed to send payment from Bob %v", err)
	}
	if res.PaymentError != "" {
		t.Fatalf("failed to send payment from Bob %v", res.PaymentError)
	}
}

// func TestSubswap(t *testing.T) {
// 	test := newTestFramework(t)
// 	// subswap := lnrpc.NewLightningClient(test.subswapNode)
// 	aliceClient := lnrpc.NewLightningClient(test.aliceNode)
// 	breezClient := lnrpc.NewLightningClient(test.breezNode)

// list, err := breezAPIClient.GetLSPList(context.Background(), &data.LSPListRequest{})
// if err != nil {
// 	t.Fatalf("failed to get lsp list %w", err)
// }

// subswapAddr, err := subswap.NewAddress(context.Background(),
// 	&lnrpc.NewAddressRequest{Type: lnrpc.AddressType_NESTED_PUBKEY_HASH})
// if err != nil {
// 	t.Fatalf("failed to get address from subswapper %w", err)
// }
// _, err = breezClient.SendCoins(context.Background(),
// 	&lnrpc.SendCoinsRequest{Addr: subswapAddr.Address, Amount: 10000000})
// if err != nil {
// 	t.Fatalf("failed to send coins to local client %w", err)
// }
// test.GenerateBlocks(10)

// subswap
//lsp := list.Lsps["lspd-secret"]
// lspAddress := &lnrpc.LightningAddress{
// 	Pubkey: lsp.Pubkey,
// 	Host:   lsp.Host,
// }
//initSwapperNode(t, subswap, lspAddress)

// 	openChannel(t, test)

// 	add, err := aliceClient.NewAddress(context.Background(),
// 		&lnrpc.NewAddressRequest{Type: lnrpc.AddressType_NESTED_PUBKEY_HASH})
// 	if err != nil {
// 		t.Fatalf("failed to get address from alice %w", err)
// 	}

// 	_, err = breezClient.SendCoins(context.Background(),
// 		&lnrpc.SendCoinsRequest{Addr: add.Address, Amount: 10000000})
// 	if err != nil {
// 		t.Fatalf("failed to send coins to alice %w", err)
// 	}
// 	test.GenerateBlocks(10)
// 	balance, err := aliceClient.WalletBalance(context.Background(), &lnrpc.WalletBalanceRequest{})
// 	if err != nil {
// 		t.Fatalf("failed to get alice balance %w", err)
// 	}
// 	if balance.ConfirmedBalance == 0 {
// 		t.Fatalf("expected positive balance")
// 	}
// 	res, err := test.aliceBreezClient.AddFundInit(context.Background(), &data.AddFundInitRequest{NotificationToken: "testtoken"})
// 	if err != nil {
// 		t.Fatalf("error in AddFundInit %v", err)
// 	}
// 	_, err = aliceClient.SendCoins(context.Background(), &lnrpc.SendCoinsRequest{
// 		Addr:       res.Address,
// 		Amount:     100000,
// 		TargetConf: 1,
// 	})
// 	if err != nil {
// 		t.Fatalf("error in SendCoins from alice %v", err)
// 	}
// 	test.GenerateBlocks(10)
// 	err = poll(func() bool {
// 		chanBalance, err := aliceClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
// 		if err != nil {
// 			t.Fatalf("error in ChannelBalance from alice %v", err)
// 		}
// 		return chanBalance.Balance > 0
// 	}, time.Second*10)
// 	if err != nil {
// 		t.Fatalf("Swap failed, got zero in ChannelBalance from alice")
// 	}
// }

func openBreezChannel(t *testing.T, test *framework,
	breezAPIClient data.BreezAPIClient, breezLNDCon *grpc.ClientConn) {
	breezClient := lnrpc.NewLightningClient(test.breezNode)
	localClient := lnrpc.NewLightningClient(breezLNDCon)
	test.GenerateBlocks(5)

	_, err := breezAPIClient.ConnectToLSP(
		context.Background(), &data.ConnectLSPRequest{LspId: "lspd-secret"})

	if err != nil {
		t.Fatalf("failed to connect to LSP: %v", err)
	}
	test.GenerateBlocks(5)

	var bobChanID uint64
	err = poll(func() bool {
		chanRes, err := localClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to list local channels: %v", err)
		}
		if len(chanRes.Channels) != 1 {
			t.Logf("expected 1 channel got %v", len(chanRes.Channels))
			return false
		}
		bobChanID = chanRes.Channels[0].ChanId
		return true
	}, time.Second*10)
	if err != nil {
		t.Fatalf("expected 1 channel")
	}

	err = poll(func() bool {
		chanRes, err := breezClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to list local channels: %v", err)
		}
		for _, c := range chanRes.Channels {
			if c.ChanId == bobChanID && c.Active {
				return true
			}
		}
		return false
	}, time.Second*10)
	if err != nil {
		t.Fatalf("expected 1 channel")
	}
}

// func initSwapperNode(t *testing.T, subswapNode lnrpc.LightningClient, lspPeer *lnrpc.LightningAddress) {
// 	balance, err := subswapNode.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
// 	if err != nil {
// 		t.Fatalf("failed to get sub swap node balance")
// 	}
// 	swapPeers, _ := subswapNode.ListPeers(context.Background(), &lnrpc.ListPeersRequest{})
// 	if balance.Balance == 0 {
// 		if len(swapPeers.Peers) == 0 {
// 			_, err = subswapNode.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
// 				Addr: lspPeer,
// 			})
// 			if err != nil {
// 				t.Fatalf("failed to connect to breez from lsp %w", err)
// 			}
// 		}
// 		_, err = subswapNode.OpenChannelSync(context.Background(), &lnrpc.OpenChannelRequest{
// 			NodePubkeyString:   lspPeer.Pubkey,
// 			LocalFundingAmount: 1000000,
// 			TargetConf:         1,
// 		})
// 		if err != nil {
// 			t.Fatalf("failed to open channel to breez from lsp %w", err)
// 		}
// 	}
// }
