package sync

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/btcsuite/btclog"
)

/*
Job contains a running job info.
*/
type Job struct {
	workingDir string
	network    string
	config     config.JobConfig
	shutdown   int32
	log        btclog.Logger
	wg         sync.WaitGroup
	quit       chan struct{}
}

// JobResult is the interface returned as the result of the
// Start method.
type JobResult interface {
	BreachDetected() bool
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
	logBackend, err := log.GetLogBackend(workingDir)
	if err != nil {
		return nil, err
	}
	log := logBackend.Logger("SYNC")

	return &Job{
		log:        log,
		workingDir: workingDir,
		network:    config.Network,
		config:     config.JobCfg,
		quit:       make(chan struct{}),
	}, nil
}
