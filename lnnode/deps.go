package lnnode

import (
	"io"

	breezlog "github.com/breez/breez/log"
	"github.com/lightninglabs/neutrino"
	"github.com/lightningnetwork/lnd/channeldb"
)

/*
Dependencies is an implementation of interface daemon.Dependencies
used for injecting breez deps int LND running as library
*/
type Dependencies struct {
	workingDir   string
	chainService *neutrino.ChainService
	readyChan    chan interface{}
	chanDB       *channeldb.DB
}

/*
ReadyChan returns the channel passed to LND for getting ready signal
*/
func (d *Dependencies) ReadyChan() chan interface{} {
	return d.readyChan
}

/*
LogPipeWriter returns the io.PipeWriter streamed to the log file.
This will be passed as dependency to LND so breez will have a shared log file
*/
func (d *Dependencies) LogPipeWriter() *io.PipeWriter {
	writer, _ := breezlog.GetLogWriter(d.workingDir)
	return writer
}

/*
ChainService returns a neutrino.ChainService to be used from both sync job and
LND running daemon
*/
func (d *Dependencies) ChainService() *neutrino.ChainService {
	return d.chainService
}

/*
ChanDB returns the channel db for the daemon
*/
func (d *Dependencies) ChanDB() *channeldb.DB {
	return d.chanDB
}
