package sync

import (
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/lightninglabs/neutrino"
)

// ChannelsWatcher contains all the data that is needed in order to scan several
// input scripts using neutrino compact filters download and matching.
// This is usefull to run as a background job that scans periodically all the user
// opened channels funding points, detect a possible closed channel and warn the user if
// a the channel was ulitirally found.
type ChannelsWatcher struct {
	log                     btclog.Logger
	chainService            *neutrino.ChainService
	db                      *jobDB
	quitChan                chan struct{}
	watchedScripts          []neutrino.InputWithScript
	firstChannelBlockHeight uint64
	closedChannelDetected   bool
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

	chandb, cleanup, err := channeldbservice.NewService(workingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	channels, err := chandb.FetchAllOpenChannels()
	if err != nil {
		return nil, err
	}

	var watchedScripts []neutrino.InputWithScript
	var firstChannelBlockHeight uint64

	for _, c := range channels {
		fundingOut := c.FundingOutpoint
		localKey := c.LocalChanCfg.MultiSigKey.PubKey.SerializeCompressed()
		remoteKey := c.RemoteChanCfg.MultiSigKey.PubKey.SerializeCompressed()
		multiSigScript, err := lnwallet.GenMultiSigScript(
			localKey, remoteKey,
		)
		if err != nil {
			return nil, err
		}

		pkScript, err := lnwallet.WitnessScriptHash(multiSigScript)
		if err != nil {
			return nil, err
		}

		log.Infof("watching channel id: %v", c.ShortChannelID.String())
		channelBlockHeight := uint64(c.ShortChannelID.BlockHeight)
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
	if startHeight == 0 && b.firstChannelBlockHeight > 0 {
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

	startStamp := &waddrmgr.BlockStamp{Height: int32(startHeight), Hash: *startHash}
	tipStamp := &waddrmgr.BlockStamp{Height: int32(tipHeight), Hash: *tipHash}
	rescan := b.chainService.NewRescan(
		neutrino.StartBlock(startStamp),
		neutrino.EndBlock(tipStamp),
		neutrino.QuitChan(b.quitChan),
		neutrino.NotificationHandlers(
			rpcclient.NotificationHandlers{
				OnFilteredBlockConnected: b.onFilteredBlockConnected,
			},
		),
	)

	for _, c := range b.watchedScripts {
		go rescan.Update(neutrino.AddInputs(c))
	}

	err = <-rescan.Start()
	if err == nil && !b.closedChannelDetected {
		b.db.setChannelsWatcherBlockHeight(tipHeight)
	}
	b.log.Infof("ChannelsWatcher finished: closedChannelDetected=%v err=%v", b.closedChannelDetected, err)
	return b.closedChannelDetected, err
}

func (b *ChannelsWatcher) onFilteredBlockConnected(height int32, header *wire.BlockHeader,
	txs []*btcutil.Tx) {
	b.closedChannelDetected = b.closedChannelDetected || (len(txs) > 0)
}
