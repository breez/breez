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
	select {
	case <-time.After(time.Second * 2):
	case <-quitChan:
		return
	}
	pendingID, err := breezDB.SetPendingBackup()
	if err != nil {
		log.Errorf("failed to set pending backup %v", err)
		notifyBackupFailed()
		return
	}
	backupChan <- backupNow{pendingID: pendingID}
}

/*
SetBackupFn sets the backup service that executes the backup
*/
func SetBackupFn(backup backupFn) {
	backupChan <- setBackupFn{
		backupFn: backup,
	}
}

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

	go runPendingBackup()
}

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
