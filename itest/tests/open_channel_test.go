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
