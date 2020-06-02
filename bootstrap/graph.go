package bootstrap

import (
	"fmt"
	"path"
	"time"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	"github.com/coreos/bbolt"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
)

const (
	threshold = 4 * 24 * time.Hour
)

func GraphURL(workingDir string, breezDB *db.DB) (string, error) {
	// open the destination database.
	chanDB, chanDBCleanUp, err := channeldbservice.Get(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to open channeldb: %w", err)
	}
	defer chanDBCleanUp()

	chainService, chainServiceCleanUp, err := chainservice.Get(workingDir, breezDB)
	if err != nil {
		//chanDBCleanUp()
		return "", fmt.Errorf("failed to create chainservice: %w", err)
	}
	defer chainServiceCleanUp()

	chanID, err := chanDB.ChannelGraph().HighestChanID()
	if err != nil {
		return "", fmt.Errorf("chanDB.ChannelGraph().HighestChanID(): %w", err)
	}

	bs, err := chainService.BestBlock()
	if err != nil {
		return "", fmt.Errorf("chainService.BestBlock(): %w", err)
	}

	t := bs.Timestamp
	scid := lnwire.NewShortChanIDFromInt(chanID)
	_ = scid.BlockHeight
	if uint32(bs.Height) > scid.BlockHeight {
		hash, err := chainService.GetBlockHash(int64(scid.BlockHeight))
		if err != nil {
			return "", fmt.Errorf("chainService.GetBlockHash(%v): %w", scid.BlockHeight, err)
		}
		header, err := chainService.GetBlockHeader(hash)
		if err != nil {
			return "", fmt.Errorf("chainService.GetBlockHeader(%v): %w", scid.BlockHeight, err)
		}
		t = header.Timestamp
	} else {
		t = t.Add(time.Duration(int(scid.BlockHeight)-int(bs.Height)) * 10 * time.Minute)
	}

	url := ""
	if time.Now().After(t.Add(threshold)) {
		cfg, err := config.GetConfig(workingDir)
		if err != nil {
			return "", fmt.Errorf("config.GetConfig(%v): %w", workingDir, err)
		}
		var meta *channeldb.Meta
		err = chanDB.View(func(tx *bbolt.Tx) error {
			meta, err = chanDB.FetchMeta(tx)
			return err
		})
		if err != nil {
			return "", fmt.Errorf("chanDB.FetchMeta(): %w", err)
		}
		filename := "graph-" + fmt.Sprintf("%04x", meta.DbVersionNumber) + ".db"
		url = path.Join(cfg.BootstrapURL, cfg.Network, "/graph/", filename)
	}

	return url, nil
}
