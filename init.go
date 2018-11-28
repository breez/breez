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
	"sync"
	"sync/atomic"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/breez/doubleratchet"
	"github.com/breez/breez/lightningclient"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/signal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/jessevdk/go-flags"
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
const (
	syncToChainDefaultPollingInterval = 3 * time.Second
	syncToChainFastPollingInterval    = 1 * time.Second
	waitBestBlockDuration             = 10 * time.Second
)

var (
	cfg                          *Config
	lightningClient              lnrpc.LightningClient
	breezClientConnection        *grpc.ClientConn
	breezClientConnectionFailure int32
	connectionMu                 sync.Mutex
	notificationsChan            = make(chan data.NotificationEvent)
	appWorkingDir                string
	isReady                      int32
	started                      int32
	quitChan                     chan struct{}
)

type Config struct {
	RoutingNodeHost   string `long:"routingnodehost"`
	RoutingNodePubKey string `long:"routingnodepubkey"`
	BreezServer       string `long:"breezserver"`
	Network           string `long:"network"`
}

func getBreezClientConnection() *grpc.ClientConn {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	breezClientConnection.ResetConnectBackoff()
	return breezClientConnection
}

func initBreezClientConnection() error {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(letsencryptCert)) {
		return fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	con, err := grpc.Dial(cfg.BreezServer, grpc.WithTransportCredentials(creds), grpc.WithKeepaliveParams(
		keepalive.ClientParameters{
			PermitWithoutStream: true,
			Time:                time.Second * 20,
		},
	))

	//trace connection changes
	go func() {
		for {
			currentState := con.GetState()
			log.Infof("getBreezClientConnection - connection state = %v", currentState)
			_ = con.WaitForStateChange(context.Background(), currentState)
		}
	}()
	breezClientConnection = con
	return err
}

/*
Start is responsible for starting the lightning client and some go routines to track and notify for account changes
*/
func Start(workingDir string, syncJobMode bool) (chan data.NotificationEvent, error) {
	if atomic.SwapInt32(&started, 1) == 1 {
		return nil, errors.New("Daemon already started")
	}
	quitChan = make(chan struct{})
	fmt.Println("Breez daemon started syncJobMode ", syncJobMode)
	appWorkingDir = workingDir
	if err := initConfig(); err != nil {
		fmt.Println("Warning initConfig", err)
		return nil, err
	}

	if err := openDB(path.Join(appWorkingDir, "breez.db")); err != nil {
		return nil, err
	}
	if err := doubleratchet.Start(path.Join(appWorkingDir, "sessions_encryption.db")); err != nil {
		return nil, err
	}
	go func() {
		defer closeDB()
		defer doubleratchet.Stop()
		defer atomic.StoreInt32(&started, 0)
		defer atomic.StoreInt32(&isReady, 0)
		err := initBreezClientConnection()
		if err != nil {
			fmt.Println("Error connecting to breez", err)
		}
		defer breezClientConnection.Close()
		if syncJobMode {
			startLightningDaemon(syncAndStop)
		} else {
			startLightningDaemon(startBreez)
		}
	}()

	return notificationsChan, nil
}

/*
Stop is responsible for stopping the ligtning daemon.
*/
func Stop() {
	stopLightningDaemon()
}

/*
WaitDaemonShutdown blocks untill daemon shutdown.
*/
func WaitDaemonShutdown() {
	if atomic.LoadInt32(&started) == 0 {
		return
	}
	select {
	case <-quitChan:
	}
}

/*
DaemonReady return the status of the lightningLib daemon
*/
func DaemonReady() bool {
	return atomic.LoadInt32(&isReady) == 1
}

/*
OnResume recalculate things we might missed when we were idle.
*/
func OnResume() {
	if atomic.LoadInt32(&started) == 1 {
		calculateAccountAndNotify()
	}
}

func startLightningDaemon(onReady func()) error {
	readyChan := make(chan interface{})
	go func() {
		<-readyChan
		atomic.StoreInt32(&isReady, 1)

		//initialize lightning client
		if err := initLightningClient(); err != nil {
			stopLightningDaemon()
			return
		}
		onReady()
	}()
	var err error
	err = daemon.LndMain([]string{"lightning-libs", "--lnddir", appWorkingDir, "--bitcoin." + cfg.Network}, readyChan)

	if err != nil {
		fmt.Println("Error starting breez", err)
		notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_LIGHTNING_SERVICE_DOWN}
		return err
	}
	close(quitChan)
	return nil
}

func stopLightningDaemon() {
	alive := signal.Alive()
	log.Infof("stopLightningDaemon called, stopping breez daemon alive=%v", alive)
	if alive {
		signal.RequestShutdown()
	}
}

//syncAndStop just wait for the chan to sync.
//It first waits a pre-defined interval for the best block to retrieve
//After that it just poll untill syncTocChain=true and then stop the daemon.
func syncAndStop() {
	//give it some time to get the best block and then sync.
	timeToWait := waitBestBlockDuration
	for {
		select {
		case <-time.After(timeToWait):
			//after first iteration switch to faster interval
			timeToWait = syncToChainFastPollingInterval
			chainInfo, chainErr := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
			if chainErr != nil {
				log.Errorf("failed to call GetInfo %v", chainErr)
				continue
			}
			log.Infof("Sync to chain interval Synced=%v BlockHeight=%v", chainInfo.SyncedToChain, chainInfo.BlockHeight)
			if chainInfo.SyncedToChain {
				stopLightningDaemon()
				return
			}
		case <-quitChan:
			return
		}
	}
}

func startBreez() {
	//start the go routings
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_READY}

	go watchRoutingNodeConnection()
	go watchPayments()
	go generateBlankInvoiceWithRetry()
	watchFundTransfers()
	go func() {
		onAccountChanged()
		err := syncToChain(syncToChainDefaultPollingInterval)
		if err != nil {
			log.Errorf("Failed to sync chain %v", err)
		}
		go connectOnStartup()
		go watchOnChainState()
	}()
}

func initConfig() error {
	c := &Config{}
	if err := flags.IniParse(path.Join(appWorkingDir, configFile), c); err != nil {
		return err
	}
	if len(c.RoutingNodeHost) == 0 || len(c.RoutingNodePubKey) == 0 {
		return errors.New("Breez must have routing node defined in the configuration file")
	}

	cfg = c
	return nil
}

// GetConfig returns the config object
func GetConfig() (*Config, error) {
	if cfg == nil {
		err := initConfig()
		if err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func initLightningClient() error {
	var clientError error
	macaroonDir := strings.Join([]string{appWorkingDir, "data", "chain", "bitcoin", cfg.Network}, "/")
	lightningClient, clientError = lightningclient.NewLightningClient(appWorkingDir, macaroonDir)
	if clientError != nil {
		log.Errorf("Error in creating client", clientError)
		notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_INITIALIZATION_FAILED}
		return clientError
	}
	return nil
}

func connectOnStartup() {
	channelPoints, err := getOpenChannelsPoints()
	if err != nil {
		log.Errorf("connectOnStartup: error in getOpenChannelsPoints", err)
		return
	}
	pendingChannels, err := lightningClient.PendingChannels(context.Background(), &lnrpc.PendingChannelsRequest{})
	if err != nil {
		log.Errorf("connectOnStartup: error in PendingChannels", err)
		return
	}
	if len(channelPoints) > 0 || len(pendingChannels.PendingOpenChannels) > 0 {
		log.Infof("connectOnStartup: already has a channel, ignoring manual connection")
		return
	}

	connectRoutingNode()
}
