package backup

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/btcsuite/btclog"
)

const (
	timeFormat = "2006-01-02 15-04-05"
)

type NextCloudProvider struct {
	authData ProviderData
	log      btclog.Logger
}

type ProviderData struct {
	User     string
	Password string
	Url      string
	BreezDir string
}

type BackupInfo struct {
	BackupDir string
	Info      *SnapshotInfo
}

type webdavProviderError struct {
	err error
}

func (d *webdavProviderError) Error() string {
	return d.err.Error()
}
func (d *webdavProviderError) IsAuthError() bool {
	if ferr, ok := d.err.(*WebdavRequestError); ok {
		status := ferr.StatusCode
		return status == 400 || status == 401 || status == 403
	}

	return false
}

func NewNextCloudProvider(authData ProviderData, log btclog.Logger) (*NextCloudProvider, error) {
	return &NextCloudProvider{
		authData: authData,
		log:      log,
	}, nil
}

func (n *NextCloudProvider) getClient() (string, *WebdavClient, error) {
	c, err := Dial(n.authData.Url, n.authData.User, n.authData.Password)
	return n.authData.BreezDir, c, err
}

func (n *NextCloudProvider) UploadBackupFiles(file string, nodeID string, encryptionType string) (
	string, error) {

	breezDir, c, err := n.getClient()
	if err != nil {
		return "", err
	}

	if err := n.createDirIfNotExists(c, breezDir); err != nil {
		return "", &webdavProviderError{err: err}
	}

	// create the backup dir
	nodeDir := path.Join(breezDir, nodeID)
	if err := n.createDirIfNotExists(c, nodeDir); err != nil {
		return "", &webdavProviderError{err: err}
	}
	backupDir := path.Join(nodeDir, time.Now().Format(timeFormat))
	if err := n.createDirIfNotExists(c, backupDir); err != nil {
		return "", &webdavProviderError{err: err}
	}

	// open the file to backup
	fileInfo, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer fileInfo.Close()
	fileName := filepath.Base(file)

	da, err := ioutil.ReadAll(fileInfo)
	if err != nil {
		return "", err
	}
	p := path.Join(backupDir, fileName)
	if err := c.Upload(da, p); err != nil {
		return "", err
	}
	backupInfo := &BackupInfo{
		BackupDir: backupDir,
		Info: &SnapshotInfo{
			BackupID:       "",
			NodeID:         nodeID,
			Encrypted:      encryptionType != "",
			EncryptionType: encryptionType,
			ModifiedTime:   time.Now().Format(time.RFC3339),
		}}
	data, err := json.Marshal(backupInfo)
	if err != nil {
		return "", err
	}

	if err := c.Upload(data, path.Join(nodeDir, "snapshotinfo")); err != nil {
		return "", &webdavProviderError{err: err}
	}

	// Delete old snapshots
	files, err := c.ListDir(nodeDir)
	if err != nil {
		return "", &webdavProviderError{err: err}
	}

	for _, file := range files.Files {
		isSnapshotFile := strings.Contains(file.Href, "snapshotinfo")
		isBackupDir := strings.Contains(file.Href, strings.ReplaceAll(backupDir, " ", "%20"))
		if !isSnapshotFile && !isBackupDir {
			normalizedDir := strings.ReplaceAll(file.Href, "%20", " ")
			pathStart := strings.Index(normalizedDir, breezDir)
			path := normalizedDir[pathStart:]
			if len(strings.Split(path, "/")) > 3 {
				if err := c.Delete(path); err != nil {
					return "", nil
				}
			}
		}
	}
	return "", nil
}

func (n *NextCloudProvider) createDirIfNotExists(client *WebdavClient, destDir string) error {
	if client.Exists(destDir) {
		return nil
	}
	if err := client.Mkdir(destDir); err != nil {
		return err
	}
	return nil
}

func (n *NextCloudProvider) AvailableSnapshots() ([]SnapshotInfo, error) {
	var snapshots []SnapshotInfo
	breezDir, client, err := n.getClient()
	if err != nil {
		return nil, err
	}
	files, err := client.ListDir(breezDir)
	if err != nil {
		if ferr, ok := err.(*WebdavRequestError); ok {
			if ferr.StatusCode == 404 {
				return snapshots, nil
			}
		}
		return nil, &webdavProviderError{err: err}
	}

	for _, file := range files.Files {
		bytes, err := client.Download(path.Join(file.Path, "snapshotinfo"))
		if err != nil {
			continue
		}
		var backupInfo BackupInfo
		if err := json.Unmarshal(bytes, &backupInfo); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *backupInfo.Info)
	}

	return snapshots, nil
}

func (n *NextCloudProvider) DownloadBackupFiles(nodeID, backupID string) ([]string, error) {
	_, client, err := n.getClient()
	if err != nil {
		return nil, err
	}
	dir, err := ioutil.TempDir("", "webdav")
	if err != nil {
		return nil, err
	}

	var backupInfo BackupInfo
	backupInfoData, err := client.Download(path.Join(n.authData.BreezDir, nodeID, "snapshotinfo"))
	if err != nil {
		return nil, &webdavProviderError{err: err}
	}
	if err := json.Unmarshal(backupInfoData, &backupInfo); err != nil {
		return nil, err
	}
	backupInfo.Info.BackupID = backupID
	data, err := json.Marshal(backupInfo)
	if err != nil {
		return nil, err
	}
	if err := client.Upload(data, path.Join(n.authData.BreezDir, nodeID, "snapshotinfo")); err != nil {
		return nil, &webdavProviderError{err: err}
	}

	// Download all the files in parallel
	var downloaded []string
	files, err := client.ListDir(backupInfo.BackupDir)
	if err != nil {
		return nil, &webdavProviderError{err: err}
	}
	for _, file := range files.Files {
		localFilePath := path.Join(dir, filepath.Base(file.Path))
		fileData, err := client.Download(path.Join(file.Path))
		if err != nil {
			return nil, &webdavProviderError{err: err}
		}
		file, err := os.Create(localFilePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		io.Copy(file, bytes.NewReader(fileData))
		downloaded = append(downloaded, localFilePath)
	}
	return downloaded, nil
}
