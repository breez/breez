package bootstrap

import (
	"encoding/hex"
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
	threshold = 3 * time.Hour
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

func ourData(chanDB *channeldb.DB) (*channeldb.LightningNode, []*channeldb.LightningNode, []*channeldb.ChannelEdgeInfo, []*channeldb.ChannelEdgePolicy, error) {
	graph := chanDB.ChannelGraph()
	ourNode, err := graph.SourceNode()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("graph.SourceNode(): %w", err)
	}

	nodeMap := make(map[string]*channeldb.LightningNode)
	var edges []*channeldb.ChannelEdgeInfo
	var policies []*channeldb.ChannelEdgePolicy

	err = chanDB.DB.View(func(tx *bbolt.Tx) error {
		return ourNode.ForEachChannel(tx, func(tx *bbolt.Tx,
			channelEdgeInfo *channeldb.ChannelEdgeInfo,
			toPolicy *channeldb.ChannelEdgePolicy,
			fromPolicy *channeldb.ChannelEdgePolicy) error {

			nodeMap[hex.EncodeToString(toPolicy.Node.PubKeyBytes[:])] = toPolicy.Node
			edges = append(edges, channelEdgeInfo)
			policies = append(policies, toPolicy, fromPolicy)
			return nil
		})
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("ourNode.ForEachChannel: %w", err)
	}
	var nodes []*channeldb.LightningNode
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	return ourNode, nodes, edges, policies, nil
}

func putOurData(chanDB *channeldb.DB, node *channeldb.LightningNode, nodes []*channeldb.LightningNode, edges []*channeldb.ChannelEdgeInfo, policies []*channeldb.ChannelEdgePolicy) error {
	graph := chanDB.ChannelGraph()

	err := graph.SetSourceNode(node)
	if err != nil {
		return fmt.Errorf("graph.SetSourceNode(%x): %w", node.PubKeyBytes, err)
	}
	for _, n := range nodes {
		err = graph.AddLightningNode(n)
		if err != nil {
			return fmt.Errorf("graph.AddLightningNode(%x): %w", n.PubKeyBytes, err)
		}
	}
	for _, edge := range edges {
		err = graph.AddChannelEdge(edge)
		if err != nil && err != channeldb.ErrEdgeAlreadyExist {
			return fmt.Errorf("graph.AddChannelEdge(%x): %w", edge.ChannelID, err)
		}
	}
	for _, policy := range policies {
		err = graph.UpdateEdgePolicy(policy)
		if err != nil {
			return fmt.Errorf("graph.UpdateEdgePolicy(): %w", err)
		}
	}
	return nil
}
