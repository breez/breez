package sync

import (
	"fmt"

	"github.com/breez/breez/channeldbservice"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/input"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ChannelsWatcher contains all the data that is needed in order to scan several
// input scripts using neutrino compact filters download and matching.
// This is usefull to run as a background job that scans periodically all the user
// opened channels funding points, detect a possible closed channel and warn the user if
// a the channel was ulitirally found.
type ChannelsWatcher struct {
	log                      btclog.Logger
	chainService             *neutrino.ChainService
	db                       *jobDB
	quitChan                 chan struct{}
	watchedScripts           []neutrino.InputWithScript
	firstChannelBlockHeight  uint64
	closedChannelBlockHeight int32
}

// NewChannelsWatcher creates a new ChannelsWatcher.
// First it makes sure it has a valid ChainService and then fetches all the
// input scripts from the user channels needed to be watched.
func NewChannelsWatcher(
	workingDir string,
	chainService *neutrino.ChainService,
	log btclog.Logger,
	db *jobDB,
	quitChan chan struct{}) (*ChannelsWatcher, error) {

	chandb, cleanup, err := channeldbservice.Get(workingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	channels, err := chandb.ChannelStateDB().FetchAllOpenChannels()
	if err != nil {
		return nil, err
	}

	var watchedScripts []neutrino.InputWithScript
	var firstChannelBlockHeight uint64

	for _, c := range channels {
		if c.LocalCommitment.LocalBalance < lnwire.NewMSatFromSatoshis(1000) {
			log.Infof("Skipping watching channel with less than 1000 sats balance: %v", c.ShortChannelID.String())
			continue
		}

		fundingOut := c.FundingOutpoint
		localKey := c.LocalChanCfg.MultiSigKey.PubKey.SerializeCompressed()
		remoteKey := c.RemoteChanCfg.MultiSigKey.PubKey.SerializeCompressed()
		multiSigScript, err := input.GenMultiSigScript(
			localKey, remoteKey,
		)
		if err != nil {
			return nil, err
		}

		pkScript, err := input.WitnessScriptHash(multiSigScript)
		if err != nil {
			return nil, err
		}

		log.Infof("watching channel id: %v", c.ShortChannelID.String())
		channelBlockHeight := uint64(c.ShortChannelID.BlockHeight)

		// query spend hint for channel
		hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
			QueryDisable: false,
		}, chandb)
		if err != nil {
			return nil, fmt.Errorf("failed to create height hint cache for channel %v", err)
		}
		spentRequest, err := chainntnfs.NewSpendRequest(&fundingOut, pkScript)
		if err != nil {
			return nil, fmt.Errorf("failed to create spent request %v", err)
		}
		startBlock, err := hintCache.QuerySpendHint(spentRequest)
		if err != nil && err != chainntnfs.ErrSpendHintNotFound {
			return nil, fmt.Errorf("failed to query spent hint %v", err)
		}
		log.Info("Query hint for channel %v = %v", fundingOut.String(), startBlock)
		if channelBlockHeight < uint64(startBlock) {
			channelBlockHeight = uint64(startBlock)
		}
		if firstChannelBlockHeight == 0 || channelBlockHeight < firstChannelBlockHeight {
			firstChannelBlockHeight = channelBlockHeight
		}
		watchedScripts = append(watchedScripts, neutrino.InputWithScript{OutPoint: fundingOut, PkScript: pkScript})
	}

	return &ChannelsWatcher{
		chainService:            chainService,
		db:                      db,
		log:                     log,
		quitChan:                quitChan,
		firstChannelBlockHeight: firstChannelBlockHeight,
		watchedScripts:          watchedScripts,
	}, nil
}

// Scan scans from the last saved point up to the tipHeight given as parameter.
// it scans for all watchedScripts and if the ChainService notifies on a filter match
// this function returns true to reflect that a closed channel was detected and the
// user should be warned.
func (b *ChannelsWatcher) Scan(tipHeight uint64) (bool, error) {
	if len(b.watchedScripts) == 0 {
		b.log.Infof("No input scripts to watch, skipping scan")
		return false, b.db.setChannelsWatcherBlockHeight(tipHeight)
	}

	startHeight, err := b.db.fetchChannelsWatcherBlockHeight()
	if err != nil {
		return false, err
	}
	b.log.Info("fetchChannelsWatcherBlockHeight = %v", startHeight)
	if b.firstChannelBlockHeight > startHeight {
		startHeight = b.firstChannelBlockHeight
	}

	startHash, err := b.chainService.GetBlockHash(int64(startHeight))
	if err != nil {
		return false, err
	}

	tipHash, err := b.chainService.GetBlockHash(int64(tipHeight))
	if err != nil {
		return false, err
	}

	b.log.Infof("Scanning for closed channels in range %v-%v", startHeight, tipHeight)

	startStamp := &headerfs.BlockStamp{Height: int32(startHeight), Hash: *startHash}
	tipStamp := &headerfs.BlockStamp{Height: int32(tipHeight), Hash: *tipHash}

	rescan := neutrino.NewRescan(
		&neutrino.RescanChainSource{
			ChainService: b.chainService,
		},
		neutrino.StartBlock(startStamp),
		neutrino.EndBlock(tipStamp),
		neutrino.QuitChan(b.quitChan),
		neutrino.NotificationHandlers(
			rpcclient.NotificationHandlers{
				OnFilteredBlockConnected: b.onFilteredBlockConnected,
			},
		),
		neutrino.ProgressHandler(func(lastBlock uint32) {
			// in case we didn't find any closed channels, we update the last
			// watcher state.
			if b.closedChannelBlockHeight == 0 && lastBlock%100 == 0 {
				b.db.setChannelsWatcherBlockHeight(uint64(lastBlock))
			}
		}),
	)

	for _, c := range b.watchedScripts {
		go rescan.Update(neutrino.AddInputs(c))
	}

	err = <-rescan.Start()

	// We advance the channel watcher next start in case we either didn't find
	// closed channels or the one we found is more than 144 blocks away from tip.
	// The main reason is we don't want to notify again for the same range of blocks.
	shouldNotify := tipHeight-uint64(b.closedChannelBlockHeight) >= 144
	if err == nil && (b.closedChannelBlockHeight == 0 || shouldNotify) {
		b.db.setChannelsWatcherBlockHeight(tipHeight)
	}

	b.log.Infof("ChannelsWatcher finished: closedChannelDetected=%v err=%v", b.closedChannelBlockHeight, err)
	return shouldNotify && b.closedChannelBlockHeight > 0, err
}

func (b *ChannelsWatcher) onFilteredBlockConnected(height int32, header *wire.BlockHeader,
	txs []*btcutil.Tx) {
	if (len(txs) > 0) && (b.closedChannelBlockHeight == 0 || height < b.closedChannelBlockHeight) {
		b.closedChannelBlockHeight = height
	}
}
