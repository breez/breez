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
	mu       sync.Mutex
	refCount uint32
	service  *neutrino.ChainService
	db       walletdb.DB
	logger   btclog.Logger
)

/*
NewService returns a new ChainService, either newly created or existing one.
Internally it handles a reference count so callers will be able to use the same
ChainService and won't have to deal with synchronization problems.
The responsiblity of the caller is to call "release" when done using the service.
*/
func NewService(workingDir string) (cs *neutrino.ChainService, release func(), err error) {
	mu.Lock()
	defer mu.Unlock()
	if refCount == 0 {
		cs, err := createService(workingDir)
		if err != nil {
			return nil, nil, err
		}
		service = cs
	}
	refCount++
	return service, release, nil
}

func release() {
	mu.Lock()
	defer mu.Unlock()
	refCount--
	if refCount == 0 {
		stopService()
	}
}

func createService(workingDir string) (*neutrino.ChainService, error) {
	var err error
	neutrino.MaxPeers = 8
	neutrino.BanDuration = 5 * time.Second
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}
	service, db, err = newNeutrino(workingDir, config.Network, &config.JobCfg)
	if err != nil {
		return nil, err
	}
	logBackend, err := log.GetLogBackend(workingDir)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = logBackend.Logger("CHAIN")
		logger.SetLevel(btclog.LevelDebug)
		neutrino.UseLogger(logger)
		neutrino.QueryTimeout = time.Second * 10
	}

	logger.Infof("chain service was created successfuly")
	return service, err
}

func stopService() {

	if db != nil {
		db.Close()
	}
	if service != nil {
		service.Stop()
		service = nil
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
