package breez

import (
	"io/ioutil"

	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
)

/*
Backup creates the backup files of breez and send notification when ready
*/
func Backup() error {
	return extractBackupPaths()
}

func breezdbCopy() (string, error) {
	dir, err := ioutil.TempDir("", "backup")
	if err != nil {
		return "", err
	}
	return backupDb(dir)
}

func extractBackupPaths() error {
	response, err := lightningClient.GetBackup(context.Background(), &lnrpc.GetBackupRequest{})
	if err != nil {
		log.Errorf("Couldn't get backup: %v", err)
		return err
	}
	f, err := breezdbCopy()
	if err != nil {
		log.Errorf("Couldn't get breez backup file: %v", err)
		return err
	}
	files := append(response.Files, f)
	log.Infof("Database backed up: %v", response.Files)
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FILES_AVAILABLE, Data: files}
	return nil
}
