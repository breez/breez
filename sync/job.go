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

const (
	rateLimitJobInterval = time.Duration(time.Minute * 10)
)

/*
Run executes the download filter operation synchronousely
*/
func (s *Job) Run() (channelClosed bool, err error) {
	s.wg.Add(1)
	defer s.wg.Done()

	res, err := s.syncFilters()
	if err != nil {
		return res, fmt.Errorf("sync finished with error %v", err)
	}
	s.terminate()
	return res, nil
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

func (s *Job) syncFilters() (channelClosed bool, err error) {
	s.log.Info("syncFilters started...")
	chainService, cleanFn, err := chainservice.Get(s.workingDir)
	if err != nil {
		s.log.Errorf("Error creating ChainService: %s", err)
		return false, err
	}
	defer cleanFn()
	jobDB, err := openJobDB(path.Join(s.workingDir, "job.db"))
	if err != nil {
		return false, err
	}
	defer jobDB.close()

	// ensure job is rate limited.
	lastRun, err := jobDB.lastSuccessRunDate()
	if err != nil {
		return false, err
	}
	if time.Now().Sub(lastRun) < rateLimitJobInterval {
		s.log.Infof("job was not running due to rate limit, last run was at %v", lastRun)
		return false, nil
	}

	startSyncHeight, err := jobDB.fetchCFilterSyncHeight()
	if err != nil {
		return false, err
	}

	chainService.Start()
	s.log.Infof("Starting sync job from height: %v", startSyncHeight)

	bestBlockHeight, err := s.waitForHeaders(chainService, startSyncHeight)
	if err != nil {
		return false, err
	}

	if startSyncHeight == 0 {
		startSyncHeight = bestBlockHeight
	}

	//must wait for neutrino to connect to a peer and download the best
	//block before starting the filters download loop.
	for i := 0; i < 50 && startSyncHeight == bestBlockHeight; i++ {
		select {
		case <-time.After(time.Millisecond * 100):
			bestBlockHeight, err = s.waitForHeaders(chainService, startSyncHeight)
			if err != nil {
				return false, err
			}
			continue
		case <-s.quit:
			return false, nil
		}
	}

	s.log.Infof("Finished waiting for neutrino to sync best block: %v", bestBlockHeight)

	for currentHeight := startSyncHeight; currentHeight <= bestBlockHeight; currentHeight++ {
		if s.terminated() {
			return false, nil
		}

		// Get block hash
		h, err := chainService.GetBlockHash(int64(currentHeight))
		if err != nil {
			s.log.Errorf("fail to fetch block hash", err)
			return false, err
		}
		if s.terminated() {
			return false, nil
		}

		// Get filter
		_, err = chainService.GetCFilter(*h, wire.GCSFilterRegular, neutrino.PersistToDisk())
		if err != nil {
			s.log.Errorf("fail to download block filter", err)
			return false, err
		}
		err = jobDB.setCFilterSyncHeight(currentHeight)
		if err != nil {
			return false, err
		}

		if s.terminated() {
			return false, nil
		}

		//wait for the backend to sync if needed
		bestBlockHeight, err = s.waitForHeaders(chainService, currentHeight)
		if err != nil {
			return false, err
		}
	}
	s.log.Info("syncFilters completed succesfully, checking for close channels...")
	channelsWatcher, err := NewChannelsWatcher(s.workingDir, chainService, s.log, jobDB, s.quit)
	if err != nil {
		return false, err
	}
	channelClosedDetected, err := channelsWatcher.Scan(bestBlockHeight)
	if err != nil {
		return false, err
	}

	return channelClosedDetected, jobDB.setLastSuccessRunDate(time.Now())
}

func (s *Job) waitForHeaders(chainService *neutrino.ChainService, currentHeight uint64) (uint64, error) {
	s.log.Infof("Waiting for headers from height: %v", currentHeight)
	bestBlock, err := chainService.BestBlock()
	if err != nil {
		return 0, err
	}
	bestBlockHeight := uint64(bestBlock.Height)

	for currentHeight == bestBlockHeight && !chainService.IsCurrent() {
		s.log.Infof("Waiting for headers bestBlockHeight=%v", bestBlockHeight)
		select {
		case <-time.After(time.Millisecond * 100):
			bestBlock, err := chainService.BestBlock()
			if err != nil {
				return 0, err
			}
			bestBlockHeight = uint64(bestBlock.Height)
		case <-s.quit:
			return 0, nil
		}
	}
	return bestBlockHeight, nil
}
