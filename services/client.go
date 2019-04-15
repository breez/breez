package services

import (
	"context"
	"crypto/x509"
	"fmt"
	"sync/atomic"
	"time"

	breezservice "github.com/breez/breez/breez"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
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

func dial(serverURL string) (*grpc.ClientConn, error) {
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM([]byte(letsencryptCert)) {
		return nil, fmt.Errorf("credentials: failed to append certificates")
	}
	creds := credentials.NewClientTLSFromCert(cp, "")
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return grpc.Dial(serverURL, dialOptions...)
}
