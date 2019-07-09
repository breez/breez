package db

import ()

// FetchLastSyncedHeaderTimestamp the last known header timestamp that the node is synced to.
func (db *DB) FetchLastSyncedHeaderTimestamp() (int64, error) {
	tsBytes, err := db.fetchItem([]byte(syncstatus), []byte("last_header_timestamp"))
	if err != nil || tsBytes == nil {
		return 0, err
	}
	return int64(btoi(tsBytes)), nil
}

// SetLastSyncedHeaderTimestamp saves the last known header timestamp that the node is synced to.
func (db *DB) SetLastSyncedHeaderTimestamp(ts int64) error {
	return db.saveItem([]byte(syncstatus), []byte("last_header_timestamp"), itob(uint64(ts)))
}
