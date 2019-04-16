package sharedservices

import (
	"fmt"
	"path"
	"sync"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/breez/breez/db"
	"github.com/breez/lightninglib/channeldb"
	"github.com/lightninglabs/neutrino"
)

type factoryFunc func(workingDir string) (s interface{}, cleanup ReleaseFunc, err error)

// ReleaseFunc should be called by the client when he stopps to use the service
type ReleaseFunc func() error

// ServiceRegistry is the interface clients use to get services
type ServiceRegistry interface {
	ChainService() (*neutrino.ChainService, ReleaseFunc, error)
	ChannelDB() (*channeldb.DB, ReleaseFunc, error)
	BreezDB() (*db.DB, ReleaseFunc, error)
}

var (
	registry       *serviceRegistry
	initialization = sync.Once{}

	newNeutrino = func(workingDir string) (s interface{}, cleanup ReleaseFunc, err error) {
		return chainservice.NewService(workingDir)
	}
	newChanndelDB = func(workingDir string) (s interface{}, cleanup ReleaseFunc, err error) {
		return channeldbservice.NewService(workingDir)
	}
	newBreezDB = func(workingDir string) (s interface{}, cleanup ReleaseFunc, err error) {
		db, err := db.OpenDB(path.Join(workingDir, "breez.db"))
		return db, db.CloseDB, err
	}
)

// Registry returns the singleton ServiceRegistry instance
func Registry(workingDir string) ServiceRegistry {
	initialization.Do(func() {
		registry = &serviceRegistry{
			services:   make(map[string]*registration),
			workingDir: workingDir,
		}
		registry.services["chainservice"] = &registration{factFunc: newNeutrino}
		registry.services["channeldb"] = &registration{factFunc: newChanndelDB}
		registry.services["breezdb"] = &registration{factFunc: newBreezDB}
	})
	return registry
}

type registration struct {
	instance    interface{}
	refCount    int
	factFunc    factoryFunc
	releaseFunc ReleaseFunc
}

type serviceRegistry struct {
	mu         sync.Mutex
	services   map[string]*registration
	workingDir string
}

// ServiceRegistry interface implementation
func (s *serviceRegistry) ChainService() (*neutrino.ChainService, ReleaseFunc, error) {
	serv, r, err := s.get("chainservice")
	return serv.(*neutrino.ChainService), r, err
}

func (s *serviceRegistry) ChannelDB() (*channeldb.DB, ReleaseFunc, error) {
	serv, r, err := s.get("channeldb")
	return serv.(*channeldb.DB), r, err
}

func (s *serviceRegistry) BreezDB() (*db.DB, ReleaseFunc, error) {
	serv, r, err := s.get("breezdb")
	return serv.(*db.DB), r, err
}

func (s *serviceRegistry) get(key string) (service interface{}, cleanup ReleaseFunc, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	serviceContainer, ok := s.services[key]
	if !ok {
		return nil, nil, fmt.Errorf("service %s is not registered", key)
	}
	if serviceContainer.instance == nil {
		instance, releaseFunc, err := serviceContainer.factFunc(s.workingDir)
		if err != nil {
			return nil, nil, err
		}
		serviceContainer.instance = instance
		serviceContainer.releaseFunc = releaseFunc
	}
	serviceContainer.refCount++
	return serviceContainer.instance, func() error { return s.releaseService(key) }, nil
}

func (s *serviceRegistry) releaseService(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	serviceContainer, ok := s.services[key]
	if !ok {
		return fmt.Errorf("service %s is not registered", key)
	}

	if serviceContainer.refCount == 1 {
		if err := serviceContainer.releaseFunc(); err != nil {
			return err
		}
		serviceContainer.instance = nil
	}
	serviceContainer.refCount--
	return nil
}
