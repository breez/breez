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
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"

	breezservice "github.com/breez/breez/breez"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
	"github.com/breez/breez/services"
	"github.com/breez/lspd/btceclegacy"
	lspdrpc "github.com/breez/lspd/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
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

	a.log.Infof("finding channel %v", chanPoint)
	channel, err := a.findChannel(chanPoint)
	if err != nil {
		a.log.Infof("failed to find channel %v", err)
		return err
	}
	if channel == nil {
		a.log.Infof("could not find channel %v, no result", chanPoint)
		return nil
	}
	a.log.Infof("resetting chain info for channel %v to block: %v", chanPoint, blockHeight)
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

	var remotePubKey []byte
	var fundingOutpoint wire.OutPoint
	a.log.Infof("finding channel %v", chanPoint)
	c, err := a.findChannel(chanPoint)
	if err != nil {
		a.log.Infof("failed to find channel %v", err)
		return false, err
	}
	if c == nil {
		a.log.Infof("could not find channel %v, no result, looking in closed channels", chanPoint)
		summary, err := a.findPendingClosedChannel(chanPoint)
		if err != nil {
			a.log.Errorf("failed to query pending closed channels %v", err)
			return false, err
		}
		if summary == nil {
			a.log.Infof("channel %v was not found in pending closed", chanPoint)
			return false, nil
		}
		a.log.Infof("found pending closed channel: %v", chanPoint)
		remotePubKey = summary.RemotePub.SerializeCompressed()
		fundingOutpoint = summary.ChanPoint
	} else {
		remotePubKey = c.IdentityPub.SerializeCompressed()
		fundingOutpoint = c.FundingOutpoint
	}

	a.log.Infof("found waiting close channel: %v", chanPoint)

	if hex.EncodeToString(remotePubKey) != lspNodePubkey {
		return false, nil
	}
	hint, err := hintCache.QuerySpendHint(chainntnfs.SpendRequest{OutPoint: fundingOutpoint})
	if err != nil && err != chainntnfs.ErrSpendHintNotFound {
		return false, err
	}
	_, unconfirmedClosed, err := a.checkChannels(map[string]uint64{}, map[string]uint64{chanPoint: uint64(hint)},
		lspPubkey, lspID)
	if err != nil {
		return false, err
	}
	hasMismatch := len(unconfirmedClosed) > 0
	a.log.Infof("checkLSPClosedChannelMismatch finished, hasMismatch = %v", hasMismatch)
	return hasMismatch, nil
}

func (a *lspChanStateSync) checkChannels(fakeChannels, waitingCloseChannels map[string]uint64,
	lspPubkey []byte, lspID string) (map[string]uint64, map[string]uint64, error) {
	c, ctx, cancel := a.breezAPI.NewChannelOpenerClient()
	defer cancel()

	priv, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	checkChannelsRequest := &lspdrpc.CheckChannelsRequest{
		EncryptPubkey:        priv.PubKey().SerializeCompressed(),
		FakeChannels:         fakeChannels,
		WaitingCloseChannels: waitingCloseChannels,
	}
	data, _ := proto.Marshal(checkChannelsRequest)
	pubkey, err := btcec.ParsePubKey(lspPubkey)
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
	encrypted, err := btceclegacy.Encrypt(pubkey, signedData)
	if err != nil {
		a.log.Infof("btcec.Encrypt(%x) error: %v", data, err)
		return nil, nil, fmt.Errorf("btcec.Encrypt(%x) error: %w", data, err)
	}
	r, err := c.CheckChannels(ctx, &breezservice.CheckChannelsRequest{LspId: lspID, Blob: encrypted})
	if err != nil {
		return nil, nil, fmt.Errorf("CheckChannels error: %w", err)
	}
	decrypt, err := btceclegacy.Decrypt(priv, r.Blob)
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

	channels, err := chandb.ChannelStateDB().FetchAllChannels()
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

func (a *lspChanStateSync) findPendingClosedChannel(chanPoint string) (*channeldb.ChannelCloseSummary, error) {
	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	channels, err := chandb.ChannelStateDB().FetchClosedChannels(true)
	if err != nil {
		return nil, err
	}

	for _, c := range channels {
		if c.ChanPoint.String() == chanPoint {
			return c, nil
		}
	}
	return nil, nil
}
