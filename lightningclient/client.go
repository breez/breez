package lightningclient

import (
	"io/ioutil"
	"net"
	"path/filepath"
	"time"

	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/macaroons"
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
func NewLightningClient(tlsDir, macaroonDir string) (lnrpc.LightningClient, error) {
	tlsCertPath := filepath.Join(tlsDir, defaultTLSCertFilename)
	creds, err := credentials.NewClientTLSFromFile(tlsCertPath, "")
	if err != nil {
		return nil, err
	}

	// Create a dial options array.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(maxMsgRecvSize),
	}

	macPath := filepath.Join(macaroonDir, defaultMacaroonFilename)
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

	conn, err := daemon.MemDial()
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return lnrpc.NewLightningClient(grpcCon), nil
}
