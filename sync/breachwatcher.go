package sync

import (
	"fmt"

	"github.com/breez/breez/channeldbservice"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/lightninglabs/neutrino"
)

// BreachWatcher contains all the data that is needed in order to scan several
// input scripts using neutrino compact filters download and matching.
// This is usefull to run as a background job that scans periodically all the user
// opened channels funding points, detect a possible breach and warn the user if
// a breach was found.
type BreachWatcher struct {
	log            btclog.Logger
	chainService   *neutrino.ChainService
	db             *jobDB
	quitChan       chan struct{}
	watchedScripts []neutrino.InputWithScript
	breachDetected bool
}

// NewBreachWatcher creates a new BreachWatcher.
// First it makes sure it has a valid ChainService and then fetches all the
// input scripts from the user channels needed to be watched.
func NewBreachWatcher(
	workingDir string,
	chainService *neutrino.ChainService,
	log btclog.Logger,
	db *jobDB,
	quitChan chan struct{}) (*BreachWatcher, error) {

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
		fmt.Println("channel id: ", c.ShortChannelID.String())
		watchedScripts = append(watchedScripts, neutrino.InputWithScript{OutPoint: fundingOut, PkScript: pkScript})
	}

	return &BreachWatcher{
		chainService:   chainService,
		db:             db,
		log:            log,
		quitChan:       quitChan,
		watchedScripts: watchedScripts,
	}, nil
}

// Scan scans from the last saved point up to the tipHeight given as parameter.
// it scans for all watchedScripts and if the ChainService notifies on a filter match
// this function returns true to reflect that a breach was detected and the user should
// be warned.
func (b *BreachWatcher) Scan(tipHeight uint64) (bool, error) {
	startHeight, err := b.db.fetchBreachWatcherHeight()
	if err != nil {
		return false, err
	}

	startHash, err := b.chainService.GetBlockHash(int64(startHeight))
	if err != nil {
		return false, err
	}

	tipHash, err := b.chainService.GetBlockHash(int64(tipHeight))
	if err != nil {
		return false, err
	}

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
	if err == nil && !b.breachDetected {
		b.db.setBreachWatcherBlockHeight(tipHeight)
	}
	return b.breachDetected, err
}

func (b *BreachWatcher) onFilteredBlockConnected(height int32, header *wire.BlockHeader,
	txs []*btcutil.Tx) {
	b.log.Infof("onFilteredBlockConnected height = %v, txs: %v", height, len(txs))
	b.breachDetected = b.breachDetected || (len(txs) > 0)
}
