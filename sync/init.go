package sync

import (
	"sync"

	"github.com/breez/breez"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

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

	return &Job{
		workingDir: workingDir,
		network:    config.Network,
		config:     config.JobCfg,
	}, nil
}
