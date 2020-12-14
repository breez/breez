package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/breez/breez/data"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/walletrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"google.golang.org/grpc"
)

type offchainAction int

type zeroConfTest struct {
	amountSat        int64
	expectedChannels int
}

func getLSPRoutingHints(t *testing.T, lnclient lnrpc.LightningClient, lspInfo *data.LSPInformation) ([]*lnrpc.RouteHint, error) {
	channelsRes, err := lnclient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{
		PrivateOnly: true,
	})
	if err != nil {
		return nil, err
	}

	var hints []*lnrpc.RouteHint
	usedPeers := make(map[string]struct{})
	for _, h := range channelsRes.Channels {
		if _, ok := usedPeers[h.RemotePubkey]; ok {
			continue
		}
		ci, err := lnclient.GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{
			ChanId: h.ChanId,
		})
		if err != nil {
			t.Fatalf("Unable to add routing hint for channel %v error=%v", h.ChanId, err)
			continue
		}
		remotePolicy := ci.Node1Policy
		if h.RemotePubkey == lspInfo.Pubkey && ci.Node2Pub == h.RemotePubkey {
			remotePolicy = ci.Node2Policy
		}

		// skip non lsp channels without remote policy
		if remotePolicy == nil && h.RemotePubkey != lspInfo.Pubkey {
			continue
		}

		feeBaseMsat := uint32(lspInfo.BaseFeeMsat)
		proportionalFee := uint32(lspInfo.FeeRate * 1000000)
		cltvExpiryDelta := lspInfo.TimeLockDelta
		if remotePolicy != nil {
			feeBaseMsat = uint32(remotePolicy.FeeBaseMsat)
			proportionalFee = uint32(remotePolicy.FeeRateMilliMsat)
			cltvExpiryDelta = remotePolicy.TimeLockDelta
		}

		hints = append(hints, &lnrpc.RouteHint{
			HopHints: []*lnrpc.HopHint{
				{
					NodeId:                    h.RemotePubkey,
					ChanId:                    h.ChanId,
					FeeBaseMsat:               feeBaseMsat,
					FeeProportionalMillionths: proportionalFee,
					CltvExpiryDelta:           cltvExpiryDelta,
				},
			},
		})
		usedPeers[h.RemotePubkey] = struct{}{}
	}
	return hints, nil
}

func Test_zero_conf_mining_during_pending_sender(t *testing.T) {
	t.Logf("Testing Test_zero_conf_mining_during_pending_sender")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        50000,
			expectedChannels: 1,
		},
	})
	bobInvoiceClient := invoicesrpc.NewInvoicesClient(test.bobNode)
	bobLightningClient := lnrpc.NewLightningClient(test.bobNode)
	paymentPreimage := &lntypes.Preimage{}
	if _, err := rand.Read(paymentPreimage[:]); err != nil {
		t.Fatalf("failed to generate preimage %v", err)
	}
	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}

	hints, err := getLSPRoutingHints(t, bobLightningClient, list.Lsps["lspd-secret"])
	if err != nil {
		t.Fatalf("failed to get routing hints %v", err)
	}

	paymentHash := paymentPreimage.Hash()
	t.Logf("preImage: %v", paymentPreimage.String())
	t.Logf("preImage: %v", paymentHash.String())
	holdInvoice, err := bobInvoiceClient.AddHoldInvoice(context.Background(), &invoicesrpc.AddHoldInvoiceRequest{
		RouteHints: hints,
		Hash:       paymentHash[:],
		Value:      12000,
		Private:    true,
	})
	if err != nil {
		t.Fatalf("failed to add hold invoice %v", err)
	}
	fmt.Println(holdInvoice.PaymentRequest)
	go func() {
		c := lnrpc.NewLightningClient(test.aliceNode)
		_, err := c.SendPaymentSync(context.Background(), &lnrpc.SendRequest{
			PaymentRequest: holdInvoice.PaymentRequest,
		})
		if err != nil {
			t.Logf("failed to send payment from bob %v", err)
		}
	}()

	stream, err := bobInvoiceClient.SubscribeSingleInvoice(context.Background(),
		&invoicesrpc.SubscribeSingleInvoiceRequest{
			RHash: paymentHash[:],
		})
	if err != nil {
		t.Fatalf("failed to subscribe alice invoice %v", err)
	}
	for {
		payment, err := stream.Recv()
		if err != nil {
			t.Fatalf("error in payment %v", err)
		}
		t.Logf("invoice event state = %v", payment.State)
		if payment.State == lnrpc.Invoice_ACCEPTED {
			break
		}
	}
	stream.CloseSend()

	t.Logf("restarting alice")
	fmt.Println("restargin alice")
	if err := test.restartAlice(); err != nil {
		t.Fatalf("failed to restart alice %v", err)
	}
	time.Sleep(5 * time.Second)
	t.Logf("generating blocks")
	test.miner.Generate(6)

	if err := waitForNodeSynced(aliceDir, aliceAddress, 0); err != nil {
		t.Fatalf("failed to sync alice %v", err)
	}
	fmt.Println("after restargin alice")

	_, err = bobInvoiceClient.SettleInvoice(context.Background(), &invoicesrpc.SettleInvoiceMsg{
		Preimage: paymentPreimage[:],
	})
	if err != nil {
		t.Fatalf("failed to settle invoice %v", err)
	}

	breezClient := lnrpc.NewLightningClient(test.breezNode)
	err = poll(func() bool {
		breezChannels, err := breezClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to list breez node payments: %v", err)
		}
		for _, c := range breezChannels.Channels {
			if len(c.PendingHtlcs) > 0 {
				return false
			}
		}

		aliceClient := lnrpc.NewLightningClient(test.aliceNode)
		balanceRes, err := aliceClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
		if err != nil {
			return false
		}
		fmt.Println("balanceRes.Balance = ", balanceRes.Balance)
		return balanceRes.Balance < 40000
	}, time.Second*10)

	if err != nil {
		t.Fatalf("more than 1000 sat balance")
	}
}

func Test_zero_conf_mining_during_pending(t *testing.T) {
	t.Logf("Testing Test_zero_conf_mining_during_pending")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        10,
			expectedChannels: 1,
		},
	})
	invoiceClient := invoicesrpc.NewInvoicesClient(test.aliceNode)
	aliceLightningClient := lnrpc.NewLightningClient(test.aliceNode)
	paymentPreimage := &lntypes.Preimage{}
	if _, err := rand.Read(paymentPreimage[:]); err != nil {
		t.Fatalf("failed to generate preimage %v", err)
	}
	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}

	hints, err := getLSPRoutingHints(t, aliceLightningClient, list.Lsps["lspd-secret"])
	if err != nil {
		t.Fatalf("failed to get routing hints %v", err)
	}

	paymentHash := paymentPreimage.Hash()
	t.Logf("preImage: %v", paymentPreimage.String())
	t.Logf("preImage: %v", paymentHash.String())
	holdInvoice, err := invoiceClient.AddHoldInvoice(context.Background(), &invoicesrpc.AddHoldInvoiceRequest{
		RouteHints: hints,
		Hash:       paymentHash[:],
		Value:      1000,
		Private:    true,
	})
	if err != nil {
		t.Fatalf("failed to add hold invoice %v", err)
	}
	fmt.Println(holdInvoice.PaymentRequest)
	go func() {
		c := lnrpc.NewLightningClient(test.bobNode)
		_, err := c.SendPaymentSync(context.Background(), &lnrpc.SendRequest{
			PaymentRequest: holdInvoice.PaymentRequest,
		})
		if err != nil {
			t.Fatalf("failed to send payment from bob")
		}
	}()

	stream, err := invoiceClient.SubscribeSingleInvoice(context.Background(),
		&invoicesrpc.SubscribeSingleInvoiceRequest{
			RHash: paymentHash[:],
		})
	if err != nil {
		t.Fatalf("failed to subscribe alice invoice %v", err)
	}
	for {
		payment, err := stream.Recv()
		if err != nil {
			t.Fatalf("error in payment %v", err)
		}
		t.Logf("invoice event state = %v", payment.State)
		if payment.State == lnrpc.Invoice_ACCEPTED {
			break
		}
	}
	stream.CloseSend()
	test.GenerateBlocks(6)
	t.Logf("after generating blocks")
	time.Sleep(3 * time.Second)
	_, err = invoiceClient.SettleInvoice(context.Background(), &invoicesrpc.SettleInvoiceMsg{
		Preimage: paymentPreimage[:],
	})
	if err != nil {
		t.Fatalf("failed to settle invoice %v", err)
	}

	breezClient := lnrpc.NewLightningClient(test.breezNode)
	err = poll(func() bool {
		breezChannels, err := breezClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
		if err != nil {
			t.Fatalf("failed to list breez node payments: %v", err)
		}
		for _, c := range breezChannels.Channels {
			if len(c.PendingHtlcs) > 0 {
				return false
			}
		}

		balanceRes, err := aliceLightningClient.ChannelBalance(context.Background(), &lnrpc.ChannelBalanceRequest{})
		if err != nil {
			t.Fatalf("failed to list local channels: %v", err)
		}
		return balanceRes.Balance > 1000
	}, time.Second*10)

	if err != nil {
		t.Fatalf("more than 1000 sat balance")
	}
}

func Test_zero_conf_10(t *testing.T) {
	t.Logf("Testing Test_zero_conf_10")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        10,
			expectedChannels: 1,
		},
	})
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
		return len(payments.PaymentsList) == 1 && payments.PaymentsList[0].Fee == 100
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
			amountSat:        -349650 + 4,
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
			amountSat:        -19_988 + 200,
			expectedChannels: 2,
		},
	})
}

func Test_routing_hints_existing(t *testing.T) {
	t.Logf("Testing Test_routing_hints_existing")
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

	list, err := test.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %v", err)
	}

	reply, err := test.aliceBreezClient.AddInvoice(context.Background(), &data.AddInvoiceRequest{
		LspInfo: list.Lsps["lspd-secret"],
		InvoiceDetails: &data.InvoiceMemo{
			Description: "Zero conf",
			Amount:      1000,
		},
	})
	if err != nil {
		t.Fatalf("failed to add alice invoice %v", err)
	}
	aliceClient := lnrpc.NewLightningClient(test.aliceNode)
	decodeReply, err := aliceClient.DecodePayReq(context.Background(), &lnrpc.PayReqString{
		PayReq: reply.PaymentRequest,
	})
	if err != nil {
		t.Fatalf("failed to decode alice invoice %v", err)
	}
	if len(decodeReply.RouteHints) != 1 {
		t.Fatalf("expected 1 hint got %v", len(decodeReply.RouteHints))
	}
	hopChanID := decodeReply.RouteHints[0].HopHints[0].ChanId
	channReply, err := aliceClient.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if channReply.Channels[0].ChanId != hopChanID && channReply.Channels[1].ChanId != hopChanID {
		t.Fatalf("wrong hint")
	}
}

func Test_zero_conf_close(t *testing.T) {
	t.Logf("Testing Test_zero_conf_close")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        10000,
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

func Test_zero_conf_force_close(t *testing.T) {
	t.Logf("Testing Test_zero_conf_force_close")
	test := newTestFramework(t)
	runZeroConfMultiple(test, []zeroConfTest{
		{
			amountSat:        10000,
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
		Force: true,
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
			test.test.Logf("pending closed channel: %v\n\n", pending.ClosePending.Txid)
			payments, err := test.aliceBreezClient.ListPayments(context.Background(), &data.ListPaymentsRequest{})
			if err != nil {
				t.Fatalf("failed to get list of payments %v", err)
			}
			test.test.Logf("closed channel payment %+v\n\n", payments.PaymentsList[0])
			break
		}
	}

	err = poll(func() bool {
		payments, err := test.aliceBreezClient.ListPayments(context.Background(), &data.ListPaymentsRequest{})
		if err != nil {
			t.Fatalf("failed to get list of payments %v", err)
		}

		if len(payments.PaymentsList) == 2 && payments.PaymentsList[0].ClosedChannelPoint != "" {
			closeEntry := payments.PaymentsList[0]
			if closeEntry.ClosedChannelTxID != "" && closeEntry.ClosedChannelRemoteTxID != "" && closeEntry.IsChannelPending {
				test.test.Logf("waiting close payment %+v\n\n", payments.PaymentsList[0])
				return true
			}
		}
		return false
	}, time.Second*10)
	if err != nil {
		t.Fatalf("failed to poll for waiting closed channel")
	}

	test.GenerateBlocks(6)

	//var closingTxID, sweepTxId string
	err = poll(func() bool {
		payments, err := test.aliceBreezClient.ListPayments(context.Background(), &data.ListPaymentsRequest{})
		if err != nil {
			t.Fatalf("failed to get list of payments %v", err)
		}
		//test.test.Logf("before confirmed closed payment %+v\n", payments.PaymentsList[0])
		closeEntry := payments.PaymentsList[0]
		if closeEntry.ClosedChannelTxID != "" && closeEntry.ClosedChannelRemoteTxID == "" && closeEntry.IsChannelPending {
			//closingTxID = closeEntry.ClosedChannelTxID
			test.test.Logf("confirmed closed payment %+v\n\n", payments.PaymentsList[0])
			return true
		}
		//return len(payments.PaymentsList) == 2 && payments.PaymentsList[0].ClosedChannelPoint != ""
		return false
	}, time.Second*10)
	if err != nil {
		t.Fatalf("failed to poll for closed channel %v", err)
	}

	// generate block to bypass timeout and enforce sweep tx.
	test.GenerateBlocks(10)
	walletClient := walletrpc.NewWalletKitClient(test.aliceNode)
	var pollErr error
	for i := 0; i < 10; i++ {
		test.test.Logf("generating block...")
		test.GenerateBlocks(1)
		pollErr = poll(func() bool {
			sweeps, err := walletClient.ListSweeps(context.Background(), &walletrpc.ListSweepsRequest{Verbose: true})
			if err != nil {
				t.Fatalf("failed to fetch wallet sweeps")
			}
			txDetails := sweeps.Sweeps.(*walletrpc.ListSweepsResponse_TransactionDetails)
			return len(txDetails.TransactionDetails.Transactions) == 2
		}, time.Second*6)
		if err != nil {
			continue
		}
	}
	if pollErr != nil {
		t.Fatalf("failed to poll wallet sweeps")
	}

	err = poll(func() bool {
		payments, err := test.aliceBreezClient.ListPayments(context.Background(), &data.ListPaymentsRequest{})
		if err != nil {
			t.Fatalf("failed to get list of payments %v", err)
		}
		closeEntry := payments.PaymentsList[0]
		if closeEntry.ClosedChannelTxID != "" && closeEntry.ClosedChannelSweepTxID != "" {
			test.test.Logf("sweep closed channel payment %+v\n\n", payments.PaymentsList[0])
			return true
		}
		return false
	}, time.Second*10)
	if err != nil {
		t.Fatalf("failed to wait for wallet sweep transaction")
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
