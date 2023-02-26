package tests

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/breez/breez/data"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

type zeroConfTest struct {
	amountSat        int64
	expectedChannels int
}

func Test_zero_conf_10k(t *testing.T) {
	t.Logf("Testing Test_zero_conf_10k")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        10_000,
			expectedChannels: 1,
		},
	})
}

func Test_zero_conf_100k_50k(t *testing.T) {
	t.Logf("Testing Test_zero_conf_100k_50k")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        50_000,
			expectedChannels: 1,
		},
	})
}

func Test_zero_conf_100k_100k(t *testing.T) {
	t.Logf("Testing Test_zero_conf_100k_100k")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        100_000,
			expectedChannels: 2,
		},
	})
}

func Test_zero_conf_100k_100k_pay_150k(t *testing.T) {
	t.Logf("Testing Test_zero_conf_100k_100k_pay_150k")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        100_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -150_000,
			expectedChannels: 2,
		},
	})
}

func Test_zero_conf_100k_100k_pay_150k_300k(t *testing.T) {
	t.Logf("Testing Test_zero_conf_100k_100k_pay_150k_300k")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        100_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -150_000,
			expectedChannels: 2,
		},
		{
			amountSat:        300_000,
			expectedChannels: 3,
		},
	})
}

func Test_zero_conf_LSP_fee(t *testing.T) {
	t.Logf("Testing Test_zero_conf_LSP_fee")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
	})

	poll(func() bool {
		payments, err := test.aliceBreezClient.ListPayments(context.Background(), &data.ListPaymentsRequest{})
		if err != nil {
			t.Fatalf("failed to get list of payments %v", err)
		}
		fmt.Printf("payments.PaymentsList[0].Fee = %v\n", payments.PaymentsList[0].Fee)
		return len(payments.PaymentsList) == 1 && payments.PaymentsList[0].Fee == 2000
	}, time.Second*10)
}

func Test_zero_conf_send_all_at_once(t *testing.T) {
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        100_000,
			expectedChannels: 2,
		},
		{
			amountSat:        150_000,
			expectedChannels: 3,
		},
		{
			amountSat:        -337500 + 6,
			expectedChannels: 3,
		},
	})
}

func Test_zero_conf_send_all(t *testing.T) {
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100_000,
			expectedChannels: 1,
		},
		{
			amountSat:        100_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -20_000,
			expectedChannels: 2,
		},
		{
			amountSat:        -11_988,
			expectedChannels: 2,
		},
	})
}

func Test_zero_conf_close(t *testing.T) {
	t.Logf("Testing Test_zero_conf_close")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        100000,
			expectedChannels: 1,
		},
	})
	aliceClient := lnrpc.NewLightningClient(test.aliceNode)
	ch, err := aliceClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("unexpected error in list alice channels")
	}

	parts := strings.Split(ch.Channels[0].ChannelPoint, ":")
	outputIndex, err := strconv.Atoi(parts[1])
	closeRes, err := aliceClient.CloseChannel(context.Background(), &lnrpc.CloseChannelRequest{
		ChannelPoint: &lnrpc.ChannelPoint{
			FundingTxid: &lnrpc.ChannelPoint_FundingTxidStr{
				FundingTxidStr: parts[0],
			},

			OutputIndex: uint32(outputIndex),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error in close alice channel")
	}
	for {
		closeUpdate, err := closeRes.Recv()
		if err != nil {
			t.Fatalf("failed in close channel event %v", err)
		}
		if pending, ok := closeUpdate.Update.(*lnrpc.CloseStatusUpdate_ClosePending); ok {
			test.test.Logf("pending closed channel: %v", pending.ClosePending.Txid)
			break
		}
	}
	test.GenerateBlocks(8)

	ch, err = aliceClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("unexpected error in list alice channels")
	}
	if len(ch.Channels) > 0 {
		t.Fatalf("expected zero channels got %v", len(ch.Channels))
	}

	pCh, err := aliceClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		t.Fatalf("unexpected error in list alice pending channels")
	}
	if len(pCh.PendingClosingChannels) > 0 {
		t.Fatalf("expected zero pending channels got %v", len(pCh.PendingClosingChannels))
	}
}

func runZeroConfMultiple(test *framework, tests []zeroConfTest) {
	bobClient := lnrpc.NewLightningClient(test.bobNode)
	breezNodeClient := lnrpc.NewLightningClient(test.breezNode)
	aliceClient := lnrpc.NewLightningClient(test.aliceNode)
	t := test.test
	test.initSwapperNode()
	openBreezChannel(t, test, test.bobBreezClient, test.bobNode)
	invoice, err := bobClient.AddInvoice(context.Background(), &lnrpc.Invoice{
		Value: 500000,
	})
	if err != nil {
		t.Fatalf("failed to create bob invoice %v", err)
	}
	payRes, err := breezNodeClient.SendPaymentSync(context.Background(), &lnrpc.SendRequest{
		PaymentRequest: invoice.PaymentRequest,
	})
	if err != nil {
		t.Fatalf("failed to send payment from Breez to Bob %v", err)
	}
	if payRes.PaymentError != "" {
		t.Fatalf("failed to send payment from Breez to Bob %v", payRes.PaymentError)
	}

	time.Sleep(2 * time.Second)

	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}
	_, err = aliceClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Host:   list.Lsps["lspd-secret"].Host,
			Pubkey: list.Lsps["lspd-secret"].Pubkey,
		},
	})
	if err != nil {
		t.Fatalf("failed to connect alice to lspd %v", err)
	}

	for _, zeroConfTest := range tests {
		amount := zeroConfTest.amountSat
		breezClient := test.aliceBreezClient
		senderClient := test.bobBreezClient
		senderLNDNode := lnrpc.NewLightningClient(test.bobNode)
		receiverLNDNode := lnrpc.NewLightningClient(test.aliceNode)
		//senderRouter := routerrpc.NewRouterClient(test.bobNode)
		lessZero := amount < 0
		if amount < 0 {
			amount = amount * -1
			breezClient = test.bobBreezClient
			senderClient = test.aliceBreezClient
			senderLNDNode = lnrpc.NewLightningClient(test.aliceNode)
			receiverLNDNode = lnrpc.NewLightningClient(test.bobNode)
		}

		reply, err := breezClient.AddInvoice(context.Background(), &data.AddInvoiceRequest{
			InvoiceDetails: &data.InvoiceMemo{
				Description: "Zero conf",
				Amount:      amount,
			},
			LspInfo: list.Lsps["lspd-secret"],
		})
		if err != nil {
			t.Fatalf("failed to generate alice invoice %v", err)
		}

		res, err := senderClient.PayInvoice(context.Background(), &data.PayInvoiceRequest{
			PaymentRequest: reply.PaymentRequest,
			Fee:            10000,
		})
		if err != nil || res.PaymentError != "" {
			t.Fatalf("failed to send payment from Bob %v %v %v %v", err, res.PaymentError, lessZero, reply.PaymentRequest)
		}
		poll(func() bool {
			senderChans, err := senderLNDNode.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
			if err != nil {
				t.Fatalf("failed to list sender channels %v", err)
			}
			receiverChans, err := receiverLNDNode.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
			if err != nil {
				t.Fatalf("failed to list sender channels %v", err)
			}

			for _, ch := range senderChans.Channels {
				if len(ch.PendingHtlcs) > 0 {
					return false
				}
			}
			for _, ch := range receiverChans.Channels {
				if len(ch.PendingHtlcs) > 0 {
					return false
				}
			}
			return true
		}, time.Second*10)

		channelCount := 0
		err = poll(func() bool {
			aliceInfo, err := aliceClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
			if err != nil {
				return false
			}
			chanRes, err := aliceClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
			if err != nil {
				t.Fatalf("failed to list local channels: %v", err)
			}
			channelCount = len(chanRes.Channels)
			if channelCount != zeroConfTest.expectedChannels {
				return false
			}

			breezChanRes, err := breezNodeClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
			if err != nil {
				t.Fatalf("failed to list local breez: %v", err)
			}

			var remoteChannels []*lnrpc.Channel
			for _, c := range breezChanRes.Channels {
				if c.RemotePubkey == aliceInfo.IdentityPubkey {
					remoteChannels = append(remoteChannels, c)
				}
			}
			if len(remoteChannels) != len(chanRes.Channels) {
				return false
			}

			return true
		}, time.Second*10)
		if err != nil {
			t.Fatalf("expected %v channels got %v channels", zeroConfTest.expectedChannels, channelCount)
		}

	}
}

func openBreezChannel(t *testing.T, test *framework,
	breezAPIClient data.BreezAPIClient, breezLNDCon *grpc.ClientConn) {

	breezClient := lnrpc.NewLightningClient(test.breezNode)
	localClient := lnrpc.NewLightningClient(breezLNDCon)
	test.GenerateBlocks(5)

	list, err := breezAPIClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}
	localClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Host:   list.Lsps["lspd-secret"].Host,
			Pubkey: list.Lsps["lspd-secret"].Pubkey,
		},
	})
	bobInfo, err := localClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		t.Fatalf("failed to get bob info")
	}
	_, err = breezClient.OpenChannelSync(context.Background(), &lnrpc.OpenChannelRequest{
		NodePubkeyString:   bobInfo.IdentityPubkey,
		LocalFundingAmount: 5000000,
		Private:            true,
		TargetConf:         1,
	})

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

		breezChans, err := breezClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to list local channels: %v", err)
		}
		if len(chanRes.Channels) != 1 {
			t.Logf("expected 1 channel got %v", len(chanRes.Channels))
			return false
		}

		var bobChannel *lnrpc.Channel
		for _, c := range breezChans.Channels {
			if c.ChanId == chanRes.Channels[0].ChanId {
				bobChannel = c
				break
			}
		}
		if bobChannel == nil {
			return false
		}

		bobChanID = bobChannel.ChanId
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
