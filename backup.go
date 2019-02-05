package breez

import (
	"crypto/rand"
	"encoding/hex"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
)

// BackupUploader defines the methods needs to be implemented by an external service
// in order to save the backuped up files.
type BackupUploader interface {
	UploadBackupFiles(files string, nodeID, backupID string) error
}

// BackupManager holds the data needed for the backup to execute its work.
type BackupManager struct {
	started           int32
	stopped           int32
	uploader          BackupUploader
	backupRequestChan chan uint64
	quitChan          chan struct{}
	wg                sync.WaitGroup
}

// NewBackupService creates a new BackupService
func NewBackupManager(uploader BackupUploader) *BackupManager {
	return &BackupManager{
		uploader:          uploader,
		backupRequestChan: make(chan uint64, 10),
		quitChan:          make(chan struct{}),
	}
}

/*
RequestBackup push a request for the backup files of breez
*/
func (b *BackupManager) RequestBackup() {

	// first thing push a pending backup request to the database so we
	// can recover in case of error.
	pendingID, err := breezDB.SetPendingBackup()
	if err != nil {
		log.Errorf("failed to set pending backup %v", err)
		b.notifyBackupFailed()
		return
	}

	select {
	case <-time.After(time.Second * 2):
	case <-b.quitChan:
		return
	}
	b.backupRequestChan <- pendingID
}

// GetBackupIdentifier returns the backup identifier unique for this breez instance
func (b *BackupManager) GetBackupIdentifier() (string, error) {
	return b.getBackupIdentifier()
}

// Start is the main go routine that listens to backup requests and is resopnsible for executing it.
func (b *BackupManager) Start() {
	if atomic.SwapInt32(&b.started, 1) == 1 {
		return
	}

	b.wg.Add(2)
	go func() {
		defer b.wg.Done()

		for {
			select {
			case pendingID := <-b.backupRequestChan:
				paths, nodeID, backupID, err := b.prepareBackupInfo()
				if err != nil {
					log.Errorf("error in backup %v", err)
					b.notifyBackupFailed()
					continue
				}
				if err := b.uploader.UploadBackupFiles(strings.Join(paths, ","), nodeID, backupID); err != nil {
					log.Errorf("error in backup %v", err)
					b.notifyBackupFailed()
					continue
				}
				breezDB.ComitPendingBackup(pendingID)
				log.Infof("backup finished succesfully")
				notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_SUCCESS}
			case <-b.quitChan:
				return
			}
		}
	}()

	// execute recovery if needed.
	go b.runPendingBackup()
}

// Stop stops the BackupService and wait for complete shutdown.
func (b *BackupManager) Stop() {
	if atomic.SwapInt32(&b.stopped, 1) == 1 {
		return
	}

	close(quitChan)
	b.wg.Wait()
}

// runPendingBackup is responsible for running any pending backup requests that haven't
// been completed successfuly. We do that first thing on startup to ensure we don't miss any
// critical backups.
func (b *BackupManager) runPendingBackup() {
	defer b.wg.Done()

	pendingID, err := breezDB.PendingBackup()
	if err != nil {
		b.notifyBackupFailed()
		return
	}
	if pendingID > 0 {
		b.backupRequestChan <- pendingID
	}
}

func (b *BackupManager) notifyBackupFailed() {
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FAILED}
}

func (b *BackupManager) breezdbCopy() (string, error) {
	dir, err := ioutil.TempDir("", "backup")
	if err != nil {
		return "", err
	}
	return breezDB.BackupDb(dir)
}

// extractBackupInfo extracts the information that is needed for the external backup service:
// 1. paths - the files need to be backed up.
// 2. nodeID - the current lightning node id.
// 3. backupID - an identifier for this instance of breez. It is needed for conflict detection.
func (b *BackupManager) prepareBackupInfo() (paths []string, nodeID string, backupID string, err error) {
	log.Infof("extractBackupInfo started")
	response, err := lightningClient.GetBackup(context.Background(), &lnrpc.GetBackupRequest{})
	if err != nil {
		log.Errorf("Couldn't get backup: %v", err)
		return nil, "", "", err
	}
	info, err := lightningClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		return nil, "", "", err
	}
	backupIdentifier, err := b.getBackupIdentifier()
	if err != nil {
		return nil, "", "", err
	}

	f, err := b.breezdbCopy()
	if err != nil {
		log.Errorf("Couldn't get breez backup file: %v", err)
		return nil, "", "", err
	}
	files := append(response.Files, f)
	log.Infof("extractBackupInfo completd")
	return files, info.IdentityPubkey, backupIdentifier, nil
}

// getBackupIdentifier retrieves an identifier that is unique for this instance of breez.
// We use is as a mechanism for conflict detection, in case a restore was done on some
// other device.
// The identifier is generated once and save in a file, we can't save it in the db as it
// is backed up and would cause the restored node to have the same identifier...
func (b *BackupManager) getBackupIdentifier() (string, error) {
	backupDir := path.Join(appWorkingDir, "backup")
	if err := os.MkdirAll(backupDir, os.ModePerm); err != nil {
		return "", err
	}
	backupFile := path.Join(backupDir, "breez_backup_id")
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		var id [32]byte
		_, err = rand.Read(id[:])
		if err != nil {
			return "", err
		}
		if err = ioutil.WriteFile(backupFile, id[:], os.ModePerm); err != nil {
			return "", err
		}
	}
	id, err := ioutil.ReadFile(backupFile)
	if err != nil {
		return "", err
	}
	return "backup-id-" + hex.EncodeToString(id), nil
}
