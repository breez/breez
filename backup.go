package breez

import (
	"crypto/rand"
	"io/ioutil"
	"os"
	"path"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
)

var (
	backupChan chan interface{}
)

type backupFn func(files []string, nodeID, backupID string) error

// go routine message to set the backup function.
type setBackupFn struct {
	backupFn func(files []string, nodeID, backupID string) error
}

// go routine message to do backup now
type backupNow struct {
}

/*
RequestBackup push a request for the backup files of breez
*/
func RequestBackup() {
	backupChan <- backupNow{}
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
	backupChan = make(chan interface{})
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
						notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FAILED}
						continue
					}
					if err := backupService(paths, nodeID, backupID); err != nil {
						notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FAILED}
						continue
					}
					//db.CompleteBackup()
					notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_SUCCESS}
				}
			case <-quitChan:
				return
			}
		}
	}()
}

func breezdbCopy() (string, error) {
	dir, err := ioutil.TempDir("", "backup")
	if err != nil {
		return "", err
	}
	return breezDB.BackupDb(dir)
}

func extractBackupInfo() (paths []string, nodeID string, backupID string, err error) {
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
	return "backup-id-" + string(id), nil
}
