package backup

import (
	"os"
	"testing"

	"github.com/breez/breez/data"
)

type MockAuthService struct{}

func (s *MockAuthService) SignIn() (string, error) {
	return "test-token", nil
}

type MockProvider struct {
	UploadBackupFilesImpl   func(files []string, nodeID string) error
	AvailableSnapshotsImpl  func() ([]SnapshotInfo, error)
	DownloadBackupFilesImpl func(nodeID, backupID string) ([]string, error)
}

func newMockProvider() *MockProvider {
	p := MockProvider{}
	p.AvailableSnapshotsImpl = func() ([]SnapshotInfo, error) {
		return []SnapshotInfo{}, nil
	}
	p.DownloadBackupFilesImpl = func(nodeID, backupID string) ([]string, error) {
		return []string{}, nil
	}
	p.UploadBackupFilesImpl = func(files []string, nodeID string) error {
		return nil
	}
	return &p
}

func (m *MockProvider) UploadBackupFiles(files []string, nodeID string) error {
	return m.UploadBackupFilesImpl(files, nodeID)
}
func (m *MockProvider) AvailableSnapshots() ([]SnapshotInfo, error) {
	return m.AvailableSnapshotsImpl()
}
func (m *MockProvider) DownloadBackupFiles(nodeID, backupID string) ([]string, error) {
	return m.DownloadBackupFilesImpl(nodeID, backupID)
}

func TestCreateDefaultProvider(t *testing.T) {
	tempDir := os.TempDir()
	ntfnChan := make(chan data.NotificationEvent)
	manager, err := NewManager("gdrive", nil, ntfnChan, tempDir)
	if err != nil {
		t.Error(err)
	}
	if _, ok := manager.provider.(*GoogleDriveProvider); !ok {
		t.Error("default provider is not google drive")
	}
}
