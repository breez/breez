package backup

import (
	"fmt"
	"sync"

	"github.com/breez/breez/data"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/daemon"
)

var (
	log = daemon.BackendLog().Logger("BCKP")
)

// Manager holds the data needed for the backup to execute its work.
type Manager struct {
	started           int32
	stopped           int32
	workingDir        string
	db                *db.DB
	provider          Provider
	backupRequestChan chan struct{}
	ntfnChan          chan data.NotificationEvent
	quitChan          chan struct{}
	wg                sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(
	providerName string,
	authService AuthService,
	db *db.DB,
	ntfnChan chan data.NotificationEvent,
	workingDir string) (*Manager, error) {

	provider, err := createBackupProvider(providerName, authService)
	if err != nil {
		return nil, err
	}

	return &Manager{
		db:                db,
		workingDir:        workingDir,
		ntfnChan:          ntfnChan,
		provider:          provider,
		backupRequestChan: make(chan struct{}, 10),
		quitChan:          make(chan struct{}),
	}, nil
}

func createBackupProvider(providerName string, authService AuthService) (Provider, error) {
	switch providerName {
	case "gdrive":
		return NewGoogleDriveProvider(authService)
	default:
		return nil, fmt.Errorf("provider not found for %v", providerName)
	}
}
