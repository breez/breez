package backup

import (
	"sync"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/daemon"
	"github.com/breez/lightninglib/lnrpc"
)

var (
	log = daemon.BackendLog().Logger("BCKP")
)

// Uploader defines the methods needs to be implemented by an external service
// in order to save the backuped up files.
type Uploader interface {
	UploadBackupFiles(files string, nodeID, backupID string) error
}

// Manager holds the data needed for the backup to execute its work.
type Manager struct {
	started           int32
	stopped           int32
	workingDir        string
	db                *db.DB
	uploader          Uploader
	backupRequestChan chan uint64
	lightningClient   lnrpc.LightningClient
	ntfnChan          chan data.NotificationEvent
	quitChan          chan struct{}
	wg                sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(
	uploader Uploader,
	db *db.DB,
	ntfnChan chan data.NotificationEvent,
	lightningClient lnrpc.LightningClient,
	workingDir string) *Manager {

	return &Manager{
		db:                db,
		workingDir:        workingDir,
		lightningClient:   lightningClient,
		ntfnChan:          ntfnChan,
		uploader:          uploader,
		backupRequestChan: make(chan uint64, 10),
		quitChan:          make(chan struct{}),
	}
}
