package channeldbservice

import (
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/config"
	"github.com/breez/breez/log"
	"github.com/breez/breez/refcount"
	"github.com/btcsuite/btclog"
	"github.com/lightningnetwork/lnd/channeldb"
)

const (
	directoryPattern = "data/graph/{{network}}/"
	dbName           = "channel.db"
)

var (
	serviceRefCounter refcount.ReferenceCountable
	chanDB            *channeldb.DB
	logger            btclog.Logger
)

// Get returns a Ch
func Get(workingDir string) (db *channeldb.DB, cleanupFn func() error, err error) {
	service, release, err := serviceRefCounter.Get(
		func() (interface{}, refcount.ReleaseFunc, error) {
			return newService(workingDir)
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return service.(*channeldb.DB), release, err
}

func newService(workingDir string) (db *channeldb.DB, rel refcount.ReleaseFunc, err error) {
	chanDB, err = createService(workingDir)
	if err != nil {
		return nil, nil, err
	}
	return chanDB, release, err
}

func release() error {
	return chanDB.Close()
}

func compactDB(graphDir string) error {
	dbPath := path.Join(graphDir, dbName)
	f, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if f.Size() <= 200000000 {
		return nil
	}
	newFile, err := ioutil.TempFile(graphDir, "cdb-compact")
	if err != nil {
		return err
	}
	if err = chainservice.BoltCopy(dbPath, newFile.Name(),
		func(keyPath [][]byte, k []byte, v []byte) bool { return false }); err != nil {
		return err
	}
	if err = os.Rename(dbPath, dbPath+".old"); err != nil {
		return err
	}
	if err = os.Rename(newFile.Name(), dbPath); err != nil {
		logger.Criticalf("Error when renaming the new channeldb file: %v", err)
		return err
	}
	logger.Infof("channel.db was compacted because it's too big")
	return nil
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

	graphDir := path.Join(workingDir, strings.Replace(directoryPattern, "{{network}}", config.Network, -1))
	if err = compactDB(graphDir); err != nil {
		logger.Errorf("Error in compactDB: %v", err)
	}

	logger.Infof("creating shared channeldb service.")
	chanDB, err := channeldb.Open(graphDir,
		channeldb.OptionSetSyncFreelist(true))
	if err != nil {
		logger.Errorf("unable to open channeldb: %v", err)
		return nil, err
	}

	logger.Infof("channeldb was opened successfuly")
	return chanDB, err
}
