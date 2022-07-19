package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

func (a *Service) generateInvoiceWithNewAmount(payReq string, newAmount int64) (string, []byte, error) {
	invoice, err := zpay32.Decode(payReq, a.activeParams)
	if err != nil {
		return "", nil, fmt.Errorf("zpay32.Decode() error: %w", err)
	}

	signerClient := a.daemonAPI.SignerClient()
	if signerClient == nil {
		return "", nil, fmt.Errorf("API is not ready")
	}

	m := lnwire.MilliSatoshi(newAmount)
	invoice.MilliSat = &m
	signer := zpay32.MessageSigner{SignCompact: func(msg []byte) ([]byte, error) {
		kl := signrpc.KeyLocator{
			KeyFamily: int32(keychain.KeyFamilyNodeKey),
			KeyIndex:  0,
		}

		r, err := signerClient.SignMessage(context.Background(),
			&signrpc.SignMessageReq{
				Msg:        msg,
				KeyLoc:     &kl,
				CompactSig: true,
				DoubleHash: true,
			})
		if err != nil {
			return nil, fmt.Errorf("m.client.SignMessage() error: %w", err)
		}
		return r.Signature, nil
	}}
	newInvoice, err := invoice.Encode(signer)
	if err != nil {
		log.Printf("invoice.Encode() error: %v", err)
	}
	return newInvoice, (*invoice.PaymentAddr)[:], err
}

func (a *Service) trackZeroConfInvoice() error {

	invoiceHashes, err := a.breezDB.FetchZeroConfHashes()
	if err != nil {
		return fmt.Errorf("trackZeroConfInvoice: failed to fetch zero conf hashes %w", err)
	}

	for _, invoiceHash := range invoiceHashes {
		a.trackInvoice(invoiceHash)
	}
	return nil
}

func (a *Service) trackInvoice(invoiceHash []byte) error {
	a.log.Infof("subscribing zero-conf invoice %x", invoiceHash)
	invoicesClient := a.daemonAPI.InvoicesClient()
	if invoicesClient == nil {
		return errors.New("trackZeroConfInvoice: api not ready")
	}

	stream, err := invoicesClient.SubscribeSingleInvoice(context.Background(), &invoicesrpc.SubscribeSingleInvoiceRequest{
		RHash: invoiceHash,
	})
	if err != nil {
		return fmt.Errorf("trackZeroConfInvoice: failed to subscribe zero-conf invoice %w", err)
	}

	go func() {
		for {
			invoice, err := stream.Recv()
			if err != nil {
				a.log.Errorf("trackZeroConfInvoice: failed to receive an invoice : %v", err)
				return
			}
			a.log.Infof("trackZeroConfInvoice: invoice received %v", invoice.State)
			if invoice.State == lnrpc.Invoice_ACCEPTED {
				a.log.Infof("trackZeroConfInvoice: invoice accepted")
				allChannelsTrusted := true
				edgeByChannelID := make(map[uint64]*lnrpc.ChannelEdge)
				for _, htlc := range invoice.Htlcs {
					if lnwire.NewShortChanIDFromInt(htlc.ChanId).IsFake() {
						channelTrusted := false
						edge, ok := edgeByChannelID[htlc.ChanId]
						if !ok {
							edge, err = a.daemonAPI.APIClient().GetChanInfo(context.Background(), &lnrpc.ChanInfoRequest{
								ChanId: htlc.ChanId,
							})
							if err != nil {
								a.log.Warnf("failed to fetch htlc channel with id: %v", htlc.ChanId)
								continue
							}
							edgeByChannelID[htlc.ChanId] = edge
						}

						for _, hint := range invoice.RouteHints {
							for _, hop := range hint.HopHints {
								if hop.NodeId == edge.Node1Pub || hop.NodeId == edge.Node2Pub {
									channelTrusted = true
									break
								}
							}
						}
						allChannelsTrusted = allChannelsTrusted && channelTrusted
					}
				}
				if allChannelsTrusted {
					a.log.Infof("settlling invoice %x", invoice.RHash)
					invoicesClient.SettleInvoice(context.Background(), &invoicesrpc.SettleInvoiceMsg{
						Preimage: invoice.RPreimage,
					})
				} else {
					a.log.Infof("cancelling invoice %x", invoice.RHash)
					invoicesClient.CancelInvoice(context.Background(), &invoicesrpc.CancelInvoiceMsg{
						PaymentHash: invoice.RHash,
					})
				}
				a.log.Infof("removing zero-conf invoice %x", invoice.RHash)
				return
			}

			if (invoice.CreationDate + invoice.Expiry) < time.Now().Unix() {
				expirationDate := time.Unix(invoice.CreationDate+invoice.Expiry, 0)
				a.log.Infof("removing expired zero-conf invoice at: %v", expirationDate)
				a.breezDB.RemoveZeroConfHash(invoice.RHash)
			}
		}
	}()
	return nil
}
