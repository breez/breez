package tests

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/breez/breez/data"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/macaroon.v2"
)

var (
	// alice
	aliceBreezAddress = os.Getenv("ALICE_BREEZ_ADDRESS") // "127.0.0.1:50053"
	aliceDir          = os.Getenv("ALICE_DIR")           // "/Users/roeierez/gopath4/src/github.com/breez/breez/docker/test/alice_node"
	aliceAddress      = os.Getenv("ALICE_LND_ADDRESS")   //"127.0.0.1:10009"

	// bob
	bobBreezAddress = os.Getenv("BOB_BREEZ_ADDRESS") // "127.0.0.1:50054"
	bobDir          = os.Getenv("BOB_DIR")           //"/Users/roeierez/gopath4/src/github.com/breez/breez/docker/test/bob_node"
	bobAddress      = os.Getenv("BOB_LND_ADDRESS")   // "127.0.0.1:10011"

	// breez
	breezDir     = os.Getenv("BREEZ_DIR")         //"/Users/roeierez/gopath4/src/github.com/breez/breez/docker/test/bob_node"
	breezAddress = os.Getenv("BREEZ_LND_ADDRESS") // "127.0.0.1:10011"

	// subswapper
	subswapDir     = os.Getenv("SUBSWAP_DIR")         //"/Users/roeierez/gopath4/src/github.com/breez/breez/docker/test/bob_node"
	subswapAddress = os.Getenv("SUBSWAP_LND_ADDRESS") // "127.0.0.1:10012"

	// btcd
	btcdHost     = os.Getenv("BTCD_HOST")      //"127.0.0.1:18556"
	btcdCertFile = os.Getenv("BTCD_CERT_FILE") //"/Users/roeierez/gopath4/src/github.com/breez/breez/docker/btcd-rpc.cert"
)

type framework struct {
	test             *testing.T
	miner            *rpcclient.Client
	aliceBreezClient data.BreezAPIClient
	bobBreezClient   data.BreezAPIClient
	aliceNode        *grpc.ClientConn
	bobNode          *grpc.ClientConn
	breezNode        *grpc.ClientConn
	subswapNode      *grpc.ClientConn
}

func setup() error {
	fmt.Println("setup started")
	miner, err := getMiner()
	if err != nil {
		return err
	}
	_, _ = miner.Generate(1)
	if _, err := os.Create(fmt.Sprintf("%v/shutdown", aliceDir)); err != nil {
		return err
	}
	os.Create(fmt.Sprintf("%v/shutdown", bobDir))
	time.Sleep(time.Second * 2)
	if err := waitForNodeSynced(aliceDir, aliceAddress); err != nil {
		return err
	}
	if err := waitForNodeSynced(bobDir, bobAddress); err != nil {
		return err
	}
	fmt.Println("setup completed")
	return nil
}

func poll(pred func() bool, timeout time.Duration) error {
	const pollInterval = 20 * time.Millisecond

	exitTimer := time.After(timeout)
	for {
		<-time.After(pollInterval)

		select {
		case <-exitTimer:
			return fmt.Errorf("predicate not satisfied after time out")
		default:
		}

		if pred() {
			return nil
		}
	}
}

func waitForNodeSynced(dir, address string) error {
	fmt.Println("waiting for node to sync")
	for i := 0; i < 10; i++ {
		node, err := newLightningConnection(dir, address)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		nodeClient := lnrpc.NewLightningClient(node)

		info, err := nodeClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if err == nil && info.SyncedToChain {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("Timeout in waiting for node to sync")
}

func waitSynced(nodeClient lnrpc.LightningClient, bestBlock uint32) error {
	var lastBlock uint32
	for i := 0; i < 10; i++ {
		info, err := nodeClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if err == nil && info.SyncedToChain && info.BlockHeight == bestBlock {
			return nil
		}
		lastBlock = info.BlockHeight
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Timeout in waiting for node to sync to best block %v only have %v", bestBlock, lastBlock)
}

func newTestFramework(test *testing.T) *framework {
	if err := setup(); err != nil {
		test.Fatalf("failed to setup test %v", err)
	}
	// miner
	miner, err := getMiner()
	if err != nil {
		test.Fatalf("failed to create miner node %w", err)
	}

	//alice bree client
	aliceBreezClient, err := getBreezClient(aliceBreezAddress)
	if err != nil {
		test.Fatalf("failed to create alice breez client %w", err)
	}

	// alice lnd grpc
	aliceNode, err := newLightningConnection(aliceDir, aliceAddress)
	if err != nil {
		test.Fatalf("failed to connect to alice node %w", err)
	}

	//bob breez client
	bobBreezClient, err := getBreezClient(bobBreezAddress)
	if err != nil {
		test.Fatalf("failed to create alice breez client %w", err)
	}

	// bob lnd grpc
	bobNode, err := newLightningConnection(bobDir, bobAddress)
	if err != nil {
		test.Fatalf("failed to connect to bob node %w", err)
	}

	// breez lnd grpc
	breezNode, err := newLightningConnection(breezDir, breezAddress)
	if err != nil {
		test.Fatalf("failed to connect to bob node %w", err)
	}

	// subswap lnd grpc
	subswapNode, err := newLightningConnection(subswapDir, subswapAddress)
	if err != nil {
		test.Fatalf("failed to connect to subswap node %w", err)
	}

	return &framework{
		test:             test,
		miner:            miner,
		aliceBreezClient: aliceBreezClient,
		bobBreezClient:   bobBreezClient,
		aliceNode:        aliceNode,
		bobNode:          bobNode,
		breezNode:        breezNode,
		subswapNode:      subswapNode,
	}
}

func (f *framework) GenerateBlocks(num uint32) {
	info, err := f.miner.GetBlockChainInfo()
	if err != nil {
		f.test.Fatalf("failed to get miner info ")
	}
	bestBlock := uint32(info.Blocks) + num
	if _, err := f.miner.Generate(num); err != nil {
		f.test.Fatalf("failed to generate blocks")
	}
	time.Sleep(time.Second)
	for _, n := range []*grpc.ClientConn{f.aliceNode, f.bobNode, f.breezNode, f.subswapNode} {
		nodeClient := lnrpc.NewLightningClient(n)
		if err := waitSynced(nodeClient, bestBlock); err != nil {
			f.test.Fatalf("failed to wait for nodes to sync %v %v", bestBlock, err)
		}
		f.test.Logf("node synced")
	}
}

func (f *framework) initSwapperNode() {
	t := f.test
	subswapNode := lnrpc.NewLightningClient(f.subswapNode)
	breezClient := lnrpc.NewLightningClient(f.breezNode)
	swapChannels, err := subswapNode.ListChannels(context.Background(), &lnrpc.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("failed to get sub swapper channels %v", err)
	}
	if len(swapChannels.Channels) > 0 {
		return
	}

	list, err := f.aliceBreezClient.GetLSPList(context.Background(), &data.LSPListRequest{})
	if err != nil {
		t.Fatalf("failed to get lsp list %w", err)
	}
	lsp := list.Lsps["lspd-secret"]

	// breezInfo, err := breezClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	// if err != nil {
	// 	t.Fatalf("failed to get breez node info %v", err)
	// }
	subswapAddr, err := subswapNode.NewAddress(context.Background(),
		&lnrpc.NewAddressRequest{Type: lnrpc.AddressType_NESTED_PUBKEY_HASH})
	if err != nil {
		t.Fatalf("failed to get address from subswapper %w", err)
	}
	_, err = breezClient.SendCoins(context.Background(),
		&lnrpc.SendCoinsRequest{Addr: subswapAddr.Address, Amount: 10000000})
	if err != nil {
		t.Fatalf("failed to send coins to local client %w", err)
	}
	f.GenerateBlocks(10)

	// swapPeers, _ := subswapNode.ListPeers(context.Background(), &lnrpc.ListPeersRequest{})

	// if len(swapPeers.Peers) == 0 {
	_, err = subswapNode.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: lsp.Pubkey,
			Host:   lsp.Host,
		},
	})
	if err != nil {
		t.Fatalf("failed to connect to breez from lsp %w", err)
	}
	//}
	_, err = subswapNode.OpenChannelSync(context.Background(), &lnrpc.OpenChannelRequest{
		NodePubkeyString:   lsp.Pubkey,
		LocalFundingAmount: 1000000,
		TargetConf:         1,
	})
	if err != nil {
		t.Fatalf("failed to open channel to breez from lsp %w", err)
	}

}

func getBreezClient(address string) (data.BreezAPIClient, error) {
	con, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	return data.NewBreezAPIClient(con), nil
}

func getMiner() (*rpcclient.Client, error) {
	certFile, err := os.Open(btcdCertFile)
	if err != nil {
		return nil, err
	}
	rpcCert, err := ioutil.ReadAll(certFile)
	if err != nil {
		return nil, err
	}
	if err := certFile.Close(); err != nil {
		return nil, err
	}

	rpcConfig := &rpcclient.ConnConfig{
		Host:                 btcdHost,
		Endpoint:             "ws",
		User:                 "devuser",
		Pass:                 "devpass",
		Certificates:         rpcCert,
		DisableTLS:           false,
		DisableConnectOnNew:  true,
		DisableAutoReconnect: false,
	}

	ntfnCallbacks := &rpcclient.NotificationHandlers{
		OnBlockConnected:    func(hash *chainhash.Hash, height int32, t time.Time) {},
		OnBlockDisconnected: func(hash *chainhash.Hash, height int32, t time.Time) {},
		OnRedeemingTx:       func(transaction *btcutil.Tx, details *btcjson.BlockDetails) {},
	}

	client, err := rpcclient.New(rpcConfig, ntfnCallbacks)
	if err != nil {
		return nil, err
	}
	if err = client.Connect(1); err != nil {
		return nil, err
	}
	return client, nil
}

func newLightningConnection(lndDir, address string) (*grpc.ClientConn, error) {
	macaroonDir := strings.Join([]string{lndDir, "data", "chain", "bitcoin", "simnet"}, "/")
	tlsCertPath := filepath.Join(lndDir, "tls.cert")
	creds, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, err
	}

	// Create a dial options array.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.MaxRetryRPCBufferSize(1024 * 1024 * 500)),
	}

	macPath := filepath.Join(macaroonDir, "admin.macaroon")
	macBytes, err := ioutil.ReadFile(macPath)
	if err != nil {
		return nil, err
	}
	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macBytes); err != nil {
		return nil, err
	}

	// Now we append the macaroon credentials to the dial options.
	cred := macaroons.NewMacaroonCredential(mac)
	opts = append(opts, grpc.WithPerRPCCredentials(cred))

	// We need to use a custom dialer so we can also connect to unix sockets
	// and not just TCP addresses.
	grpcCon, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, err
	}
	ensureNodeLive(grpcCon)
	return grpcCon, nil
}

func ensureNodeLive(con *grpc.ClientConn) {
	rpc := lnrpc.NewLightningClient(con)
	for {
		_, err := rpc.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
		if err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}
