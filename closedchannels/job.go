package closedchannels

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/breez/breez/chainservice"
	"github.com/breez/breez/channeldbservice"
	"github.com/lightningnetwork/lnd/channeldb"
)

const (
	firstFileNumber = 5655
	deletedSuffix   = ".deleted"
)

/*
Run executes the download filter operation synchronousely
*/
func (s *Job) Run() error {
	s.wg.Add(1)
	defer s.wg.Done()
	bootstrapped, err := chainservice.Bootstrapped(s.workingDir)
	if err != nil {
		return err
	}
	if !bootstrapped {
		s.log.Info("closed channels started needs bootstrap, skiping job")
		return nil
	}

	err = s.downloadClosedChannels()
	if err != nil {
		return fmt.Errorf("download closed channels finished with error %v", err)
	}

	if !s.terminated() {
		err = s.importAndPruneClosedChannels(s.workingDir)
		if err != nil {
			return fmt.Errorf("An error occured when importing & prunning closed channels %v", err)
		}
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

func (s *Job) importAndPruneClosedChannels(workingDir string) error {
	chanDB, chanDBCleanUp, err := channeldbservice.Get(workingDir)
	if err != nil {
		s.log.Infof("Error creating channeldbservice: %v", err)
		return err
	}
	defer chanDBCleanUp()

	dirname := path.Join(s.workingDir, "pruned")
	channelPruned := false
	last := uint64(0)
	for !s.terminated() {
		file, err := fileToImport(last, dirname)
		s.log.Infof("Result of fileToImport %v %v", file, err)
		if err != nil {
			return err
		}
		if file == 0 {
			break
		}
		channelPruned = true
		err = s.importClosedChannels(chanDB, dirname, file)
		s.log.Infof("Result of importClosedChannels %v", err)
		last = file // even if there is an error. We don't want to loop forever
	}
	if !s.terminated() && channelPruned {
		return chanDB.ChannelGraph().PruneGraphNodes()
	}

	return nil
}

func fileToImport(moreThan uint64, dirname string) (uint64, error) {
	var toImport uint64
	f, err := os.Open(dirname)
	if err != nil {
		return 0, err
	}
	list, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return 0, err
	}
	for _, f := range list {
		if !f.IsDir() && !strings.HasSuffix(f.Name(), deletedSuffix) {
			if s, err := strconv.ParseUint(f.Name(), 10, 64); err == nil {
				if s > moreThan && (toImport == 0 || s < toImport) {
					toImport = s
				}
			}
		}
	}
	return toImport, nil
}

func (s *Job) importClosedChannels(chanDB *channeldb.DB, dirname string, file uint64) error {
	filename := path.Join(dirname, strconv.Itoa(int(file)))
	fileContent, err := ioutil.ReadFile(filename)
	s.log.Infof("Result of ReadFile %v %v", len(fileContent), err)
	if err != nil {
		return err
	}
	if len(fileContent)%8 != 0 {
		return fmt.Errorf("Bad file")
	}
	chanIds := make([]uint64, 0, len(fileContent))
	for i := 0; i < len(fileContent); i += 8 {
		chanID := binary.BigEndian.Uint64(fileContent[i : i+8])
		_, _, exists, _, _ := chanDB.ChannelGraph().HasChannelEdge(chanID)
		if exists {
			chanIds = append(chanIds, chanID)
		}
	}
	err = chanDB.ChannelGraph().DeleteChannelEdges(true, true, chanIds...)
	if err != nil {
		s.log.Infof("DeleteChannelEdges error: %v", err)
		return err
	}
	return os.Rename(filename, filename+deletedSuffix)
}

func (s *Job) downloadClosedChannels() error {

	directory := path.Join(s.workingDir, "pruned")
	os.MkdirAll(directory, 0755)

	f, err := firstFileNumberToDownload(directory)
	if err != nil {
		return err
	}
	var filename string
	statusCode := http.StatusOK
	s.log.Infof("Downloading: %v", f)
	for ; err == nil && statusCode == http.StatusOK && !s.terminated(); f++ {
		filename = strconv.Itoa(int(f))
		s.log.Infof("Downloading: %v from %v", path.Join(directory, filename), s.config.ClosedChannelsURL+"/"+filename)
		statusCode, err = downloadFile(path.Join(directory, filename), s.config.ClosedChannelsURL+"/"+filename)
		s.log.Infof("Download result: %v %v", statusCode, err)
	}

	return err
}

func firstFileNumberToDownload(dirname string) (uint64, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return 0, err
	}
	list, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return 0, err
	}
	n := uint64(firstFileNumber)
	for _, f := range list {
		if !f.IsDir() {
			filename := f.Name()
			if strings.HasSuffix(filename, deletedSuffix) {
				filename = filename[0 : len(filename)-len(deletedSuffix)]
			}
			if s, err := strconv.ParseUint(filename, 10, 64); err == nil {
				if s >= n {
					n = s
				}
			}
		}
	}
	return n, nil
}

func downloadFile(filename string, url string) (int, error) {
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}

	// Create a temporary file
	out, err := ioutil.TempFile(filepath.Dir(filename), "")
	if err != nil {
		return 0, err
	}
	defer os.Remove(out.Name())
	defer out.Close()

	// Write the body to file
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return http.StatusOK, err
	}
	fi, err := os.Stat(filename + deletedSuffix)
	if err == nil {
		if fi.Size() == written {
			return http.StatusOK, nil
		} else {
			os.Rename(filename+deletedSuffix, filename)
		}
	}
	err = os.Rename(out.Name(), filename)
	return http.StatusOK, err
}
