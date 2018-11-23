package breez

import (
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
	"path/filepath"
)

func getBackupPath() error {
	response, err := lightningClient.GetBackup(context.Background(), &lnrpc.GetBackupRequest{})
	if err != nil {
		log.Errorf("Couldn't get backup: %v", err)
		return err
	}
	log.Infof("Database backed up: %v", filepath.Dir(response.Files[0]))
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FILES_AVAILABLE, Data: filepath.Dir(response.Files[0])}
	return nil
}
