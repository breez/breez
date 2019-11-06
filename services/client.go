package services

import (
	"context"
	"crypto/x509"
	"fmt"
	"sync/atomic"
	"time"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/data"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// Start the client
func (c *Client) Start() error {
	if atomic.SwapInt32(&c.started, 1) == 1 {
		return nil
	}
	con, err := dial(c.cfg.BreezServer)
	c.connection = con
	return err
}

func (c *Client) Stop() (err error) {
	if atomic.SwapInt32(&c.stopped, 1) == 1 {
		return nil
	}
	c.Lock()
	defer c.Unlock()
	if c.connection != nil {
		err = c.connection.Close()
	}
	c.log.Infof("BreezServicesClient shutdown succesfully")
	return
}

//NewFundManager creates a new FundsManager
func (c *Client) NewFundManager() (breezservice.FundManagerClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewFundManager - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewFundManagerClient(con), ctx, cancel
}

//NewSyncNotifierClient creates a new SyncNotifierClient
func (c *Client) NewSyncNotifierClient() (breezservice.SyncNotifierClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewSyncNotifierClient - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewSyncNotifierClient(con), ctx, cancel
}

//NewSyncNotifierClient creates a new SyncNotifierClient
func (c *Client) NewChannelOpenerClient() (breezservice.ChannelOpenerClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewSyncNotifierClient - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(
		metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+c.cfg.LspToken),
		15 * time.Second,
	)
	return breezservice.NewChannelOpenerClient(con), ctx, cancel
}

func (c *Client) getBreezClientConnection() *grpc.ClientConn {
	c.log.Infof("getBreezClientConnection - before Ping;")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(c.connection)
	_, err := ic.Ping(ctx, &breezservice.PingRequest{})
	c.log.Infof("getBreezClientConnection - after Ping; err: %v", err)
	if grpc.Code(err) == codes.DeadlineExceeded {
		c.Lock()
		defer c.Unlock()
		c.connection.Close()
		c.connection, err = dial(c.cfg.BreezServer)
		c.log.Infof("getBreezClientConnection - new connection; err: %v", err)
	}
	return c.connection
}

//Rates returns the rates obtained from the server
func (c *Client) Rates() (*data.Rates, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(c.connection)
	rates, err := ic.Rates(ctx, &breezservice.RatesRequest{})
	if err != nil {
		return nil, err
	}
	r := make([]*data.Rate, 0, len(rates.Rates))
	for _, rate := range rates.Rates {
		r = append(r, &data.Rate{Coin: rate.Coin, Value: rate.Value})
	}
	return &data.Rates{Rates: r}, nil
}

//LSPList returns the list of the LSPs
func (c *Client) LSPList() (*data.LSPList, error) {
	ctx, cancel := context.WithTimeout(
		metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+c.cfg.LspToken),
		endpointTimeout*time.Second,
	)
	defer cancel()
	ic := breezservice.NewChannelOpenerClient(c.connection)
	lsps, err := ic.LSPList(ctx, &breezservice.LSPListRequest{})
	if err != nil {
		return nil, err
	}
	r := make(map[string]*data.LSPInformation)
	for id, l := range lsps.Lsps {
		r[id] = &data.LSPInformation{
			Name:            l.Name,
			WidgetUrl:       l.WidgetUrl,
			Pubkey:          l.Pubkey,
			Host:            l.Host,
			ChannelCapacity: l.ChannelCapacity,
			TargetConf:      l.TargetConf,
			BaseFeeMsat:     l.BaseFeeMsat,
			FeeRate:         l.FeeRate,
			TimeLockDelta:   l.TimeLockDelta,
			MinHtlcMsat:     l.MinHtlcMsat,
		}
	}
	return &data.LSPList{Lsps: r}, nil
}

func dial(serverURL string) (*grpc.ClientConn, error) {
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(letsencryptCert)) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return grpc.Dial(serverURL, dialOptions...)
}
