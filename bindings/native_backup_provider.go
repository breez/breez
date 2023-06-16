package bindings

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/breez/breez/backup"
	"github.com/breez/breez/tor"
)

// NativeBackupProvider is interface that serves as backup provider and is intended to be
// implemented by the native platform and injected to breez library.
// It is usefull when it is more nature to implement the provider in the native environment
// rather than in go API.
type NativeBackupProvider interface {
	UploadBackupFiles(file string, nodeID string, encryptionType string, timestamp string) (string, error)
	AvailableSnapshots() (string, error)
	DownloadBackupFiles(nodeID, backupID string) (string, error)
	GetProviderTimestamp(nodeID string) (string, error)
}

// nativeProviderError is the type of error this provider returns in case
// of API error. It also implements the ProviderError interface
type nativeProviderError struct {
	err error
}

func (d *nativeProviderError) Error() string {
	return d.err.Error()
}

func (d *nativeProviderError) IsAuthError() bool {
	if strings.Contains(d.err.Error(), "AuthError") {
		return true
	}
	return false
}

// NativeBackupProviderBridge is a bridge for using a native implemented provider in the backup manager.
type NativeBackupProviderBridge struct {
	nativeProvider NativeBackupProvider
}

// UploadBackupFiles is called when files needs to be uploaded as part of the backup
func (b *NativeBackupProviderBridge) UploadBackupFiles(file string, nodeID string, encryptionType string, timestamp time.Time) (string, error) {
	acc, err := b.nativeProvider.UploadBackupFiles(file, nodeID, encryptionType, timestamp.Format(time.RFC3339))
	if err != nil {
		return "", &nativeProviderError{err: err}
	}
	return acc, nil
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

// GetProviderTimestamp is called in order to get a more accurate timestamp from the cloud.
func (b *NativeBackupProviderBridge) GetProviderTimestamp(nodeID string) (time.Time, error) {
	timestamp, err := b.nativeProvider.GetProviderTimestamp(nodeID)
	if err != nil {
		fmt.Printf("Failed to get timestamp for UploadFileAndRetriveTimeStamp: %v\n", err)
		return time.Time{}, err
	}
	if timestamp == "" {
		return time.Time{}, errors.New("GetProviderTimestamp got empty timestamp")
	}
	return time.Parse(time.RFC3339, timestamp)
}

func (b *NativeBackupProviderBridge) SetTor(torConfig *tor.TorConfig) {
	return
}

func (b *NativeBackupProviderBridge) TestAuth() error {
	return nil
}

// RegisterNativeBackupProvider registered a native backup provider
func RegisterNativeBackupProvider(name string, provider NativeBackupProvider) {
	backup.RegisterProvider(name, func(providerFactoryInfo backup.ProviderFactoryInfo) (backup.Provider, error) {
		return &NativeBackupProviderBridge{nativeProvider: provider}, nil
	})
}
