package chainservice

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	"github.com/breez/breez/log"
	"github.com/breez/breez/refcount"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	directoryPattern = "data/chain/bitcoin/{{network}}/"
)

var (
	serviceRefCounter refcount.ReferenceCountable
	service           *neutrino.ChainService
	walletDB          walletdb.DB
	logger            btclog.Logger
)

// Get returned a reusable ChainService
func Get(workingDir string, breezDB *db.DB) (cs *neutrino.ChainService, cleanupFn func() error, err error) {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()

	chainSer, release, err := serviceRefCounter.Get(
		func() (interface{}, refcount.ReleaseFunc, error) {
			return createService(workingDir, breezDB)
		},
	)
	if err != nil {
		return nil, nil, err
	}
	service = chainSer.(*neutrino.ChainService)
	return service, release, err
}

func createService(workingDir string, breezDB *db.DB) (*neutrino.ChainService, refcount.ReleaseFunc, error) {
	var err error
	neutrino.MaxPeers = 8
	neutrino.BanDuration = 5 * time.Second
	neutrino.ConnectionRetryInterval = 1 * time.Second
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, nil, err
	}
	if logger == nil {
		logBackend, err := log.GetLogBackend(workingDir)
		if err != nil {
			return nil, nil, err
		}
		logger = logBackend.Logger("CHAIN")
		logger.Infof("After get logger")
		logger.SetLevel(btclog.LevelDebug)
		neutrino.UseLogger(logger)
		neutrino.QueryBatchTimeout = time.Second * 300
	}
	logger.Infof("creating shared chain service.")

	peers, _, err := breezDB.GetPeers(config.JobCfg.ConnectedPeers)
	if err != nil {
		logger.Errorf("peers error: %v", err)
		return nil, nil, err
	}

	service, walletDB, err = newNeutrino(workingDir, config, peers)
	if err != nil {
		logger.Errorf("failed to create chain service %v", err)
		return nil, stopService, err
	}

	logger.Infof("chain service was created successfuly")
	return service, stopService, err
}

func stopService() error {

	if walletDB != nil {
		if err := walletDB.Close(); err != nil {
			return err
		}
	}
	if service != nil {
		if err := service.Stop(); err != nil {
			return err
		}
		service = nil
	}
	return nil
}

func chainParams(network string) (*chaincfg.Params, error) {
	var params *chaincfg.Params
	switch network {
	case "testnet":
		params = &chaincfg.TestNet3Params
	case "simnet":
		params = &chaincfg.SimNetParams
	case "mainnet":
		params = &chaincfg.MainNetParams
	}

	if params == nil {
		return nil, fmt.Errorf("Unrecognized network %v", network)
	}
	return params, nil
}

func neutrinoDataDir(workingDir string, network string) string {
	dataPath := strings.Replace(directoryPattern, "{{network}}", network, -1)
	return path.Join(workingDir, dataPath)
}

func parseAssertFilterHeader(headerStr string) (*headerfs.FilterHeader, error) {
	if headerStr == "" {
		return nil, nil
	}

	heightAndHash := strings.Split(headerStr, ":")

	height, err := strconv.ParseUint(heightAndHash[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid filter header height: %v", err)
	}

	hash, err := chainhash.NewHashFromStr(heightAndHash[1])
	if err != nil {
		return nil, fmt.Errorf("invalid filter header hash: %v", err)
	}

	return &headerfs.FilterHeader{
		FilterHash: *hash,
		Height:     uint32(height),
	}, nil
}

/*
newNeutrino creates a chain service that the sync job uses
in order to fetch chain data such as headers, filters, etc...
*/
func newNeutrino(workingDir string, cfg *config.Config, peers []string) (*neutrino.ChainService, walletdb.DB, error) {
	params, err := chainParams(cfg.Network)

	if err != nil {
		return nil, nil, err
	}

	//if neutrino directory or wallet directory do not exit then
	//We exit silently.
	dataPath := strings.Replace(directoryPattern, "{{network}}", cfg.Network, -1)
	neutrinoDataDir := path.Join(workingDir, dataPath)
	if err := os.MkdirAll(neutrinoDataDir, 0700); err != nil {
		return nil, nil, err
	}
	neutrinoDB := path.Join(neutrinoDataDir, "neutrino.db")

	db, err := walletdb.Create("bdb", neutrinoDB)
	if err != nil {
		return nil, nil, err
	}

	assertHeader, err := parseAssertFilterHeader(cfg.JobCfg.AssertFilterHeader)
	if err != nil {
		return nil, nil, err
	}

	neutrinoConfig := neutrino.Config{
		DataDir:            neutrinoDataDir,
		Database:           db,
		ChainParams:        *params,
		ConnectPeers:       peers,
		AssertFilterHeader: assertHeader,
	}
	logger.Infof("creating new neutrino service.")
	chainService, err := neutrino.NewChainService(neutrinoConfig)
	return chainService, db, err
}
