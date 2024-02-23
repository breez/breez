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
	return nil
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
	c.log.Infof("BreezServicesClient shutdown successfully")
	return
}

// NewFundManager creates a new FundsManager
func (c *Client) NewFundManager() (breezservice.FundManagerClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewFundManager - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewFundManagerClient(con), ctx, cancel
}

// NewSwapper creates a new Swapper
func (c *Client) NewSwapper(timeout time.Duration) (breezservice.SwapperClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewSwapper - connection state = %v", con.GetState())
	swapperTimeout := timeout
	if timeout == 0 {
		swapperTimeout = endpointTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), swapperTimeout)
	return breezservice.NewSwapperClient(con), ctx, cancel
}

// NewSyncNotifierClient creates a new SyncNotifierClient
func (c *Client) NewSyncNotifierClient() (breezservice.SyncNotifierClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewSyncNotifierClient - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewSyncNotifierClient(con), ctx, cancel
}

// NewChannelOpenerClient creates a new SyncNotifierClient
func (c *Client) NewChannelOpenerClient() (breezservice.ChannelOpenerClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewSyncNotifierClient - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(
		metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+c.cfg.LspToken),
		15*time.Second,
	)
	return breezservice.NewChannelOpenerClient(con), ctx, cancel
}

// NewPushTxNotifierClient creates a new PushTxNotifierClient
func (c *Client) NewPushTxNotifierClient() (breezservice.PushTxNotifierClient, context.Context, context.CancelFunc) {
	con := c.getBreezClientConnection()
	c.log.Infof("NewPushTxNotifierClient - connection state = %v", con.GetState())
	ctx, cancel := context.WithTimeout(context.Background(), endpointTimeout*time.Second)
	return breezservice.NewPushTxNotifierClient(con), ctx, cancel
}

func (c *Client) getBreezClientConnection() *grpc.ClientConn {
	c.log.Infof("getBreezClientConnection - before Ping;")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	con := c.ensureConnection(false)
	ic := breezservice.NewInformationClient(con)
	_, err := ic.Ping(ctx, &breezservice.PingRequest{})
	c.log.Infof("getBreezClientConnection - after Ping; err: %v", err)
	if grpc.Code(err) == codes.DeadlineExceeded {
		con = c.ensureConnection(true)
		c.log.Infof("getBreezClientConnection - new connection; err: %v", err)
	}
	return con
}

func (c *Client) ensureConnection(closeOldConnection bool) *grpc.ClientConn {
	c.Lock()
	defer c.Unlock()
	if closeOldConnection && c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}
	if c.connection == nil {
		con, err := dial(c.cfg.BreezServer, c.cfg.BreezServerNoTLS)
		if err != nil {
			c.log.Errorf("failed to dial to grpc connection: %v", err)
		}
		c.connection = con
	}
	return c.connection
}

// Versions returns the list of Breez app version authorized by the server
func (c *Client) Versions() ([]string, error) {
	con := c.getBreezClientConnection()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(con)
	r, err := ic.BreezAppVersions(ctx, &breezservice.BreezAppVersionsRequest{})
	if err != nil {
		return []string{}, err
	}
	return r.Version, nil
}

// Rates returns the rates obtained from the server
func (c *Client) Rates() (*data.Rates, error) {
	con := c.getBreezClientConnection()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(con)
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

func (c *Client) ReceiverNode() (string, error) {
	con := c.getBreezClientConnection()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ic := breezservice.NewInformationClient(con)
	receiverInfo, err := ic.ReceiverInfo(ctx, &breezservice.ReceiverInfoRequest{})
	if err != nil {
		return "", err
	}
	return receiverInfo.Pubkey, nil
}

// LSPList returns the list of the LSPs
func (c *Client) LSPList() (*data.LSPList, error) {
	con := c.getBreezClientConnection()
	ctx, cancel := context.WithTimeout(
		metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+c.cfg.LspToken),
		endpointTimeout*time.Second,
	)
	defer cancel()
	ic := breezservice.NewChannelOpenerClient(con)
	lsps, err := ic.LSPList(ctx, &breezservice.LSPListRequest{})
	if err != nil {
		return nil, err
	}
	r := make(map[string]*data.LSPInformation)
	for id, l := range lsps.Lsps {
		var menu []*data.OpeningFeeParams
		var lastmin uint64
		var lastproportional uint32
		for _, params := range l.OpeningFeeParamsMenu {
			if params.MinMsat < lastmin || params.Proportional < lastproportional {
				return nil, fmt.Errorf("received invalid lsp response with fees not strictly increasing")
			}

			menu = append(menu, &data.OpeningFeeParams{
				MinMsat:              params.MinMsat,
				Proportional:         params.Proportional,
				ValidUntil:           params.ValidUntil,
				MaxIdleTime:          params.MaxIdleTime,
				MaxClientToSelfDelay: params.MaxClientToSelfDelay,
				Promise:              params.Promise,
				MinPaymentSizeMsat:   params.MinPaymentSizeMsat,
				MaxPaymentSizeMsat:   params.MaxPaymentSizeMsat,
			})

			lastmin = params.MinMsat
			lastproportional = params.Proportional
		}

		lspInfo := &data.LSPInformation{
			Id:                    id,
			Name:                  l.Name,
			WidgetUrl:             l.WidgetUrl,
			Pubkey:                l.Pubkey,
			Host:                  l.Host,
			ChannelCapacity:       l.ChannelCapacity,
			TargetConf:            l.TargetConf,
			BaseFeeMsat:           l.BaseFeeMsat,
			FeeRate:               l.FeeRate,
			TimeLockDelta:         l.TimeLockDelta,
			MinHtlcMsat:           l.MinHtlcMsat,
			ChannelFeePermyriad:   l.ChannelFeePermyriad,
			ChannelMinimumFeeMsat: l.ChannelMinimumFeeMsat,
			LspPubkey:             l.LspPubkey,
			MaxInactiveDuration:   l.MaxInactiveDuration,
		}

		cheapestParams := c.getCheapestOpeningFeeParams(menu)
		longestParams, err := c.getLongestValidOpeningFeeParams(menu)
		if err != nil {
			return nil, fmt.Errorf("received invalid lsp response: %w", err)
		}
		// NOTE: This could be nil, if there are no params in the response.
		lspInfo.CheapestOpeningFeeParams = cheapestParams
		lspInfo.LongestValidOpeningFeeParams = longestParams
		r[id] = lspInfo
	}

	return &data.LSPList{Lsps: r}, nil
}

func (c *Client) getCheapestOpeningFeeParams(menu []*data.OpeningFeeParams) *data.OpeningFeeParams {
	if len(menu) == 0 {
		c.log.Info("No channel opening params are available in lsp info")
		return nil
	}

	return menu[0]
}

func (c *Client) getLongestValidOpeningFeeParams(menu []*data.OpeningFeeParams) (*data.OpeningFeeParams, error) {
	if len(menu) == 0 {
		c.log.Info("No channel opening params are available in lsp info")
		return nil, nil
	}

	// Take the longest validity.
	var params *data.OpeningFeeParams
	var validUntil time.Time
	for _, p := range menu {
		v, err := time.Parse("2006-01-02T15:04:05.999Z", p.ValidUntil)
		if err != nil {
			return nil, fmt.Errorf("LSPInformation OpeningFeeParams got invalid time format: %v", p.ValidUntil)
		}

		if params == nil {
			params = p
			validUntil = v
			continue
		}

		if v.After(validUntil) {
			params = p
			validUntil = v
		}
	}

	return params, nil
}

func dial(serverURL string, noTLS bool) (*grpc.ClientConn, error) {
	if noTLS {
		return grpc.Dial(serverURL, grpc.WithInsecure())
	}
	systemCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("Error getting SystemCertPool: %w", err)
	}
	creds := credentials.NewClientTLSFromCert(systemCertPool, "")
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return grpc.Dial(serverURL, dialOptions...)
}
