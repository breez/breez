package breez

import (
	"context"

	"github.com/breez/lightninglib/lnrpc"
)

func watchBackupEvents() {
	stream, err := lightningClient.SubscribeBackupEvents(context.Background(), &lnrpc.BackupEventSubscription{})
	if err != nil {
		log.Criticalf("Failed to call SubscribeBackupEvents %v, %v", stream, err)
	}
	log.Infof("Backup events subscription created")
	for {
		_, err := stream.Recv()
		log.Infof("watchBackupEvents received new event")
		if err != nil {
			log.Errorf("watchBackupEvents failed to receive a new event: %v, %v", stream, err)
			return
		}
		RequestBackup()
	}
}
