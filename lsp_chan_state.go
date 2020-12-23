package breez

import (
	"fmt"

	"github.com/btcsuite/btcd/wire"

	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/chainntnfs"
	"github.com/lightningnetwork/lnd/channeldb"
)

type lspChanStateSync struct {
	cfg               *config.Config
	log               btclog.Logger
	unconfirmedOpen   map[wire.OutPoint]uint32
	unconfirmedClosed map[wire.OutPoint]uint32
}

func (a *lspChanStateSync) collectChannelsStatus() error {
	a.unconfirmedOpen = make(map[wire.OutPoint]uint32)
	a.unconfirmedClosed = make(map[wire.OutPoint]uint32)

	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	channels, err := chandb.FetchAllChannels()
	if err != nil {
		return err
	}

	for _, c := range channels {
		if !c.ShortChanID().IsFake() && c.ChanStatus() == channeldb.ChanStatusDefault {
			continue
		}

		a.log.Infof("asking status for channel id: %v", c.ShortChannelID.String())
		if err != nil {
			return fmt.Errorf("failed to create height hint cache for channel %w", err)
		}

		if c.ShortChanID().IsFake() {
			height, err := hintCache.QueryConfirmHint(chainntnfs.ConfRequest{TxID: c.FundingOutpoint.Hash})
			if err != nil {
				return err
			}
			a.unconfirmedOpen[c.FundingOutpoint] = height
			a.log.Infof("adding unconfirmed channel to query %v hint=%v", c.FundingOutpoint.Hash.String(), height)
		} else {
			height, err := hintCache.QuerySpendHint(chainntnfs.SpendRequest{OutPoint: c.FundingOutpoint})
			if err != nil {
				return err
			}
			a.unconfirmedClosed[c.FundingOutpoint] = height
			a.log.Infof("adding waiting close channel to query %v hint=%v", c.FundingOutpoint.Hash.String(), height)
		}
	}

	return nil
}

func (a *lspChanStateSync) purgeHeightHint(channelPoints []wire.OutPoint) error {
	chandb, cleanup, err := channeldbservice.Get(a.cfg.WorkingDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// query spend hint for channel
	hintCache, err := chainntnfs.NewHeightHintCache(chainntnfs.CacheConfig{
		QueryDisable: false,
	}, chandb)

	for _, tx := range channelPoints {
		if _, ok := a.unconfirmedOpen[tx]; ok {
			if err := hintCache.PurgeConfirmHint(chainntnfs.ConfRequest{TxID: tx.Hash}); err != nil {
				return err
			}
		}
		if _, ok := a.unconfirmedClosed[tx]; ok {
			if err := hintCache.PurgeSpendHint(chainntnfs.SpendRequest{OutPoint: tx}); err != nil {
				return err
			}
		}
	}
	return nil
}
