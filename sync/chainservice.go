package sync

import (
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/breez/breez"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

const (
	directoryPattern = "data/chain/bitcoin/{{network}}/"
)

/*
NewChainService creates a chain service that the sync job uses
in order to fetch chain data such as headers, filters, etc...
*/
func newNeutrino(workingDir string, network string, jobConfig *breez.JobConfig) (walletdb.DB, *neutrino.ChainService, error) {
	var chainParams *chaincfg.Params
	switch network {
	case "testnet":
		chainParams = &chaincfg.TestNet3Params
	case "simnet":
		chainParams = &chaincfg.SimNetParams
	case "mainnet":
		chainParams = &chaincfg.MainNetParams
	}

	if chainParams == nil {
		return nil, nil, fmt.Errorf("Unrecognized network %v", network)
	}

	logger := btclog.NewBackend(os.Stdout)
	chainLogger := logger.Logger("CHAIN")
	chainLogger.SetLevel(btclog.LevelDebug)
	neutrino.UseLogger(chainLogger)

	//if neutrino directory or wallet directory do not exit then
	//We exit silently.
	dataPath := strings.Replace(directoryPattern, "{{network}}", network, -1)
	neutrinoDataDir := path.Join(workingDir, dataPath)
	neutrinoDB := path.Join(neutrinoDataDir, "neutrino.db")
	if _, err := os.Stat(neutrinoDB); os.IsNotExist(err) {
		return nil, nil, nil
	}
	//os.MkdirAll(path.Join(workingDir, dataPath), os.ModePerm)
	db, err := walletdb.Create("bdb", neutrinoDB)
	if err != nil {
		log.Printf("Error opening DB: %s\n", err)
		return nil, nil, err
	}

	neutrinoConfig := neutrino.Config{
		DataDir:      neutrinoDataDir,
		Database:     db,
		ChainParams:  *chainParams,
		ConnectPeers: jobConfig.ConnectedPeers,
	}

	neutrino.MaxPeers = jobConfig.MaxPeers
	neutrino.BanDuration = time.Duration(jobConfig.BanDuration) * time.Second
	neutrino.QueryPeerConnectTimeout = time.Duration(jobConfig.ConnectTimeout) * time.Second
	chainService, err := neutrino.NewChainService(neutrinoConfig)
	return db, chainService, err
}
