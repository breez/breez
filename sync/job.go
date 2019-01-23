package sync

import (
	"fmt"
	"path"
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
	jobDB, err := openJobDB(path.Join(s.workingDir, "job.db"))
	if err != nil {
		return err
	}
	defer jobDB.close()

	startSyncHeight, err := jobDB.fetchCFilterSyncHeight()
	if err != nil {
		return err
	}
	//get the best block before starting neutrino
	bestBlock, err := chainService.BestBlock()
	if err != nil {
		return err
	}
	bestBlockHeight := uint64(bestBlock.Height)
	if startSyncHeight == 0 {
		startSyncHeight = bestBlockHeight
	}

	chainService.Start()
	s.log.Infof("Starting sync job from height: %v", startSyncHeight)

	//save last block in db
	for currentHeight := startSyncHeight; currentHeight <= bestBlockHeight; currentHeight++ {
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
		err = jobDB.setCFilterSyncHeight(currentHeight)
		if err != nil {
			return err
		}

		if s.terminated() {
			return nil
		}

		//wait for the backend to sync if needed
		for currentHeight == bestBlockHeight && !chainService.IsCurrent() {
			select {
			case <-time.After(time.Millisecond * 100):
				bestBlock, err = chainService.BestBlock()
				if err != nil {
					return err
				}
				bestBlockHeight = uint64(bestBlock.Height)
			case <-s.quit:
				return nil
			}
		}
	}
	s.log.Info("syncFilters completed succesfully")
	return nil
}
