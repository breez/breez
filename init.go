//Package breez generate protobuf command
//protoc -I data data/messages.proto --go_out=plugins=grpc:data
package breez

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/lightningclient"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	flags "github.com/jessevdk/go-flags"
)

const (
	configFile      = "breez.conf"
	letsencryptCert = `-----BEGIN CERTIFICATE-----
MIIEkjCCA3qgAwIBAgIQCgFBQgAAAVOFc2oLheynCDANBgkqhkiG9w0BAQsFADA/
MSQwIgYDVQQKExtEaWdpdGFsIFNpZ25hdHVyZSBUcnVzdCBDby4xFzAVBgNVBAMT
DkRTVCBSb290IENBIFgzMB4XDTE2MDMxNzE2NDA0NloXDTIxMDMxNzE2NDA0Nlow
SjELMAkGA1UEBhMCVVMxFjAUBgNVBAoTDUxldCdzIEVuY3J5cHQxIzAhBgNVBAMT
GkxldCdzIEVuY3J5cHQgQXV0aG9yaXR5IFgzMIIBIjANBgkqhkiG9w0BAQEFAAOC
AQ8AMIIBCgKCAQEAnNMM8FrlLke3cl03g7NoYzDq1zUmGSXhvb418XCSL7e4S0EF
q6meNQhY7LEqxGiHC6PjdeTm86dicbp5gWAf15Gan/PQeGdxyGkOlZHP/uaZ6WA8
SMx+yk13EiSdRxta67nsHjcAHJyse6cF6s5K671B5TaYucv9bTyWaN8jKkKQDIZ0
Z8h/pZq4UmEUEz9l6YKHy9v6Dlb2honzhT+Xhq+w3Brvaw2VFn3EK6BlspkENnWA
a6xK8xuQSXgvopZPKiAlKQTGdMDQMc2PMTiVFrqoM7hD8bEfwzB/onkxEz0tNvjj
/PIzark5McWvxI0NHWQWM6r6hCm21AvA2H3DkwIDAQABo4IBfTCCAXkwEgYDVR0T
AQH/BAgwBgEB/wIBADAOBgNVHQ8BAf8EBAMCAYYwfwYIKwYBBQUHAQEEczBxMDIG
CCsGAQUFBzABhiZodHRwOi8vaXNyZy50cnVzdGlkLm9jc3AuaWRlbnRydXN0LmNv
bTA7BggrBgEFBQcwAoYvaHR0cDovL2FwcHMuaWRlbnRydXN0LmNvbS9yb290cy9k
c3Ryb290Y2F4My5wN2MwHwYDVR0jBBgwFoAUxKexpHsscfrb4UuQdf/EFWCFiRAw
VAYDVR0gBE0wSzAIBgZngQwBAgEwPwYLKwYBBAGC3xMBAQEwMDAuBggrBgEFBQcC
ARYiaHR0cDovL2Nwcy5yb290LXgxLmxldHNlbmNyeXB0Lm9yZzA8BgNVHR8ENTAz
MDGgL6AthitodHRwOi8vY3JsLmlkZW50cnVzdC5jb20vRFNUUk9PVENBWDNDUkwu
Y3JsMB0GA1UdDgQWBBSoSmpjBH3duubRObemRWXv86jsoTANBgkqhkiG9w0BAQsF
AAOCAQEA3TPXEfNjWDjdGBX7CVW+dla5cEilaUcne8IkCJLxWh9KEik3JHRRHGJo
uM2VcGfl96S8TihRzZvoroed6ti6WqEBmtzw3Wodatg+VyOeph4EYpr/1wXKtx8/
wApIvJSwtmVi4MFU5aMqrSDE6ea73Mj2tcMyo5jMd6jmeWUHK8so/joWUoHOUgwu
X4Po1QYz+3dszkDqMp4fklxBwXRsW10KXzPMTZ+sOPAveyxindmjkW8lGy+QsRlG
PfZ+G6Z6h7mjem0Y+iWlkYcV4PIWL1iwBi8saCbGS5jN2p8M+X+Q7UNKEkROb3N6
KOqkqm57TH2H3eDJAkSnh6/DNFu0Qg==
-----END CERTIFICATE-----`
)

var (
	cfg                   *config
	lightningClient       lnrpc.LightningClient
	breezClientConnection *grpc.ClientConn
	notificationsChan     = make(chan data.NotificationEvent)
	appWorkingDir         string
)

type config struct {
	RoutingNodeHost   string `long:"routingnodehost"`
	RoutingNodePubKey string `long:"routingnodepubkey"`
	BreezServer       string `long:"breezserver"`
	Network           string `long:"network"`
}

func getBreezClientConnection(breezServer string) (*grpc.ClientConn, error) {
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(letsencryptCert)) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	return grpc.Dial(breezServer, grpc.WithTransportCredentials(creds))
}

/*
Start is responsible for starting the lightning client and some go routines to track and notify for account changes
*/
func Start(workingDir string) (chan data.NotificationEvent, error) {
	appWorkingDir = workingDir
	if err := initConfig(); err != nil {
		fmt.Println("Warning initConfig", err)
		return nil, err
	}
	readyChan := make(chan interface{})
	go func() {
		<-readyChan
		onReady()
	}()

	openDB(path.Join(appWorkingDir, "breez.db"))
	go func() {
		defer closeDB()
		var err error
		breezClientConnection, err = getBreezClientConnection(cfg.BreezServer)
		if err != nil {
			fmt.Println("Error connecting to breez", err)
		}
		defer breezClientConnection.Close()
		for err = daemon.LndMain([]string{"lightning-libs", "--lnddir", appWorkingDir, "--bitcoin." + cfg.Network}, readyChan); err != nil; {
			fmt.Println("Error starting breez", err)
			time.Sleep(1 * time.Second)
		}
	}()

	return notificationsChan, nil
}

func onReady() {
	var clientError error
	macaroonDir := strings.Join([]string{appWorkingDir, "data", "chain", "bitcoin", cfg.Network}, "/")
	lightningClient, clientError = lightningclient.NewLightningClient(appWorkingDir, macaroonDir)
	if clientError != nil {
		log.Errorf("Error in creating client", clientError)
		notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_INITIALIZATION_FAILED}
		return
	}
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_READY}

	go func() {
		go watchRoutingNodeConnection()
		go watchPayments()
		go generateBlankInvoiceWithRetry()
		syncToChain()
		connectRoutingNode()
		go watchOnChainState()
	}()
}

func initConfig() error {
	cfg = &config{}
	if err := flags.IniParse(path.Join(appWorkingDir, configFile), cfg); err != nil {
		return err
	}
	if len(cfg.RoutingNodeHost) == 0 || len(cfg.RoutingNodePubKey) == 0 {
		return errors.New("Breez must have routing node defined in the configuration file")
	}
	return nil
}

func connectRoutingNode() error {
	log.Infof("Connecting to routing node host: %v, pubKey: %v", cfg.RoutingNodeHost, cfg.RoutingNodePubKey)
	_, err := lightningClient.ConnectPeer(context.Background(), &lnrpc.ConnectPeerRequest{
		Addr: &lnrpc.LightningAddress{
			Pubkey: cfg.RoutingNodePubKey,
			Host:   cfg.RoutingNodeHost,
		},
		Perm: true,
	})
	return err
}
