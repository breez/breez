package breez

import (
	"crypto/rand"
	"encoding/hex"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
)

var (
	backupChan = make(chan interface{}, 10)
)

type backupFn func(files []string, nodeID, backupID string) error

// go routine message to set the backup function.
type setBackupFn struct {
	backupFn func(files []string, nodeID, backupID string) error
}

// go routine message to execute backup now
type backupNow struct {
	pendingID uint64
}

/*
RequestBackup push a request for the backup files of breez
*/
func RequestBackup() {

	// first thing push a pending backup request to the database so we
	// can recover in case of error.
	pendingID, err := breezDB.SetPendingBackup()
	if err != nil {
		log.Errorf("failed to set pending backup %v", err)
		notifyBackupFailed()
		return
	}

	select {
	case <-time.After(time.Second * 2):
	case <-quitChan:
		return
	}
	backupChan <- backupNow{pendingID: pendingID}
}

/*
SetBackupFn sets the external backup service that is responsible for uploading
the files
*/
func SetBackupFn(backup backupFn) {
	backupChan <- setBackupFn{
		backupFn: backup,
	}
}

// watchBackupRequests is the main go routine that listens to messages and update its state.
// basically there are two messages:
// 1. setBackupFn - to set the external backup service
// 2. backupNow - a request to backup.
func watchBackupRequests() {
	var backupService backupFn
	go func() {
		for {
			select {
			case req := <-backupChan:
				switch msg := req.(type) {
				case setBackupFn:
					backupService = msg.backupFn
				case backupNow:
					paths, nodeID, backupID, err := extractBackupInfo()
					if err != nil || backupService == nil {
						log.Errorf("error in backup %v", err)
						notifyBackupFailed()
						continue
					}
					if err := backupService(paths, nodeID, backupID); err != nil {
						log.Errorf("error in backup %v", err)
						notifyBackupFailed()
						continue
					}
					breezDB.ComitPendingBackup(msg.pendingID)
					log.Infof("backup finished succesfully")
					notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_SUCCESS}
				}
			case <-quitChan:
				return
			}
		}
	}()

	// execute recovery if needed.
	go runPendingBackup()
}

// runPendingBackup is responsible for running any pending backup requests that haven't
// been completed successfuly. We do that first thing on startup to ensure we don't miss any
// critical backups.
func runPendingBackup() {
	pendingID, err := breezDB.PendingBackup()
	if err != nil {
		notifyBackupFailed()
		return
	}
	if pendingID > 0 {
		backupChan <- backupNow{pendingID: pendingID}
	}
}

func notifyBackupFailed() {
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FAILED}
}

func breezdbCopy() (string, error) {
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
func extractBackupInfo() (paths []string, nodeID string, backupID string, err error) {
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
	backupIdentifier, err := getBackupIdentifier()
	if err != nil {
		return nil, "", "", err
	}

	f, err := breezdbCopy()
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
func getBackupIdentifier() (string, error) {
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
