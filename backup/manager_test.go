package backup

import (
	"errors"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/breez/breez/config"
	"github.com/breez/breez/data"
	"github.com/btcsuite/btclog"
)

type MockAuthService struct{}

func (s *MockAuthService) SignIn() (string, error) {
	return "test-token", nil
}

type MockTester struct {
	uploadCounter           int
	downloadCounter         int
	preparer                DataPreparer
	lastNotification        data.NotificationEvent
	UploadBackupFilesImpl   func(files []string, nodeID string) error
	AvailableSnapshotsImpl  func() ([]SnapshotInfo, error)
	DownloadBackupFilesImpl func(nodeID, backupID string) ([]string, error)
}

func newDefaultMockTester() *MockTester {
	p := MockTester{}
	p.preparer = func() (paths []string, nodeID string, err error) {
		return []string{"file1"}, "test-node-id", nil
	}
	p.AvailableSnapshotsImpl = func() ([]SnapshotInfo, error) {
		return []SnapshotInfo{}, nil
	}
	p.DownloadBackupFilesImpl = func(nodeID, backupID string) ([]string, error) {
		p.downloadCounter++
		return []string{}, nil
	}
	p.UploadBackupFilesImpl = func(files []string, nodeID string) error {
		time.Sleep(time.Millisecond * 10)
		p.uploadCounter++
		return nil
	}
	return &p
}

func (m *MockTester) UploadBackupFiles(files []string, nodeID string) error {
	return m.UploadBackupFilesImpl(files, nodeID)
}
func (m *MockTester) AvailableSnapshots() ([]SnapshotInfo, error) {
	return m.AvailableSnapshotsImpl()
}
func (m *MockTester) DownloadBackupFiles(nodeID, backupID string) ([]string, error) {
	return m.DownloadBackupFilesImpl(nodeID, backupID)
}

func prepareBackupData() (paths []string, nodeID string, err error) {
	return []string{"file1"}, "test-node-id", nil
}

func createTestManager(mp *MockTester) (manager *Manager, err error) {
	backupDelay = time.Duration(0)
	RegisterProvider("mock", func(authServie AuthService, logger btclog.Logger) (Provider, error) { return mp, nil })

	ntfnChan := make(chan data.NotificationEvent)
	dir := os.TempDir()
	fmt.Println("tempDir " + dir)

	config := &config.Config{
		Network: "mainnet",
	}

	onEvent := func(d data.NotificationEvent) {
		ntfnChan <- d
	}
	manager, err = NewManager("mock", nil, onEvent, mp.preparer, config)
	if err != nil {
		return
	}
	go func() {
		for {
			select {
			case msg := <-ntfnChan:
				mp.lastNotification = msg
			case <-manager.quitChan:
				close(ntfnChan)
				os.RemoveAll(path.Join(dir, "backup.db"))
				fmt.Println("Remove db from go routine")
				return
			}
		}
	}()
	return
}

type mockAuthError struct{}

func (me *mockAuthError) Error() string {
	return ""
}
func (me *mockAuthError) IsAuthError() bool {
	return true
}

func TestCreateDefaultProvider(t *testing.T) {
	tempDir := os.TempDir()
	fmt.Println("tempDir " + tempDir)
	ntfnChan := make(chan data.NotificationEvent)
	config := &config.Config{
		Network: "mainnet",
	}
	onEvent := func(d data.NotificationEvent) {
		ntfnChan <- d
	}
	manager, err := NewManager("gdrive", nil, onEvent, prepareBackupData, config)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.destroy()

	if _, ok := manager.provider.(*GoogleDriveProvider); !ok {
		t.Error("default provider is not google drive")
	}
}

func TestRequestBackup(t *testing.T) {
	tester := newDefaultMockTester()
	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	defer manager.destroy()

	manager.RequestBackup()
	time.Sleep(100 * time.Millisecond)
	if tester.uploadCounter != 1 {
		t.Error("Backup was not called after backup requested")
	}
}

func TestMultipleRequestBackup(t *testing.T) {
	tester := newDefaultMockTester()
	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	defer manager.destroy()

	for i := 0; i < 10; i++ {
		manager.RequestBackup()
	}
	time.Sleep(300 * time.Millisecond)
	if tester.uploadCounter > 2 {
		t.Error("Backup was called too much times")
	}
}

func TestErrorInPrepareBackup(t *testing.T) {
	preparer := func() (paths []string, nodeID string, err error) {
		return nil, "", errors.New("failed to prepare")
	}
	tester := newDefaultMockTester()
	tester.preparer = preparer
	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	defer manager.destroy()

	manager.RequestBackup()
	time.Sleep(100 * time.Millisecond)
	if tester.uploadCounter > 0 {
		t.Error("Backup was called despite error in preparing the data")
	}
}

func TestErrorInUpload(t *testing.T) {
	tester := newDefaultMockTester()
	tester.UploadBackupFilesImpl = func(files []string, nodeID string) error {
		return errors.New("failed to upload files")
	}
	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	defer manager.destroy()

	manager.RequestBackup()
	time.Sleep(100 * time.Millisecond)
	if tester.uploadCounter > 0 {
		t.Error("Backup was called despite error in preparing the data")
	}
	if tester.lastNotification.Type != data.NotificationEvent_BACKUP_FAILED {
		t.Error("wrong fail notification type")
	}
}

func TestAuthError(t *testing.T) {
	tester := newDefaultMockTester()
	tester.UploadBackupFilesImpl = func(files []string, nodeID string) error {
		return &mockAuthError{}
	}
	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	defer manager.destroy()

	manager.RequestBackup()
	time.Sleep(100 * time.Millisecond)
	if tester.uploadCounter > 0 {
		t.Error("Backup was called despite error in preparing the data")
	}
	if tester.lastNotification.Type != data.NotificationEvent_BACKUP_AUTH_FAILED {
		t.Error("wrong fail notification type")
	}
}

func TestRestoreWrongNumberOfFiles(t *testing.T) {
	tester := newDefaultMockTester()

	tester.DownloadBackupFilesImpl = func(nodeID, backupID string) ([]string, error) {
		return []string{"file1"}, nil
	}

	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.destroy()

	if _, err := manager.Restore("test-node-id"); err == nil {
		t.Fatal(err)
	}
}

func TestRestoreSuccess(t *testing.T) {
	tester := newDefaultMockTester()
	dir := os.TempDir()

	downloads := []string{"wallet.db", "channel.db", "breez.db"}
	sourceFiles := []string{}
	for _, d := range downloads {
		sourceFiles = append(sourceFiles, path.Join(dir, d))
	}

	tester.DownloadBackupFilesImpl = func(nodeID, backupID string) ([]string, error) {
		if nodeID != "test-node-id" {
			return nil, errors.New("node id not found")
		}

		for _, d := range sourceFiles {
			_, err := os.Create(d)
			if err != nil {
				t.Fatal(err)
			}
		}

		return sourceFiles, nil
	}

	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.destroy()

	restoredFiles, err := manager.Restore("test-node-id")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		for _, f := range sourceFiles {
			os.Remove(f)
		}
		for _, f := range restoredFiles {
			os.Remove(f)
		}
	}()

	for _, f := range restoredFiles {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Fatalf("file was not restored to the right location %v", f)
		}
	}
}

func TestBackupConflict(t *testing.T) {
	tester := newDefaultMockTester()

	tester.AvailableSnapshotsImpl = func() ([]SnapshotInfo, error) {
		return []SnapshotInfo{
			SnapshotInfo{
				NodeID:       "test-node-id",
				BackupID:     "conflict-backup-id",
				ModifiedTime: time.Now().String(),
			},
		}, nil
	}

	manager, err := createTestManager(tester)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.destroy()

	safe, err := manager.IsSafeToRunNode("test-node-id")
	if safe {
		t.Fatal("returned safe to run even though it isn't")
	}
}
