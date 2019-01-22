package sync

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/breez/breez/chainservice"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightninglabs/neutrino"
)

/*
Run executes the download filter operation synchronousely
*/
func (s *Job) Run() error {
	s.wg.Add(1)
	defer s.wg.Done()

	err := s.syncFilters()
	if err != nil {
		return fmt.Errorf("sync finished with error %v", err)
	}
	s.terminate()
	return nil
}

/*
Stop stops neutrino instance and wait for the syncFitlers to complete
*/
func (s *Job) Stop() {
	s.terminate()
	s.wg.Wait()
}

func (s *Job) terminate() {
	if atomic.AddInt32(&s.shutdown, 1) == 1 {
		close(s.quit)
	}
}

func (s *Job) terminated() bool {
	select {
	case <-s.quit:
		return true
	default:
		return false
	}
}

func (s *Job) syncFilters() (err error) {
	s.log.Info("syncFilters started...")
	chainService, err := chainservice.GetInstance(s.workingDir)
	if err != nil {
		s.log.Errorf("Error creating ChainService: %s", err)
		return err
	}
	//get the best block before starting neutrino
	//this will be the start point of the filters sync
	startBlock, err := chainService.BestBlock()
	if err != nil {
		return err
	}
	chainService.Start()

	//must wait for neutrino to connect to a peer and download the best
	//block before starting the filters download loop.
	for i := 0; i < 50 && chainService.IsCurrent(); i++ {
		select {
		case <-time.After(time.Millisecond * 100):
			continue
		case <-s.quit:
			return nil
		}
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
		if s.terminated() {
			return nil
		}

		// Get block hash
		h, err := chainService.GetBlockHash(int64(currentHeight))
		if err != nil {
			s.log.Errorf("fail to fetch block hash", err)
			return err
		}
		if s.terminated() {
			return nil
		}

		// Get filter
		_, err = chainService.GetCFilter(*h, wire.GCSFilterRegular, neutrino.PersistToDisk())
		if err != nil {
			s.log.Errorf("fail to download block filter", err)
			return err
		}
		if s.terminated() {
			return nil
		}

		//wait for the backend to sync if needed
		for currentHeight == bestBlock.Height && !chainService.IsCurrent() {
			select {
			case <-time.After(time.Millisecond * 100):
				bestBlock, err = chainService.BestBlock()
				if err != nil {
					return err
				}
			case <-s.quit:
				return nil
			}
		}
	}
	s.log.Info("syncFilters completed succesfully")
	return nil
}
