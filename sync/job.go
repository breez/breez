package sync

import (
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/neutrino"
)

/*
Start starts the go routine for downloading the filters.
*/
func (s *Job) Start() error {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return nil
	}

	db, chainService, err := newNeutrino(s.workingDir, s.network, &s.config)
	if err != nil {
		s.log.Errorf("Error creating ChainService: %s", err)
		return err
	}
	neutrino.UseLogger(s.log)

	s.db = db
	s.neutrino = chainService

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := s.syncFilters()
		if err != nil {
			s.log.Infof("sync finished with error %v", err)
		}
		s.terminate()
	}()

	return nil
}

/*
Stop stops neutrino instance and wait for the syncFitlers to complete
*/
func (s *Job) Stop() error {
	if err := s.terminate(); err != nil {
		s.log.Error("Error terminating job")
		return err
	}
	s.WaitForShutdown()
	return nil
}

/*
WaitForShutdown blocks until the job completes gracefully
*/
func (s *Job) WaitForShutdown() {
	s.wg.Wait()
}

func (s *Job) terminate() error {
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		return nil
	}

	if err := s.neutrino.Stop(); err != nil {
		s.log.Error("Failed to stop neutrino from job")
	}
	if err := s.db.Close(); err != nil {
		s.log.Error("Failed close db from job")
		return err
	}

	s.log.Infof("Job terminated succesfully")
	return nil
}

func (s *Job) syncFilters() (err error) {
	chainService := s.neutrino

	//get the best block before starting neutrino
	//this will be the start point of the filters sync
	startBlock, err := chainService.BestBlock()
	if err != nil {
		return err
	}
	chainService.Start()
	defer func() {
		err = chainService.Stop()
	}()

	//must wait for neutrino to connect to a peer and download the best
	//block before starting the filters download loop.
	for i := 0; i < 50 && chainService.IsCurrent(); i++ {
		time.Sleep(time.Millisecond * 100)
	}

	//get the best block after letting neutrino some time
	//to connect to its peers.
	bestBlock, err := chainService.BestBlock()
	if err != nil {
		return err
	}

	s.log.Infof("Starting sync job from height: %v", startBlock.Height)

	//save last block in db
	for currentHeight := startBlock.Height; currentHeight <= bestBlock.Height; currentHeight++ {
		h, err := chainService.GetBlockHash(int64(currentHeight))
		if err != nil {
			s.log.Errorf("fail to fetch block hash", err)
			return err
		}
		_, err = chainService.GetCFilter(*h, wire.GCSFilterRegular, neutrino.PersistToDisk())
		if err != nil {
			s.log.Errorf("fail to download block filter", err)
			return err
		}

		//wait for the backend to sync if needed
		for currentHeight == bestBlock.Height && !chainService.IsCurrent() {
			time.Sleep(100 * time.Millisecond)
			bestBlock, err = chainService.BestBlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
