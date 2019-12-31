package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/breez/breez/config"
	"github.com/btcsuite/btclog"
	"github.com/jrick/logrotate/rotator"
)

var (
	initBackend sync.Once
	logBackend  *btclog.Backend
	logWriter   *io.PipeWriter
	initError   error
)

/*
Writer is the implementatino of io.Writer interface required for logging
*/
type Writer struct {
	writer io.Writer
}

func (w *Writer) Write(b []byte) (int, error) {
	//os.Stdout.Write(b)
	if w.writer != nil {
		w.writer.Write(b)
	}
	return len(b), nil
}

/*
GetLogBackend ensure log backend is initialized and return the LogBackend singleton.
*/
func GetLogBackend(workingDir string) (*btclog.Backend, error) {
	initLog(workingDir)
	return logBackend, initError
}

/*
GetLogWriter ensure log backend is initialized and return the writer singleton.
This writer is sent to other systems to they can use the same log file.
*/
func GetLogWriter(workingDir string) (*io.PipeWriter, error) {
	initLog(workingDir)
	return logWriter, initError
}

func initLog(workingDir string) {
	initBackend.Do(func() {
		cfg, err := config.GetConfig(workingDir)
		if err != nil {
			initError = err
			return
		}

		filename := workingDir + "/logs/bitcoin/" + cfg.Network + "/lnd.log"
		logWriter, _, initError = initLogRotator(filename, 10, 3)
		if initError == nil {
			logBackend = btclog.NewBackend(&Writer{logWriter})
		}
	})
}

func initLogRotator(logFile string, MaxLogFileSize int, MaxLogFiles int) (*io.PipeWriter, *rotator.Rotator, error) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log directory: %v", err)
	}
	r, err := rotator.New(logFile, int64(MaxLogFileSize*1024), false, MaxLogFiles)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create file rotator: %v", err)
	}

	pr, pw := io.Pipe()
	go r.Run(pr)

	return pw, r, nil
}
