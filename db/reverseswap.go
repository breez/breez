package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/breez/breez/data"
	"github.com/coreos/bbolt"
	"github.com/golang/protobuf/proto"
)

// SaveReverseSwap saves the reverse swap information
func (db *DB) SaveReverseSwap(rs *data.ReverseSwap) (string, error) {
	p, err := hex.DecodeString(rs.Preimage)
	if err != nil {
		return "", fmt.Errorf("hex.DecodeString(%v): %w", rs.Preimage, err)
	}
	data, err := proto.Marshal(rs)
	if err != nil {
		return "", fmt.Errorf("proto.Marshal(%#v): %w", rs, err)
	}
	h := sha256.Sum256(p)
	return hex.EncodeToString(h[:]), db.Update(func(tx *bbolt.Tx) error {
		rsb := tx.Bucket([]byte(reverseSwapBucket))
		return rsb.Put(h[:], data)
	})
}

// FetchReverseSwap fetches the reverse swap information
func (db *DB) FetchReverseSwap(hash string) (*data.ReverseSwap, error) {
	h, err := hex.DecodeString(hash)
	if err != nil {
		return nil, err
	}
	var b []byte
	err = db.View(func(tx *bbolt.Tx) error {
		rsb := tx.Bucket([]byte(reverseSwapBucket))
		b = rsb.Get(h)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var rs data.ReverseSwap
	err = proto.Unmarshal(b, &rs)
	if err != nil {
		return nil, fmt.Errorf("proto.Unmarshal(%x): %w", b, err)
	}
	return &rs, nil
}
