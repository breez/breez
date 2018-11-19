package breez

import (
	"github.com/breez/breez/data"
	"github.com/breez/lightninglib/lnrpc"
	"golang.org/x/net/context"
)

func updateBackupPaths() error {
	response, err := lightningClient.GetBackup(context.Background(), &lnrpc.GetBackupRequest{})
	if err != nil {
		log.Errorf("Couldn't get backup: %v", err)
		return err
	}
	log.Infof("Database backed up: %v", response.Files)
	notificationsChan <- data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FILES_AVAILABLE, Data: response.Files[0]}
	return nil
}