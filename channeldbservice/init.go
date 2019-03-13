package channeldbservice

import (
	"path"
	"strings"
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/breez/lightninglib/channeldb"
	"github.com/btcsuite/btclog"
)

const (
	directoryPattern = "data/graph/{{network}}/"
)

var (
	mu       sync.Mutex
	refCount uint32
	chanDB   *channeldb.DB
	logger   btclog.Logger
)

func NewService(workingDir string) (*channeldb.DB, func(), error) {
	mu.Lock()
	defer mu.Unlock()
	if refCount == 0 {
		db, err := createService(workingDir)
		if err != nil {
			return nil, nil, err
		}
		chanDB = db
	}
	refCount++
	return chanDB, release, nil
}

func release() {
	mu.Lock()
	defer mu.Unlock()
	refCount--
	if refCount == 0 {
		chanDB.Close()
	}
}

func createService(workingDir string) (*channeldb.DB, error) {
	config, err := config.GetConfig(workingDir)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logBackend, err := log.GetLogBackend(workingDir)
		if err != nil {
			return nil, err
		}
		logger = logBackend.Logger("CHANNELDB")
		logger.SetLevel(btclog.LevelDebug)
	}
	logger.Infof("creating shared channeldb service.")
	graphDir := path.Join(workingDir, strings.Replace(directoryPattern, "{{network}}", config.Network, -1))
	chanDB, err := channeldb.Open(graphDir)
	if err != nil {
		logger.Errorf("unable to open channeldb: %v", err)
		return nil, err
	}

	logger.Infof("channeldb was opened successfuly")
	return chanDB, err
}
