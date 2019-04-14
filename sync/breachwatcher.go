package sync

import (
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/lightninglib/lnwallet"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/lightninglabs/neutrino"
)

type BreachWatcher struct {
	chainService   neutrino.ChainService
	db             *jobDB
	watchedScripts []neutrino.InputWithScript
}

func NewBreachWatcher(cfg *config.Config, chainService neutrino.ChainService) (*BreachWatcher, error) {

	chandb, cleanup, err := channeldbservice.NewService(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	channels, err := chandb.FetchAllOpenChannels()
	if err != nil {
		return nil, err
	}

	var watchedScripts []neutrino.InputWithScript
	for i, c := range channels {
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
		watchedScripts = append(watchedScripts, neutrino.InputWithScript{OutPoint: fundingOut, PkScript: pkScript})
	}

	return &BreachWatcher{
		chainService:   chainService,
		watchedScripts: watchedScripts,
	}, nil
}

func (b *BreachWatcher) Scan(tipHeight int64) error {
	startHeight, err := b.db.fetchBreachWatcherHeight()
	if err != nil {
		return err
	}

	startHash, err := b.chainService.GetBlockHash(int64(startHeight))
	if err != nil {
		return err
	}

	tipHash, err := b.chainService.GetBlockHash(int64(tipHeight))
	if err != nil {
		return err
	}

	startStamp := &waddrmgr.BlockStamp{Height: int32(startHeight), Hash: *startHash}
	tipStamp := &waddrmgr.BlockStamp{Height: int32(tipHeight), Hash: *tipHash}
	b.chainService.NewRescan(
		neutrino.StartBlock(startStamp),
		neutrino.EndBlock(tipStamp),
		neutrino.QuitChan(n.quit),
		neutrino.NotificationHandlers(
			rpcclient.NotificationHandlers{
				OnFilteredBlockConnected:    n.onFilteredBlockConnected,
				OnFilteredBlockDisconnected: n.onFilteredBlockDisconnected,
			},
		),
		neutrino.WatchInputs(b.watchedScripts),
	)
}
