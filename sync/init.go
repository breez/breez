package sync

import (
	"io"
	"os"
	"sync"

	"github.com/breez/breez"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

func initJobLogger(workingDir, network string) (btclog.Logger, error) {
	filename := workingDir + "/logs/bitcoin/" + network + "/lnd.log"
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	if err != nil {
		return nil, err
	}

	go io.Copy(os.Stdout, reader)
	go io.Copy(f, reader)

	logger := btclog.NewBackend(writer)
	log := logger.Logger("SYNC")
	log.SetLevel(btclog.LevelDebug)
	return log, nil
}

/*
Job contains a running job info.
*/
type Job struct {
	workingDir string
	network    string
	config     breez.JobConfig
	neutrino   *neutrino.ChainService
	db         walletdb.DB
	started    int32
	shutdown   int32
	log        btclog.Logger
	wg         sync.WaitGroup
}

/*
NewJob crates a new SyncJob and given a directory for this job.
It is assumed that a config file exists in this directory.
*/
func NewJob(workingDir string) (*Job, error) {
	config, err := breez.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}

	log, err := initJobLogger(workingDir, config.Network)
	if err != nil {
		return nil, err
	}

	return &Job{
		log:        log,
		workingDir: workingDir,
		network:    config.Network,
		config:     config.JobCfg,
	}, nil
}
