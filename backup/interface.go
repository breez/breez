package backup

// SnapshotInfo is an existing backup information for a specific node id.
type SnapshotInfo struct {
	NodeID       string
	BackupID     string
	ModifiedTime string
}

// Service is the interface to expose from this package as Backup Service API.
// These functions will be used from the application layer.
type Service interface {
	RequestBackup()
	Restore(nodeID string) error
	AvailableSnapshots() ([]SnapshotInfo, error)
}

// Provider represents the functionality needed to be implemented for any backend backup
// storage provider. This provider will be used and inststiated by the service.
type Provider interface {
	UploadBackupFiles(files []string, nodeID string) error
	AvailableSnapshots() ([]SnapshotInfo, error)
	DownloadBackupFiles(nodeID, backupID string) ([]string, error)
}

// AuthService is the interface that the backup provider needs in order to function.
// Because the authentication can be in many forms (also multiple platforms) it can't be
// implemented as part of the backup provider but rather should be passed from the
// running app/platform. For example in Android the authentication would be done by using
// the AccountManager API.
type AuthService interface {
	SignIn() (string, error)
}
