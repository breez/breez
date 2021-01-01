package breez

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/lnnode"
	"github.com/breez/breez/services"
	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
)

type peerSnapshot struct {
	pubKey            string
	unconfirmedOpen   map[string]uint64
	unconfirmedClosed map[string]uint64
}

type lspChanStateSync struct {
	cfg       *config.Config
	log       btclog.Logger
	breezAPI  services.API
	daemonAPI lnnode.API
	snapshots map[string]*peerSnapshot
}

func newLSPChanStateSync(app *App) *lspChanStateSync {
	return &lspChanStateSync{
		log:       app.log,
		breezAPI:  app.ServicesClient,
		daemonAPI: app.lnDaemon,
		cfg:       app.cfg,
		snapshots: make(map[string]*peerSnapshot, 0),
	}
}

func (a *lspChanStateSync) recordChannelsStatus() error {
	status, err := a.collectChannelsStatus()
	if err != nil {
		return err
	}
	a.snapshots = status
	return nil
}

func (a *lspChanStateSync) collectChannelsStatus() (map[string]*peerSnapshot, error) {
	snapshots := make(map[string]*peerSnapshot)

	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	channels, err := chandb.FetchAllChannels()
	if err != nil {
		return nil, err
	}

	for _, c := range channels {
		if !c.ShortChanID().IsFake() && c.ChanStatus() == channeldb.ChanStatusDefault {
			continue
		}

		a.log.Infof("asking status for channel id: %v", c.ShortChannelID.String())
		if err != nil {
			return nil, fmt.Errorf("failed to create height hint cache for channel %w", err)
		}

		peerPubkey := hex.EncodeToString(c.IdentityPub.SerializeCompressed())
		a.log.Infof("sync lsp channels for pubkey: %v", peerPubkey)
		snapshot, ok := snapshots[peerPubkey]
		if !ok {
			snapshot = &peerSnapshot{
				pubKey:            peerPubkey,
				unconfirmedOpen:   make(map[string]uint64, 0),
				unconfirmedClosed: make(map[string]uint64, 0),
			}
			snapshots[peerPubkey] = snapshot
		}

		if c.ChanStatus() == channeldb.ChanStatusDefault {
			height, err := hintCache.QueryConfirmHint(chainntnfs.ConfRequest{TxID: c.FundingOutpoint.Hash})
			if errors.Is(err, chainntnfs.ErrConfirmHintNotFound) {
				height = 0
			} else if err != nil {
				return nil, err
			}
			snapshot.unconfirmedOpen[c.FundingOutpoint.String()] = uint64(height)
			a.log.Infof("adding unconfirmed channel to query %v hint=%v", c.FundingOutpoint.Hash.String(), height)
		} else {
			height, err := hintCache.QuerySpendHint(chainntnfs.SpendRequest{OutPoint: c.FundingOutpoint})
			if errors.Is(err, chainntnfs.ErrConfirmHintNotFound) {
				height = 0
			} else if err != nil {
				return nil, err
			}
			snapshot.unconfirmedClosed[c.FundingOutpoint.String()] = uint64(height)
			a.log.Infof("adding waiting close channel to query %v hint=%v", c.FundingOutpoint.Hash.String(), height)
		}
	}

	return snapshots, nil
}

func (a *lspChanStateSync) syncChannels(lspNodePubkey string, lspPubkey []byte, lspID string) (bool, error) {
	a.log.Infof("syncChannels for pubkey: %v", lspNodePubkey)
	beforeStartSnapshot, ok := a.snapshots[lspNodePubkey]
	if !ok {
		return false, nil
	}

	currentSnapshots, err := a.collectChannelsStatus()
	if err != nil {
		return false, err
	}

	afterStartSnapshot, ok := currentSnapshots[lspNodePubkey]
	if !ok {
		return false, nil
	}
	mergedUnconfirmedOpen := make(map[string]uint64, 0)
	mergedUnconfirmedClosed := make(map[string]uint64, 0)

	for key, val := range afterStartSnapshot.unconfirmedOpen {
		mergedUnconfirmedOpen[key] = val
		if hint, ok := beforeStartSnapshot.unconfirmedOpen[key]; ok {
			a.log.Infof("setting height hint = %v for %v", hint, key)
			mergedUnconfirmedOpen[key] = hint
		}
	}
	for key, val := range afterStartSnapshot.unconfirmedClosed {
		mergedUnconfirmedClosed[key] = val
		if hint, ok := beforeStartSnapshot.unconfirmedClosed[key]; ok {
			a.log.Infof("setting height hint = %v for %v", hint, key)
			mergedUnconfirmedClosed[key] = hint
		}
	}
	confirmedOpen, confirmedClosed, err := a.checkChannels(mergedUnconfirmedOpen, mergedUnconfirmedClosed, lspPubkey, lspID)
	if err != nil {
		return false, err
	}

	mismatch := false
	if len(confirmedOpen) > 0 {
		mismatch = true
		if err := a.purgeHeightHint(lspNodePubkey, confirmedOpen); err != nil {
			a.log.Errorf("failed to purge height hint for opened channels: %v error: %v", confirmedOpen, err)
		}
	}
	if len(confirmedClosed) > 0 {
		mismatch = true
		if err := a.purgeHeightHint(lspNodePubkey, confirmedClosed); err != nil {
			a.log.Errorf("failed to purge height hint for closed channels: %v error: %v", confirmedOpen, err)
		}
	}
	a.log.Infof("syncChannels finished mismatch = %v", mismatch)
	return mismatch, nil
}

func (a *lspChanStateSync) checkChannels(fakeChannels, waitingCloseChannels map[string]uint64,
	lspPubkey []byte, lspID string) (map[string]uint64, map[string]uint64, error) {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()

	priv, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		return nil, nil, err
	}
	checkChannelsRequest := &lspdrpc.CheckChannelsRequest{
		EncryptPubkey:        priv.PubKey().SerializeCompressed(),
		FakeChannels:         fakeChannels,
		WaitingCloseChannels: waitingCloseChannels,
	}
	data, _ := proto.Marshal(checkChannelsRequest)
	pubkey, err := btcec.ParsePubKey(lspPubkey, btcec.S256())
	if err != nil {
		a.log.Infof("btcec.ParsePubKey(%x) error: %v", lspPubkey, err)
		return nil, nil, fmt.Errorf("btcec.ParsePubKey(%x) error: %w", lspPubkey, err)
	}

	signerClient := a.daemonAPI.SignerClient()
	signatureReply, err := signerClient.SignMessage(context.Background(), &signrpc.SignMessageReq{Msg: data,
		KeyLoc: &signrpc.KeyLocator{
			KeyFamily: int32(keychain.KeyFamilyNodeKey),
			KeyIndex:  0,
		}})
	if err != nil {
		a.log.Infof("signerClient.SignMessage() error: %v", err)
		return nil, nil, fmt.Errorf("signerClient.SignMessage() error: %w", err)
	}
	pubKeyBytes, err := hex.DecodeString(a.daemonAPI.NodePubkey())
	if err != nil {
		a.log.Infof("hex.DecodeString(%v) error: %v", a.daemonAPI.NodePubkey(), err)
		return nil, nil, fmt.Errorf("hex.DecodeString(%v) error: error: %w", a.daemonAPI.NodePubkey(), err)
	}
	signed := &lspdrpc.Signed{
		Data:      data,
		Pubkey:    pubKeyBytes,
		Signature: signatureReply.Signature,
	}
	signedData, _ := proto.Marshal(signed)
	encrypted, err := btcec.Encrypt(pubkey, signedData)
	if err != nil {
		a.log.Infof("btcec.Encrypt(%x) error: %v", data, err)
		return nil, nil, fmt.Errorf("btcec.Encrypt(%x) error: %w", data, err)
	}
	r, err := c.CheckChannels(ctx, &breezservice.CheckChannelsRequest{LspId: lspID, Blob: encrypted})
	decrypt, err := btcec.Decrypt(priv, r.Blob)
	if err != nil {
		a.log.Infof("btcec.Decrypt error: %v", err)
		return nil, nil, fmt.Errorf("btcec.Decrypt error: %w", err)
	}
	var checkChannelsReply lspdrpc.CheckChannelsReply
	err = proto.Unmarshal(decrypt, &checkChannelsReply)
	if err != nil {
		a.log.Infof("proto.Unmarshal() error: %v", err)
		return nil, nil, fmt.Errorf("proto.Unmarshal() error: %w", err)
	}
	return checkChannelsReply.NotFakeChannels, checkChannelsReply.ClosedChannels, nil
}

func (a *lspChanStateSync) purgeHeightHint(lspPubkey string, channelPoints map[string]uint64) error {
	lspSnapshot, ok := a.snapshots[lspPubkey]
	if !ok {
		return nil
	}

	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	for tx := range channelPoints {
		txParts := strings.Split(tx, ":")
		if len(txParts) != 2 {
			return errors.New("invalid outpoint")
		}

		index, err := strconv.ParseUint(txParts[1], 10, 32)
		if err != nil {
			return errors.New("invalid tx index")
		}
		hash, err := chainhash.NewHashFromStr(txParts[0])
		if err != nil {
			return err
		}
		outpoint := wire.NewOutPoint(hash, uint32(index))
		if _, ok := lspSnapshot.unconfirmedOpen[tx]; ok {
			if err := hintCache.PurgeConfirmHint(chainntnfs.ConfRequest{TxID: *hash}); err != nil {
				return err
			}
		}
		if _, ok := lspSnapshot.unconfirmedClosed[outpoint.String()]; ok {
			if err := hintCache.PurgeSpendHint(chainntnfs.SpendRequest{OutPoint: *outpoint}); err != nil {
				return err
			}
		}
	}
	return nil
}
