package lnnode

import (
	"io/ioutil"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/breez/breez/config"
	"github.com/lightningnetwork/lnd"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/backuprpc"
	"github.com/lightningnetwork/lnd/lnrpc/breezbackuprpc"
	"github.com/lightningnetwork/lnd/lnrpc/chainrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
	"github.com/lightningnetwork/lnd/lnrpc/submarineswaprpc"
	"github.com/lightningnetwork/lnd/lnrpc/walletrpc"
	"github.com/lightningnetwork/lnd/macaroons"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	macaroon "gopkg.in/macaroon.v2"
)

const (
	defaultTLSCertFilename  = "tls.cert"
	defaultMacaroonFilename = "admin.macaroon"
)

var (
	// maxMsgRecvSize is the largest message our client will receive. We
	// set this to ~50Mb atm.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(1 * 1024 * 1024 * 50)
)

// NewLightningClient returns an instance of lnrpc.LightningClient
func newLightningClient(cfg *config.Config) (
	lnrpc.LightningClient, backuprpc.BackupClient,
	submarineswaprpc.SubmarineSwapperClient,
	breezbackuprpc.BreezBackuperClient,
	routerrpc.RouterClient,
	walletrpc.WalletKitClient,
	chainrpc.ChainNotifierClient,
	signrpc.SignerClient,
	error) {

	appWorkingDir := cfg.WorkingDir
	network := cfg.Network
	macaroonDir := strings.Join([]string{appWorkingDir, "data", "chain", "bitcoin", network}, "/")
	tlsCertPath := filepath.Join(appWorkingDir, defaultTLSCertFilename)
	creds, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}

	// Create a dial options array.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(maxMsgRecvSize),
	}

	macPath := filepath.Join(macaroonDir, defaultMacaroonFilename)
	macBytes, err := ioutil.ReadFile(macPath)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	mac := &macaroon.Macaroon{}
	if err = mac.UnmarshalBinary(macBytes); err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}

	// Now we append the macaroon credentials to the dial options.
	cred := macaroons.NewMacaroonCredential(mac)
	opts = append(opts, grpc.WithPerRPCCredentials(cred))

	conn, err := lnd.MemDial()
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}

	// We need to use a custom dialer so we can also connect to unix sockets
	// and not just TCP addresses.
	opts = append(
		opts, grpc.WithDialer(func(target string,
			timeout time.Duration) (net.Conn, error) {
			return conn, nil
		}),
	)
	grpcCon, err := grpc.Dial("localhost", opts...)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}

	return lnrpc.NewLightningClient(grpcCon),
		backuprpc.NewBackupClient(grpcCon),
		submarineswaprpc.NewSubmarineSwapperClient(grpcCon),
		breezbackuprpc.NewBreezBackuperClient(grpcCon),
		routerrpc.NewRouterClient(grpcCon),
		walletrpc.NewWalletKitClient(grpcCon),
		chainrpc.NewChainNotifierClient(grpcCon),
		signrpc.NewSignerClient(grpcCon),
		nil
}
