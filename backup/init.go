package backup

import (
	"fmt"
	"path"
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/btcsuite/btclog"
)

// ProviderFactory is a factory for create a specific provider.
// This is the function needed to be implemented for a new provider
// to be registered and used.
type ProviderFactory func(authService AuthService, log btclog.Logger) (Provider, error)

var (
	providersFactory = map[string]ProviderFactory{
		"gdrive": func(authService AuthService, log btclog.Logger) (Provider, error) {
			return NewGoogleDriveProvider(authService, log)
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
	authService 	  AuthService
	prepareBackupData DataPreparer
	config            *config.Config
	backupRequestChan chan struct{}
	onServiceEvent    func(event data.NotificationEvent)
	quitChan          chan struct{}
	log               btclog.Logger
	encryptionKey     []byte
	encryptionType    string
	mu                sync.Mutex
	wg                sync.WaitGroup
}

// NewManager creates a new Manager
func NewManager(
	providerName string,
	authService AuthService,
	onServiceEvent func(event data.NotificationEvent),
	prepareData DataPreparer,
	config *config.Config,
	log btclog.Logger) (*Manager, error) {

	var provider Provider
	var err error
	if providerName != "" {
		provider, err = createBackupProvider(providerName, authService, log)
		if err != nil {
			return nil, err
		}
	}

	db, err := openDB(path.Join(config.WorkingDir, "backup.db"))
	if err != nil {
		return nil, err
	}

	return &Manager{
		db:                db,
		workingDir:        config.WorkingDir,
		onServiceEvent:    onServiceEvent,
		provider:          provider,
		prepareBackupData: prepareData,
		config:            config,
		log:               log,
		authService: 	   authService,
		backupRequestChan: make(chan struct{}, 10),
		quitChan:          make(chan struct{}),
	}, nil
}

// RegisterProvider registers a backup provider with a unique name
func RegisterProvider(providerName string, factory ProviderFactory) {
	providersFactory[providerName] = factory
}

func createBackupProvider(providerName string, authService AuthService, log btclog.Logger) (Provider, error) {
	factory, ok := providersFactory[providerName]
	if !ok {
		return nil, fmt.Errorf("provider not found for %v", providerName)
	}
	return factory(authService, log)
}
