package closedchannels

import (
	"sync"

	"github.com/breez/breez/config"
	breezlog "github.com/breez/breez/log"
	"github.com/btcsuite/btclog"
)

/*
Job contains a running job info.
*/
type Job struct {
	workingDir string
	config     *config.Config
	shutdown   int32
	log        btclog.Logger
	wg         sync.WaitGroup
	quit       chan struct{}
}

/*
NewJob crates a new SyncJob and given a directory for this job.
It is assumed that a config file exists in this directory.
*/
func NewJob(workingDir string) (*Job, error) {
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}
	logger, err := breezlog.GetLogger(workingDir, "CLOSED")
	if err != nil {
		return nil, err
	}

	return &Job{
		log:        logger,
		workingDir: workingDir,
		config:     config,
		quit:       make(chan struct{}),
	}, nil
}
