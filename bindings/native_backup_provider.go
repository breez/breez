package bindings

import (
	"strings"
	"encoding/json"

	"github.com/breez/breez/backup"
	"github.com/btcsuite/btclog"
)

type NativeBackupProvider interface {
	UploadBackupFiles(files string, nodeID string, encryptionType string) error
	AvailableSnapshots() (string, error)
	DownloadBackupFiles(nodeID, backupID string) (string, error)
}

// NativeBackupProviderBridge is a bridge for using a native implemented provider in the backup manager.
type NativeBackupProviderBridge struct {
	nativeProvider NativeBackupProvider
}

func (b *NativeBackupProviderBridge) UploadBackupFiles(files []string, nodeID string, encryptionType string) error {
	return b.nativeProvider.UploadBackupFiles(strings.Join(files, ","), nodeID, encryptionType)
}

func (b *NativeBackupProviderBridge) AvailableSnapshots() ([]backup.SnapshotInfo, error) {
	snapshotsString, err := b.nativeProvider.AvailableSnapshots()
	if err != nil {
		return nil, err
	}

	var snapshots []backup.SnapshotInfo
	if err := json.Unmarshal([]byte(snapshotsString), &snapshots); err != nil {
		return nil, err
	}

	return snapshots, nil
}

func (b *NativeBackupProviderBridge) DownloadBackupFiles(nodeID, backupID string) ([]string, error) {
	files, err := b.nativeProvider.DownloadBackupFiles(nodeID, backupID)
	if err != nil {
		return nil, err
	}
	return strings.Split(files, ","), nil
}

// RegisterNativeBackupProvider registered a native backup provider
func RegisterNativeBackupProvider(name string, provider NativeBackupProvider) {
	backup.RegisterProvider(name, func(authService backup.AuthService, log btclog.Logger) (backup.Provider, error) {
		return &NativeBackupProviderBridge{nativeProvider: provider}, nil
	})
}
