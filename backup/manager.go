package backup

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/breez/breez/data"
)

const backupFileName = "backup.zip"

var (
	backupDelay     = time.Duration(time.Second * 2)
	ErrorNoProvider = errors.New("Provider is not set")
)

// RequestCommitmentChangedBackup is called when the commitment transaction
// of the channel is changed. We add a small delay because we know such changes
// are coming in batch
func (b *Manager) RequestCommitmentChangedBackup() {
	b.requestBackup(backupDelay)
}

/*
RequestBackup push a request for the backup files of breez
*/
func (b *Manager) RequestBackup() {
	b.requestBackup(time.Duration(0))
}

/*
RequestBackup push a request for the backup files of breez
*/
func (b *Manager) requestBackup(delay time.Duration) {
	b.log.Infof("Backup requested")
	// first thing push a pending backup request to the database so we
	// can recover in case of error.
	err := b.db.addBackupRequest()
	if err != nil {
		b.log.Errorf("failed to set pending backup %v", err)
		b.notifyBackupFailed(err)
		return
	}

	go func() {
		select {
		case <-time.After(delay):
		case <-b.quitChan:
			return
		}
		b.backupRequestChan <- struct{}{}
	}()
}

// Download handles all the download process:
// 1. Downloading the backed up files for a specific node id.
func (b *Manager) Download(nodeID string) ([]string, error) {
	b.log.Infof("Download started nodeID=%v", nodeID)
	backupID, err := b.getBackupIdentifier()
	if err != nil {
		return nil, err
	}
	provider := b.getProvider()
	if provider == nil {
		return nil, ErrorNoProvider
	}
	files, err := provider.DownloadBackupFiles(nodeID, backupID)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Restore handles all the restoring process:
// 1. Downloading the backed up files for a specific node id.
// 2. Put the backed up files in the right place according to the configuration
func (b *Manager) Restore(nodeID string, key []byte) ([]string, error) {
	b.log.Infof("Restore started")
	backupID, err := b.getBackupIdentifier()
	if err != nil {
		return nil, err
	}
	provider := b.getProvider()
	if provider == nil {
		return nil, ErrorNoProvider
	}
	files, err := provider.DownloadBackupFiles(nodeID, backupID)
	if err != nil {
		return nil, err
	}
	b.log.Infof("Download files completed %v", len(files))
	if len(files) == 1 && path.Base(files[0]) == backupFileName {
		if files, err = uncompressFiles(files[0]); err != nil {
			return nil, err
		}
	}
	if len(files) != 3 {
		return nil, fmt.Errorf("wrong number of backup files %v", len(files))
	}

	// If we got an encryption key, let's decrypt the files
	if key != nil {
		b.log.Infof("Restore has encryption key")
		for i, p := range files {
			destPath := p + ".decrypted"
			err = decryptFile(p, destPath, key)
			if err != nil {
				return files, errors.New("Failed to restore backup due to incorrect PIN")
			}
			b.log.Infof("Restore file decrypted %v", i)
			if err = os.Remove(files[i]); err != nil {
				return nil, err
			}
			if err = os.Rename(destPath, files[i]); err != nil {
				return nil, err
			}
			b.log.Infof("decrypted file renamed %v", i)
		}
	}

	paths := map[string]string{
		"wallet.db":  "data/chain/bitcoin/{{network}}",
		"channel.db": "data/graph/{{network}}",
		"breez.db":   "",
	}
	var targetFiles []string
	for _, f := range files {
		basename := path.Base(f)
		p, ok := paths[basename]
		if !ok {
			return nil, err
		}
		destDir := path.Join(b.workingDir, strings.Replace(p, "{{network}}", b.config.Network, -1))
		if destDir != b.workingDir {
			err = os.MkdirAll(destDir, 0700)
			if err != nil {
				return nil, err
			}
		}

		b.log.Infof("restore file before rename %v", basename)
		err = os.Rename(f, path.Join(destDir, basename))
		if err != nil {
			return nil, err
		}
		b.log.Infof("restore file renamed %v", basename)
		targetFiles = append(targetFiles, path.Join(destDir, basename))
	}
	return targetFiles, nil
}

// AvailableSnapshots returns a list of snsapshot that the backup provider reports
// it has. Every snapshot is for a specific node id.
func (b *Manager) AvailableSnapshots() ([]SnapshotInfo, error) {
	provider := b.getProvider()
	if provider == nil {
		return nil, ErrorNoProvider
	}
	return provider.AvailableSnapshots()
}

// IsSafeToRunNode checks if it is safe for this breez instance to run a specific node.
// It is considered safe if we don't know of another instance which is the last to restore
// this node (nodeID)
func (b *Manager) IsSafeToRunNode(nodeID string) (bool, error) {
	provider := b.getProvider()
	if provider == nil {
		return false, ErrorNoProvider
	}
	snapshots, err := provider.AvailableSnapshots()
	if err != nil {
		return false, err
	}
	backupID, err := b.getBackupIdentifier()
	if err != nil {
		return false, err
	}
	for _, s := range snapshots {
		if s.NodeID == nodeID && s.BackupID != "" && backupID != s.BackupID {
			b.log.Errorf("remote restore was found for node %v.", nodeID)
			b.log.Errorf("current backupID=%v, remote backupID-%v", backupID, s.BackupID)
			return false, nil
		}
	}
	return true, nil
}

// Start is the main go routine that listens to backup requests and is resopnsible for executing it.
func (b *Manager) Start() error {
	if atomic.SwapInt32(&b.started, 1) == 1 {
		return nil
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		for {
			select {
			case <-b.backupRequestChan:
				b.log.Infof("start processing backup request")
				//First get the last pending request in the database
				pendingID, err := b.db.lastBackupRequest()
				if pendingID == 0 {
					continue
				}

				b.mu.Lock()
				encryptionKey := b.encryptionKey
				encryptionType := b.encryptionType
				b.mu.Unlock()

				useEncryption, err := b.db.useEncryption()
				if err != nil {
					b.log.Errorf("error reading encryption settings from backup db %v", err)
					continue
				}
				if useEncryption && encryptionKey == nil {
					b.log.Errorf("fail to serve pending backup due to key not provided yet %v", err)
					continue
				}

				b.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_BACKUP_REQUEST})
				paths, nodeID, err := b.prepareBackupData()
				if err != nil {
					b.log.Errorf("error in backup %v", err)
					b.notifyBackupFailed(err)
					continue
				}
				provider := b.getProvider()
				if provider == nil {
					b.notifyBackupFailed(ErrorNoProvider)
					continue
				}

				// If we have an encryption key let's encrypt the files.
				encrypt := encryptionKey != nil
				if encrypt {
					b.log.Infof("using encryption to backup files")
					for i, p := range paths {
						destPath := p + ".enc"
						err = encryptFile(p, destPath, encryptionKey)
						if err != nil {
							break
						}
						if err = os.Remove(paths[i]); err != nil {
							break
						}
						if err = os.Rename(destPath, paths[i]); err != nil {
							break
						}
					}

					if err != nil {
						b.notifyBackupFailed(err)
						b.log.Errorf("error in encrypting backup files %v", err)
						continue
					}
				}

				for _, p := range paths {
					pathInfo, err := os.Stat(p)
					if err == nil {
						b.log.Infof("uploading %v with size: %v", p, pathInfo.Size())
					}
				}

				// Zip files
				compressedFile := path.Join(path.Dir(paths[0]), backupFileName)
				if err := b.compressFiles(paths, compressedFile); err != nil {
					b.log.Infof("failed to compress backup files", err)
					continue
				}

				accountName, err := provider.UploadBackupFiles(compressedFile, nodeID, encryptionType)
				if err != nil {
					for _, p := range paths {
						_ = os.Remove(p)
					}
					b.log.Errorf("error in backup %v", err)
					b.notifyBackupFailed(err)
					continue
				}
				for _, p := range paths {
					_ = os.Remove(p)
				}
				b.db.markBackupRequestCompleted(pendingID)
				b.log.Infof("backup finished successfully")
				b.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_BACKUP_SUCCESS, Data: []string{accountName}})
			case <-b.quitChan:
				return
			}
		}
	}()
	b.backupRequestChan <- struct{}{}

	return nil
}

func (b *Manager) compressFiles(paths []string, dest string) error {
	destFile, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer destFile.Close()

	w := zip.NewWriter(destFile)
	for _, p := range paths {
		f, err := w.Create(path.Base(p))
		if err != nil {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			b.log.Infof("failed to open backup file %v", err)
			return err
		}
		defer file.Close()
		if _, err := io.Copy(f, file); err != nil {
			return err
		}
	}

	// Make sure to check the error on Close.
	err = w.Close()
	if err != nil {
		return err
	}

	return nil
}

func uncompressFiles(zipFile string) ([]string, error) {
	r, err := zip.OpenReader(zipFile)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	destDir := path.Dir(zipFile)
	var destFiles []string
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		destFile, err := os.OpenFile(path.Join(destDir, f.Name), os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return nil, err
		}
		defer destFile.Close()
		if _, err := io.Copy(destFile, rc); err != nil {
			return nil, err
		}
		destFiles = append(destFiles, destFile.Name())
		rc.Close()
	}

	return destFiles, nil
}

// SetEncryptionKey sets the key which should be used to encrypt the backup files
func (b *Manager) SetEncryptionKey(encKey []byte, encryptionType string) error {
	if err := b.db.setUseEncryption(len(encKey) > 0); err != nil {
		return err
	}

	b.mu.Lock()
	b.encryptionKey = encKey
	b.encryptionType = encryptionType
	b.mu.Unlock()

	// After changing the encryption PIN we'll backup if we have
	// pending backup requests.
	b.backupRequestChan <- struct{}{}
	b.log.Infof("Successfully set new encryption key")
	return nil
}

// Stop stops the BackupService and wait for complete shutdown.
func (b *Manager) Stop() error {
	if atomic.SwapInt32(&b.stopped, 1) == 1 {
		return nil
	}
	b.db.close()
	close(b.quitChan)
	b.wg.Wait()
	b.log.Infof("BackupManager shutdown successfully")
	return nil
}

// destroy is intended to use internally for testing purpose.
func (b *Manager) destroy() error {
	b.Stop()
	if err := os.RemoveAll(path.Join(b.workingDir, "backup")); err != nil {
		return err
	}
	if err := os.RemoveAll(path.Join(b.workingDir, "data")); err != nil {
		return err
	}
	return os.Remove(path.Join(b.workingDir, "backup.db"))
}

func (b *Manager) notifyBackupFailed(err error) {
	b.log.Infof("notifyBackupFailed %v", err)
	if ferr, ok := err.(ProviderError); ok {
		if ferr.IsAuthError() {
			b.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_BACKUP_AUTH_FAILED})
			return
		}
	}
	b.onServiceEvent(data.NotificationEvent{Type: data.NotificationEvent_BACKUP_FAILED})
}

// getBackupIdentifier retrieves an identifier that is unique for this instance of breez.
// We use is as a mechanism for conflict detection, in case a restore was done on some
// other device.
// The identifier is generated once and save in a file, we can't save it in the db as it
// is backed up and would cause the restored node to have the same identifier...
func (b *Manager) getBackupIdentifier() (string, error) {
	backupDir := path.Join(b.workingDir, "backup")
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

func (b *Manager) SetBackupProvider(providerName string) error {
	provider, err := createBackupProvider(providerName, b.authService, b.log)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.provider = provider
	return nil
}

func (b *Manager) getProvider() Provider {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.provider
}
