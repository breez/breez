package chainservice

import (
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

const (
	directoryPattern = "data/chain/bitcoin/{{network}}/"
)

var (
	chainService *neutrino.ChainService
	db           walletdb.DB
	mu           sync.Mutex
)

/*
GetInstance returns the singleton instance of the neutrino chain service.
This instance is used from both sync job and application.
*/
func GetInstance(workingDir string) (*neutrino.ChainService, error) {
	mu.Lock()
	defer mu.Unlock()
	var err error
	if chainService == nil {
		neutrino.MaxPeers = 8
		neutrino.BanDuration = 5 * time.Second
		config, err := config.GetConfig(workingDir)
		if err != nil {
			return nil, err
		}
		chainService, db, err = newNeutrino(workingDir, config.Network, &config.JobCfg)
		if err != nil {
			return nil, err
		}
		logBackend, err := log.GetLogBackend(workingDir)
		if err != nil {
			return nil, err
		}
		logger := logBackend.Logger("CHAIN")
		logger.SetLevel(btclog.LevelDebug)
		neutrino.UseLogger(logger)
	}
	return chainService, err
}

/*
Shutdown stops the chain service and clean its resources
*/
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		db.Close()
	}
	if chainService != nil {
		chainService.Stop()
		chainService = nil
	}
}

/*
newNeutrino creates a chain service that the sync job uses
in order to fetch chain data such as headers, filters, etc...
*/
func newNeutrino(workingDir string, network string, jobConfig *config.JobConfig) (*neutrino.ChainService, walletdb.DB, error) {
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
		return nil, nil, err
	}

	neutrinoConfig := neutrino.Config{
		DataDir:      neutrinoDataDir,
		Database:     db,
		ChainParams:  *chainParams,
		ConnectPeers: jobConfig.ConnectedPeers,
	}

	chainService, err := neutrino.NewChainService(neutrinoConfig)
	return chainService, db, err
}
