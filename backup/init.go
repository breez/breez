package backup

import (
	"fmt"
	"path"
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/daemon"
)

// ProviderFactory is a factory for create a specific provider.
// This is the function needed to be implemented for a new provider
// to be registered and used.
type ProviderFactory func(authService AuthService) (Provider, error)

var (
	log              = daemon.BackendLog().Logger("BCKP")
	providersFactory = map[string]ProviderFactory{
		"gdrive": func(authService AuthService) (Provider, error) {
			return NewGoogleDriveProvider(authService)
		},
	}
)

// Manager holds the data needed for the backup to execute its work.
type Manager struct {
	started           int32
	stopped           int32
	workingDir        string
	db                *backupDB
	provider          Provider
	prepareBackupData DataPreparer
	config            *config.Config
	backupRequestChan chan struct{}
	onServiceEvent    func(event data.NotificationEvent)
	quitChan          chan struct{}
	wg                sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(
	providerName string,
	authService AuthService,
	onServiceEvent func(event data.NotificationEvent),
	prepareData DataPreparer,
	config *config.Config,
	workingDir string) (*Manager, error) {

	provider, err := createBackupProvider(providerName, authService)
	if err != nil {
		return nil, err
	}

	db, err := openDB(path.Join(workingDir, "backup.db"))
	if err != nil {
		return nil, err
	}

	return &Manager{
		db:                db,
		workingDir:        workingDir,
		onServiceEvent:    onServiceEvent,
		provider:          provider,
		prepareBackupData: prepareData,
		config:            config,
		backupRequestChan: make(chan struct{}, 10),
		quitChan:          make(chan struct{}),
	}, nil
}

// RegisterProvider registers a backup provider with a unique name
func RegisterProvider(providerName string, factory ProviderFactory) {
	providersFactory[providerName] = factory
}

func createBackupProvider(providerName string, authService AuthService) (Provider, error) {
	factory, ok := providersFactory[providerName]
	if !ok {
		return nil, fmt.Errorf("provider not found for %v", providerName)
	}
	return factory(authService)
}
