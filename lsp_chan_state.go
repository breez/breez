package breez

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btclog"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	"github.com/breez/breez/services"
	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/golang/protobuf/proto"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
)

type peerSnapshot struct {
	pubKey          string
	unconfirmedOpen map[string]uint64
}

type lspChanStateSync struct {
	cfg       *config.Config
	log       btclog.Logger
	breezAPI  services.API
	breezDB   *db.DB
	daemonAPI lnnode.API
	snapshots map[string]*peerSnapshot
}

func newLSPChanStateSync(app *App) *lspChanStateSync {
	return &lspChanStateSync{
		log:       app.log,
		breezAPI:  app.ServicesClient,
		daemonAPI: app.lnDaemon,
		breezDB:   app.breezDB,
		cfg:       app.cfg,
		snapshots: make(map[string]*peerSnapshot, 0),
	}
}

func (a *lspChanStateSync) resetClosedChannelChainInfo(chanPoint string, blockHeight int64) error {
	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	channel, err := a.findChannel(chanPoint)
	if err != nil {
		return err
	}
	return hintCache.CommitSpendHint(uint32(blockHeight),
		chainntnfs.SpendRequest{OutPoint: channel.FundingOutpoint})
}

func (a *lspChanStateSync) checkLSPClosedChannelMismatch(lspNodePubkey string, lspPubkey []byte,
	lspID string, chanPoint string) (bool, error) {

	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return false, err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	c, err := a.findChannel(chanPoint)
	if err != nil || c == nil {
		return false, err
	}

	remotePubkey := c.IdentityPub.SerializeCompressed()
	if hex.EncodeToString(remotePubkey) != lspNodePubkey {
		return false, nil
	}
	hint, err := hintCache.QuerySpendHint(chainntnfs.SpendRequest{OutPoint: c.FundingOutpoint})
	if err != nil && err != chainntnfs.ErrSpendHintNotFound {
		return false, err
	}
	_, unconfirmedClosed, err := a.checkChannels(map[string]uint64{}, map[string]uint64{chanPoint: uint64(hint)},
		lspPubkey, lspID)
	if err != nil {
		return false, err
	}
	return len(unconfirmedClosed) > 0, nil
}

func (a *lspChanStateSync) recordChannelsStatus() error {
	status, err := a.collectChannelsStatus()
	if err != nil {
		return err
	}
	a.snapshots = status
	channels, err := a.breezDB.FetchMismatchedChannels()
	if err != nil {
		return err
	}
	if channels == nil || len(channels.ChanPoints) == 0 {
		return nil
	}
	a.log.Infof("found channels mismatch to purge: %v", channels.ChanPoints)
	if err := a.commitHeightHint(channels.LSPPubkey, channels.ChanPoints); err != nil {
		a.log.Errorf("failed to purge height hint for channels: %v error: %v", channels.ChanPoints, err)
	}
	a.log.Infof("succesfully purged channels mismatch hints")
	return a.breezDB.RemoveChannelMismatch()
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
		if !c.ShortChanID().IsFake() {
			continue
		}

		a.log.Infof("collecting status for channel id: %v", c.ShortChannelID.String())
		peerPubkey := hex.EncodeToString(c.IdentityPub.SerializeCompressed())
		a.log.Infof("collecting lsp channels for pubkey: %v", peerPubkey)
		snapshot, ok := snapshots[peerPubkey]
		if !ok {
			snapshot = &peerSnapshot{
				pubKey:          peerPubkey,
				unconfirmedOpen: make(map[string]uint64, 0),
			}
			snapshots[peerPubkey] = snapshot
		}

		height, err := hintCache.QueryConfirmHint(chainntnfs.ConfRequest{TxID: c.FundingOutpoint.Hash})
		if errors.Is(err, chainntnfs.ErrConfirmHintNotFound) {
			height = 0
		} else if err != nil {
			return nil, err
		}
		snapshot.unconfirmedOpen[c.FundingOutpoint.String()] = uint64(height)
		a.log.Infof("adding unconfirmed channel to query %v fundingHeight=%v hint=%v",
			c.FundingOutpoint.Hash.String(), c.FundingBroadcastHeight, height)
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

	for key, val := range afterStartSnapshot.unconfirmedOpen {
		mergedUnconfirmedOpen[key] = val
		if hint, ok := beforeStartSnapshot.unconfirmedOpen[key]; ok {
			a.log.Infof("setting height hint = %v for %v", hint, key)
			mergedUnconfirmedOpen[key] = hint
		}
	}
	confirmedOpen, _, err := a.checkChannels(mergedUnconfirmedOpen, map[string]uint64{}, lspPubkey, lspID)
	if err != nil {
		return false, err
	}

	var mismatched []db.MismatchedChannel
	for c, shortChanID := range confirmedOpen {
		shortID := lnwire.NewShortChanIDFromInt(shortChanID)
		a.log.Infof("got confirmed open for channel %v confirmationHeight = %v", c, shortID.BlockHeight)
		if shortID.BlockHeight <= uint32(afterStartSnapshot.unconfirmedOpen[c]) {
			a.log.Infof("found mismatched unconfirmed channel %v height hint = %v", c, afterStartSnapshot.unconfirmedOpen[c])
			mismatched = append(mismatched, db.MismatchedChannel{
				ChanPoint:   c,
				ShortChanID: shortChanID,
			})
		}
	}
	if err := a.breezDB.SetMismatchedChannels(&db.MismatchedChannels{
		LSPPubkey:  lspNodePubkey,
		ChanPoints: mismatched,
	}); err != nil {
		return false, err
	}
	mismatch := len(mismatched) > 0
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

func (a *lspChanStateSync) commitHeightHint(lspPubkey string, mismatched []db.MismatchedChannel) error {
	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	for _, mismatch := range mismatched {
		txParts := strings.Split(mismatch.ChanPoint, ":")
		if len(txParts) != 2 {
			return errors.New("invalid outpoint")
		}
		hash, err := chainhash.NewHashFromStr(txParts[0])
		if err != nil {
			return err
		}
		blockHeight := lnwire.NewShortChanIDFromInt(mismatch.ShortChanID).BlockHeight
		if err := hintCache.CommitConfirmHint(blockHeight,
			chainntnfs.ConfRequest{TxID: *hash}); err != nil {
			return err
		}
	}

	return nil
}

func (a *lspChanStateSync) findChannel(chanPoint string) (*channeldb.OpenChannel, error) {
	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	channels, err := chandb.FetchAllChannels()
	if err != nil {
		return nil, err
	}

	for _, c := range channels {
		if c.FundingOutpoint.String() == chanPoint {
			return c, nil
		}
	}
	return nil, nil
}
