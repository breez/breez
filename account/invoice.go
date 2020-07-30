package account

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/zpay32"
)

func (a *Service) generateInvoiceWithNewAmount(payReq string, newAmount int64) (string, error) {
	invoice, err := zpay32.Decode(payReq, a.activeParams)
	if err != nil {
		return "", fmt.Errorf("zpay32.Decode() error: %w", err)
	}

	signerClient := a.daemonAPI.SignerClient()
	if signerClient == nil {
		return "", fmt.Errorf("API is not ready")
	}
	nodeKey := a.daemonAPI.NodePubkey()
	if nodeKey == "" {
		return "", errors.New("node public key wasn't initialized")
	}
	pubkeyBytes, err := hex.DecodeString(nodeKey)
	if err != nil {
		return "", err
	}
	pubKey, err := btcec.ParsePubKey(pubkeyBytes, btcec.S256())
	if err != nil {
		return "", err
	}

	m := lnwire.MilliSatoshi(newAmount)
	invoice.MilliSat = &m
	signer := zpay32.MessageSigner{SignCompact: func(hash []byte) ([]byte, error) {
		kl := signrpc.KeyLocator{
			KeyFamily: int32(keychain.KeyFamilyNodeKey),
			KeyIndex:  0,
		}

		r, err := signerClient.SignMessage(context.Background(), &signrpc.SignMessageReq{Msg: hash, KeyLoc: &kl})
		if err != nil {
			return nil, fmt.Errorf("m.client.SignMessage() error: %w", err)
		}
		sig, err := btcec.ParseDERSignature(r.Signature, btcec.S256())
		if err != nil {
			return nil, fmt.Errorf("btcec.ParseDERSignature error: %w", err)
		}
		return toCompact(sig, pubKey, chainhash.HashB(hash))
	}}
	newInvoice, err := invoice.Encode(signer)
	if err != nil {
		log.Printf("invoice.Encode() error: %v", err)
	}
	return newInvoice, nil
}

func toCompact(sig *btcec.Signature, pubKey *btcec.PublicKey, hash []byte) ([]byte, error) {
	curve := btcec.S256()
	result := make([]byte, 1, curve.BitSize/4+1)
	curvelen := (curve.BitSize + 7) / 8
	bytelen := (sig.R.BitLen() + 7) / 8
	if bytelen < curvelen {
		result = append(result, make([]byte, curvelen-bytelen)...)
	}
	result = append(result, sig.R.Bytes()...)
	bytelen = (sig.S.BitLen() + 7) / 8
	if bytelen < curvelen {
		result = append(result, make([]byte, curvelen-bytelen)...)
	}
	result = append(result, sig.S.Bytes()...)
	for i := 0; i < (curve.H+1)*2; i++ {
		result[0] = 27 + byte(i) + 4 // Add 4 because it's compressed
		recoveredPubKey, _, err := btcec.RecoverCompact(curve, result, hash)
		if err == nil && recoveredPubKey.IsEqual(pubKey) {
			return result, nil
		}
	}
	return nil, errors.New("The signature doesn't correspond to the pubKey")
}

func (a *Service) trackZeroConfInvoice() error {

	invoicesClient := a.daemonAPI.InvoicesClient()
	if invoicesClient == nil {
		return errors.New("trackZeroConfInvoice: api not ready")
	}

	lnClient := a.daemonAPI.APIClient()
	if lnClient == nil {
		return errors.New("trackZeroConfInvoice: api not ready")
	}

	invoiceHashes, err := a.breezDB.FetchZeroConfHashes()
	if err != nil {
		return fmt.Errorf("trackZeroConfInvoice: failed to fetch zero conf hashes %w", err)
	}

	for _, invoiceHash := range invoiceHashes {
		stream, err := invoicesClient.SubscribeSingleInvoice(context.Background(), &invoicesrpc.SubscribeSingleInvoiceRequest{
			RHash: invoiceHash,
		})
		if err != nil {
			return fmt.Errorf("trackZeroConfInvoice: failed to subscribe zero-conf invoice %w", err)
		}

		go func() {
			for {
				invoice, err := stream.Recv()
				a.log.Infof("trackZeroConfInvoice: invoice received")
				if err != nil {
					a.log.Criticalf("trackZeroConfInvoice: failed to receive an invoice : %v", err)
					return
				}
				if invoice.State == lnrpc.Invoice_ACCEPTED {
					a.log.Infof("trackZeroConfInvoice: invoice accepted")
					for _, htlc := range invoice.Htlcs {
						if lnwire.NewShortChanIDFromInt(htlc.ChanId).IsFake() {
							channelTrusted := false
							for _, hint := range invoice.RouteHints {
								for _, hop := range hint.HopHints {
									if hop.ChanId == htlc.ChanId {
										channelTrusted = true
										break
									}
								}
							}
							if channelTrusted {
								invoicesClient.SettleInvoice(context.Background(), &invoicesrpc.SettleInvoiceMsg{
									Preimage: invoice.RPreimage,
								})
							}
						}
					}
					return
				}

				if invoice.Expiry < time.Now().Unix() {
					a.breezDB.RemoveZeroConfHash(invoice.RHash)
				}
			}
		}()
	}
	return nil
}
