package backup

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	"golang.org/x/oauth2"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

const (
	backupIDProperty           = "backupID"
	activeBackupFolderProperty = "activeBackupFolder"
)

// TokenSource is a structure that implements the auth2.TokenSource interface to be used by
// GoogleDriveBackupProvider
type TokenSource struct {
	authService AuthService
}

// Token retrieved a token from the AuthService provided
func (s *TokenSource) Token() (*oauth2.Token, error) {
	token, err := s.authService.SignIn()
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{
		AccessToken: token,
	}, nil
}

// GoogleDriveProvider is an implementation of backup.Provider interface.
// It uses app data in google drive as a storage mechanism.
// Backups are stored in the following manner:
// 1. Nodes backup will be saved directly under "appDataFolder", which is the user
//    data directory in google drive. Folders will be named by the convention:
//    "snapshot-<node_id>".
// 2. When a request for backup lands, this provider will do the following:
//   2.1 Create a nested folder under the node folder.
//   2.2 Save all the backup files under this created folder.
//   2.3 Update the "activeBackupFolderProperty" metadata property of the node folder
//       to be the newly created folder id.
//   2.4 Delete stale backup folder that are not in use anymore for this node id.
// 3. When a request for restore (download) lands, this provider will do the following:
//   3.1 Identify the "active backup folder" by reading the metadata property of the node folder.
//   3.2 Download the files
//   3.3 Mark this backup as "restored by this instance" by assign the value "backupID" to the node
//       metadata property "backupIDProperty". This value will be used to detect the problematic case
//       where two nodes are running the same backup...
type GoogleDriveProvider struct {
	driveService *drive.Service
}

// NewGoogleDriveProvider creates a new instance of GoogleDriveProvider.
// It needs AuthService that will provide the access token, and refresh access token functionality
// It implements the backup.Provider interface by using google drive as storage.
func NewGoogleDriveProvider(authService AuthService) (*GoogleDriveProvider, error) {
	tokenSource := &TokenSource{authService: authService}
	token, err := tokenSource.Token()
	if err != nil {
		return nil, err
	}
	httpClient := oauth2.NewClient(context.Background(), oauth2.ReuseTokenSource(token, tokenSource))
	driveService, err := drive.New(httpClient)
	if err != nil {
		return nil, fmt.Errorf("Error creating drive service, %v", err)
	}
	return &GoogleDriveProvider{driveService: driveService}, nil
}

// AvailableSnapshots fetches all existing backup snapshots that exist in the user app data.
// There is only one snapshot per node id.
func (p *GoogleDriveProvider) AvailableSnapshots() ([]SnapshotInfo, error) {
	r, err := p.driveService.Files.List().Spaces("appDataFolder").
		Fields("files(name)", "files(modifiedTime)", "files(appProperties)").
		Q("'appDataFolder' in parents and name contains 'snapshot-'").
		Do()
	if err != nil {
		if ferr, ok := err.(*googleapi.Error); ok {
			if ferr.Code == 404 {
				return []SnapshotInfo{}, nil
			}
		}
		return nil, err
	}
	var backups []SnapshotInfo

	// We are going over all nodes that have a snapshot.
	// For each snapshot we extract the NodeID, Modifiedtime
	// and BackupID (it will only exist if this node has been restored)
	for _, f := range r.Files {
		var backupID string
		for k, v := range f.AppProperties {
			if k == backupIDProperty {
				backupID = v
			}
		}
		backups = append(backups, SnapshotInfo{
			NodeID:       f.Name,
			ModifiedTime: f.ModifiedTime,
			BackupID:     backupID,
		})
	}

	return backups, nil
}

// UploadBackupFiles uploads the backup files related to a specific node to google drive.
// It does the following:
// 1. Creates a fresh new folder under the node folder.
// 2. Uploads all files to the newly created folder.
// 3. update the node folder metadata property "activeBackupFolderProperty" to contain the
//    id of the new folder.
func (p *GoogleDriveProvider) UploadBackupFiles(files []string, nodeID string) error {
	// Fetch the node folder
	nodeFolder, err := p.nodeFolder(nodeID)
	if err != nil {
		return err
	}

	// Create the backup folder
	newBackupFolder, err := p.createFolder(nodeFolder.Id, "backup")
	if err != nil {
		return err
	}
	// Upload the files
	for _, fPath := range files {
		fileName := path.Base(fPath)
		file, err := os.Open(fPath)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = p.driveService.Files.Create(&drive.File{
			Name:    fileName,
			Parents: []string{newBackupFolder.Id}},
		).Media(file).Do()
		if err != nil {
			return err
		}
	}
	// Update active backup folder for this specific node folder
	folderUpdate := &drive.File{AppProperties: map[string]string{activeBackupFolderProperty: newBackupFolder.Id}}
	_, err = p.driveService.Files.Update(nodeFolder.Id, folderUpdate).Do()
	return err
}

// DownloadBackupFiles is responsible for download a specific node backup and updating.
// that this backup was now restored by this instance represented by "backupID"
func (p *GoogleDriveProvider) DownloadBackupFiles(nodeID, backupID string) ([]string, error) {
	// fetch the node folder
	nodeFolder, err := p.nodeFolder(nodeID)
	if err != nil {
		return nil, err
	}

	// Fetch thte backup folder
	if nodeFolder.AppProperties == nil {
		return nil, fmt.Errorf("can't find active backup for node %v", nodeID)
	}
	folderID, ok := nodeFolder.AppProperties[activeBackupFolderProperty]
	if !ok {
		return nil, fmt.Errorf("can't find active backup for node %v", nodeID)
	}

	// Query all the files under the backup folder
	r, err := p.driveService.Files.List().Spaces("appDataFolder").Q(fmt.Sprintf("'%v' in parents", folderID)).Do()
	if err != nil {
		return nil, fmt.Errorf("could not query backup folder: %v", err)
	}

	dir, err := ioutil.TempDir("", "gdrive")
	if err != nil {
		return nil, err
	}
	// Download all the files
	var downloaded []string
	for _, f := range r.Files {
		path, err := p.downloadFile(f, dir)
		if err != nil {
			return nil, err
		}
		downloaded = append(downloaded, path)
	}

	// Upate this backup as restored by this instance.
	folderUpdate := &drive.File{AppProperties: map[string]string{backupIDProperty: backupID}}
	_, err = p.driveService.Files.Update(nodeFolder.Id, folderUpdate).Do()
	if err != nil {
		return nil, err
	}
	return downloaded, p.deleteStaleSnapshots(nodeFolder.Id, folderID)
}

// nodeFolder is responsible for fetching the node folder. If it doesn't exist it creates it.
func (p *GoogleDriveProvider) nodeFolder(nodeID string) (*drive.File, error) {
	r, err := p.driveService.Files.List().Spaces("appDataFolder").
		Fields("files(id)", "files(appProperties)", "files(name)").
		Q(fmt.Sprintf("'appDataFolder' in parents and name = 'snapshot-%v'", nodeID)).
		Do()
	if err != nil {
		return nil, err
	}
	if len(r.Files) == 0 {
		return p.createFolder("appDataFolder", "snapshot-"+nodeID)
	}
	if len(r.Files) > 1 {
		return nil, fmt.Errorf("there is more than one folder with name %v", nodeID)
	}
	return r.Files[0], nil
}

// createFolder creates a folder in drive under "parent"
func (p *GoogleDriveProvider) createFolder(parent string, name string) (*drive.File, error) {
	return p.driveService.Files.Create(&drive.File{
		Name:     name,
		Parents:  []string{parent},
		MimeType: "application/vnd.google-apps.folder"}).Do()
}

// downloadFile download a file from drive to a specific dir.
// the file name is preserved.
func (p *GoogleDriveProvider) downloadFile(file *drive.File, dir string) (string, error) {
	res, err := p.driveService.Files.Get(file.Id).Download()
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	targetPath := path.Join(dir, file.Name)
	out, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, res.Body)
	return targetPath, err
}

// deleteStaleSnapshots delete all snapshots for a specific node except for the active one.
func (p *GoogleDriveProvider) deleteStaleSnapshots(nodeFolderID, activeBackupFolderID string) error {
	r, err := p.driveService.Files.List().Spaces("appDataFolder").
		Q(fmt.Sprintf("'%v' in parents", nodeFolderID)).
		Do()
	if err != nil {
		return err
	}
	for _, f := range r.Files {
		if f.Id == activeBackupFolderID {
			continue
		}
		if err := p.driveService.Files.Delete(f.Id).Do(); err != nil {
			return err
		}
	}
	return nil
}
