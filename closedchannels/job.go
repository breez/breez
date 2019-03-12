package closedchannels

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/breez/breez/channeldbservice"
	"github.com/breez/lightninglib/channeldb"
	"github.com/coreos/bbolt"
)

const (
	firstFileNumber = 5655
)

var (
	edgeBucket            = []byte("graph-edge")
	edgeIndexBucket       = []byte("edge-index")
	edgeUpdateIndexBucket = []byte("edge-update-index")
	closedBucket          = []byte("closed")
	lastImportedKey       = []byte("last-imported-file")
	closedIndexBucket     = []byte("closed-index")

	byteOrder = binary.BigEndian
)

/*
Run executes the download filter operation synchronousely
*/
func (s *Job) Run() error {
	s.wg.Add(1)
	defer s.wg.Done()

	var err error
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
	chanDB, chanDBCleanUp, err := channeldbservice.NewService(workingDir)
	if err != nil {
		fmt.Printf("Error creating channeldbservice: %v", err)
		return err
	}
	defer chanDBCleanUp()

	dirname := path.Join(s.workingDir, "pruned")
	for !s.terminated() {
		last, err := lastImportedFile(chanDB)
		if err != nil {
			return err
		}
		file, err := fileToImport(last, dirname)
		if err != nil {
			return err
		}
		if file == 0 {
			break
		}

		importClosedChannels(chanDB, dirname, file)

	}

	return nil
}

func lastImportedFile(chanDB *channeldb.DB) (uint, error) {
	var last uint
	err := chanDB.View(func(tx *bbolt.Tx) error {
		edges := tx.Bucket(edgeBucket)
		if edges == nil {
			return nil
		}
		closed := edges.Bucket(closedBucket)
		if closed == nil {
			return nil
		}
		lastB := closed.Get(lastImportedKey)
		if lastB == nil {
			return nil
		}
		r := bytes.NewReader(lastB)
		err := binary.Read(r, byteOrder, &last)
		return err
	})
	return last, err
}

func fileToImport(moreThan uint, dirname string) (uint, error) {
	var toImport uint
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
		if !f.IsDir() {
			if s, err := strconv.ParseUint(f.Name(), 10, 64); err == nil {
				if uint(s) > moreThan && (toImport == 0 || uint(s) < toImport) {
					toImport = uint(s)
				}
			}
		}
	}
	return toImport, nil
}

func importClosedChannels(chanDB *channeldb.DB, dirname string, file uint) error {
	fileContent, err := ioutil.ReadFile(path.Join(dirname, strconv.Itoa(int(file))))
	if err != nil {
		return err
	}
	_, err = chanDB.ChannelGraph().PruneClosedChannels(fileContent, file)
	return err
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

func firstFileNumberToDownload(dirname string) (uint, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return 0, err
	}
	list, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return 0, err
	}
	n := uint(firstFileNumber)
	for _, f := range list {
		if !f.IsDir() {
			if s, err := strconv.ParseUint(f.Name(), 10, 64); err == nil {
				if uint(s) >= n {
					n = uint(s) + 1
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
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return http.StatusOK, err
	}
	err = os.Rename(out.Name(), filename)
	return http.StatusOK, err
}
