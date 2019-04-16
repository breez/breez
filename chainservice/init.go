package chainservice

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/breez/breez/refcount"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

const (
	directoryPattern = "data/chain/bitcoin/{{network}}/"
)

var (
	serviceRefCounter refcount.ReferenceCountable
	service           *neutrino.ChainService
	db                walletdb.DB
	logger            btclog.Logger
)

// Get returned a reusable ChainService
func Get(workingDir string) (cs *neutrino.ChainService, cleanupFn func() error, err error) {
	service, release, err := serviceRefCounter.Get(
		func() (interface{}, refcount.ReleaseFunc, error) {
			return newService(workingDir)
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return service.(*neutrino.ChainService), release, err
}

/*
NewService returns a new ChainService.
The responsiblity of the caller is to call "cleanupFn" when done using the service.
*/
func newService(workingDir string) (*neutrino.ChainService, refcount.ReleaseFunc, error) {
	cs, err := createService(workingDir)
	return cs, stopService, err
}

func createService(workingDir string) (*neutrino.ChainService, error) {
	var err error
	neutrino.MaxPeers = 8
	neutrino.BanDuration = 5 * time.Second
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logBackend, err := log.GetLogBackend(workingDir)
		if err != nil {
			return nil, err
		}
		logger = logBackend.Logger("CHAIN")
		logger.SetLevel(btclog.LevelDebug)
		neutrino.UseLogger(logger)
		neutrino.QueryTimeout = time.Second * 10
	}
	logger.Infof("creating shared chain service.")
	service, db, err = newNeutrino(workingDir, config.Network, &config.JobCfg)
	if err != nil {
		logger.Errorf("failed to create chain service %v", err)
		return nil, err
	}

	logger.Infof("chain service was created successfuly")
	return service, err
}

func stopService() error {

	if db != nil {
		if err := db.Close(); err != nil {
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
		return nil, nil, errors.New("neutrino db does not exist")
	}

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
	logger.Infof("creating new neutrino service.")
	chainService, err := neutrino.NewChainService(neutrinoConfig)
	return chainService, db, err
}
