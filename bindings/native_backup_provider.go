package bindings

import (
	"strings"
	"encoding/json"

	"github.com/breez/breez/backup"
	"github.com/btcsuite/btclog"
)

// NativeBackupProvider is interface that serves as backup provider and is intended to be 
// implemented by the native platform and injected to breez library.
// It is usefull when it is more nature to implement the provider in the native environment
// rather than in go API.
type NativeBackupProvider interface {
	UploadBackupFiles(files string, nodeID string, encryptionType string) error
	AvailableSnapshots() (string, error)
	DownloadBackupFiles(nodeID, backupID string) (string, error)
}

// NativeBackupProviderBridge is a bridge for using a native implemented provider in the backup manager.
type NativeBackupProviderBridge struct {
	nativeProvider NativeBackupProvider
}

// UploadBackupFiles is called when files needs to be uploaded as part of the backup
func (b *NativeBackupProviderBridge) UploadBackupFiles(files []string, nodeID string, encryptionType string) error {
	return b.nativeProvider.UploadBackupFiles(strings.Join(files, ","), nodeID, encryptionType)
}

// AvailableSnapshots is called when querying for all available nodes backups.
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

// DownloadBackupFiles is called when restring from a backup snapshot.
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
