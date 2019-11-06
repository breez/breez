package db

// SaveAccount saves an account information to the database
func (db *DB) SaveAccount(account []byte) error {
	return db.saveItem([]byte(accountBucket), []byte("account"), account)
}

// FetchAccount fetches the cached account info from the database
func (db *DB) FetchAccount() ([]byte, error) {
	return db.fetchItem([]byte(accountBucket), []byte("account"))
}

// EnableAccount sets the account state to either enabled or disabled
func (db *DB) EnableAccount(enabled bool) error {
	var enabledByte byte
	if enabled {
		enabledByte = 1
	}
	return db.saveItem([]byte(accountBucket), []byte("enabled"), []byte{enabledByte})
}

// AccountEnabled returns the account state
func (db *DB) AccountEnabled() (bool, error) {
	bytes, err := db.fetchItem([]byte(accountBucket), []byte("enabled"))
	if err != nil {
		return false, err
	}
	return bytes == nil || bytes[0] == 1, nil
}