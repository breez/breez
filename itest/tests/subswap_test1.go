package tests

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
