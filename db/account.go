package db

// SaveAccount saves an account information to the database
func (db *DB) SaveAccount(account []byte) error {
	return db.saveItem([]byte(accountBucket), []byte("account"), account)
}

// FetchAccount fetches the cached account info from the database
func (db *DB) FetchAccount() ([]byte, error) {
	return db.fetchItem([]byte(accountBucket), []byte("account"))
}
