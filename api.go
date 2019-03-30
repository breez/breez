package breez

import (
	"path"

	"github.com/breez/breez/backup"
	"github.com/breez/breez/db"
)

// RequestBackup is the breez API for asking the system to backup now.
func (a *App) RequestBackup() {
	a.backupManager.RequestBackup()
}

// Restore is the breez API for restoring a specific nodeID using the configured
// backup backend provider.
func (a *App) Restore(nodeID string) error {
	a.log.Infof("Restore nodeID = %v", nodeID)
	if err := a.breezDB.Close(); err != nil {
		return err
	}
	defer func() {
		a.breezDB, _ = db.OpenDB(path.Join(a.cfg.WorkingDir, "breez.db"))
	}()
	_, err := a.backupManager.Restore(nodeID)
	return err
}

// AvailableSnapshots is thte breez API for fetching all available backuped up
// snapshot. One snapshot per node that is backed up.
func (a *App) AvailableSnapshots() ([]backup.SnapshotInfo, error) {
	return a.backupManager.AvailableSnapshots()
}
