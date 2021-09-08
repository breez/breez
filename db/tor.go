package db

const (
	useTorKey = "useTor"
)

func (db *DB) SetUseTor(useTor bool) error {

	var _useTor byte
	if useTor {
		_useTor = 1
	}

	return db.saveItem([]byte(torBucket), []byte(useTorKey), []byte{_useTor})
}

func (db *DB) GetUseTor() (result bool, err error) {

	if useTor, err := db.fetchItem([]byte(torBucket), []byte(useTorKey)); err == nil {
		if useTor != nil && useTor[0] == 1 {
			result = true
		}
	}

	return result, err
}
