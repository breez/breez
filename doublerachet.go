package breez

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/status-im/doubleratchet"
)

var (
	byteOrder binary.ByteOrder
)

func init() {
	byteOrder = binary.BigEndian
}

/*
NewSession is used by the initiator of the encrypted session.
This function takes no paramters and returns:

sessionID: A unique string that identified the session for later use
secret:    A shared secret to be shared with the other side

This function creates a session and stores its state in the SessionStore
Following this operation the caller can imediately use Encrypt/Decrypt by
providing the sessionID as identifier.
*/
func NewSession() (sessionID, secret, pubKey string, err error) {
	crypto := doubleratchet.DefaultCrypto{}

	var sk [32]byte
	_, err = rand.Read(sk[:])
	if err != nil {
		return
	}

	sID, err := generateSessionID()
	if err != nil {
		return
	}

	keyPair, err := crypto.GenerateDH()
	if err != nil {
		return
	}
	_, err = doubleratchet.New([]byte(sID), sk, keyPair, &BoltDBSessionStorage{})
	if err != nil {
		return
	}

	publicKey := keyPair.PublicKey()
	return sID, hex.EncodeToString(sk[:]), hex.EncodeToString(publicKey[:]), nil
}

/*
NewSessionWithRemoteKey is used by the recepient side  of the encrypted session.
This function takes the following parameters:

secret:    		Shared secret that was agreed with the initiator side
remotePubKey 	The initiator side public key.

This function creates a session and stores its state in the SessionStore
Following this operation the caller can imediately use Encrypt/Decrypt by
providing the sessionID as identifier.
*/
func NewSessionWithRemoteKey(secret, remotePubKey string) (string, error) {
	sID, err := generateSessionID()
	if err != nil {
		return "", err
	}

	skBytes, err := hex.DecodeString(secret)
	if err != nil {
		return "", err
	}

	pubKeyBytes, err := hex.DecodeString(remotePubKey)
	if err != nil {
		return "", err
	}

	_, err = doubleratchet.NewWithRemoteKey([]byte(sID), toKey(skBytes), toKey(pubKeyBytes), &BoltDBSessionStorage{})
	if err != nil {
		return "", err
	}

	return sID, nil
}

/*
Encrypt is used to encrypt a message providing a sessionID and a message to encrypt
This function loads the session from the session store and use it to encrypt the message.
This way the user is free of managing sessions state and only need to provide the sessionID.
*/
func RatchetEncrypt(sessionID, message string) (string, error) {
	session, err := doubleratchet.Load([]byte(sessionID), &BoltDBSessionStorage{})
	if err != nil {
		return "", err
	}
	msg, err := session.RatchetEncrypt([]byte(message), nil)
	if err != nil {
		return "", err
	}

	rawMessage, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(rawMessage), nil
}

/*
Decrypt is used to decrypt a message providing a sessionID and a message to decrypt
This function loads the session from the session store and use it to decrypt the message.
This way the user is free of managing sessions state and only need to provide the sessionID.
*/
func RatchetDecrypt(sessionID, message string) (string, error) {

	var msg doubleratchet.Message
	if err := json.Unmarshal([]byte(message), &msg); err != nil {
		return "", err
	}

	session, err := doubleratchet.Load([]byte(sessionID), &BoltDBSessionStorage{})
	if err != nil {
		return "", err
	}
	decrypted, err := session.RatchetDecrypt(msg, nil)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

//DHPair is a key pair structure that implements the doubleratchet.DHPair interface
type DHPair struct {
	privateKey [32]byte
	publicKey  [32]byte
}

//PrivateKey is part of the doubleratchets.DHPair interaface
func (p DHPair) PrivateKey() doubleratchet.Key {
	return p.privateKey
}

//PublicKey is part of the doubleratchets.DHPair interaface
func (p DHPair) PublicKey() doubleratchet.Key {
	return p.publicKey
}

//BoltDBSessionStorage is a tructure that implements the SessionStore interface.
//It uses boltdb to save sessions
type BoltDBSessionStorage struct{}

// Save the session to the session store
func (s *BoltDBSessionStorage) Save(id []byte, state *doubleratchet.State) error {

	var buf bytes.Buffer

	if err := binary.Write(&buf, byteOrder, state.RootCh.CK[:]); err != nil {
		return err
	}
	if _, err := buf.Write(state.DHr[:]); err != nil {
		return err
	}

	pubKey := state.DHs.PublicKey()
	if _, err := buf.Write(pubKey[:]); err != nil {
		return err
	}

	privateKey := state.DHs.PrivateKey()
	if _, err := buf.Write(privateKey[:]); err != nil {
		return err
	}

	if err := binary.Write(&buf, byteOrder, state.PN); err != nil {
		return err
	}
	if err := binary.Write(&buf, byteOrder, uint32(state.Step)); err != nil {
		return err
	}
	if err := binary.Write(&buf, byteOrder, uint32(state.KeysCount)); err != nil {
		return err
	}

	//send chain
	if _, err := buf.Write(state.SendCh.CK[:]); err != nil {
		return err
	}
	if err := binary.Write(&buf, byteOrder, state.SendCh.N); err != nil {
		return err
	}

	//receive chain
	if _, err := buf.Write(state.RecvCh.CK[:]); err != nil {
		return err
	}
	if err := binary.Write(&buf, byteOrder, state.RecvCh.N); err != nil {
		return err
	}

	return saveEncryptedSession(id, buf.Bytes())
}

// Load session by id
func (s *BoltDBSessionStorage) Load(id []byte) (*doubleratchet.State, error) {
	sessionData, err := fetchEncryptedSession(id)
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(sessionData)

	rootChainKey, err := readKey(buf)
	if err != nil {
		return nil, err
	}

	state := doubleratchet.DefaultState(rootChainKey)
	if _, err = buf.Read(state.DHr[:]); err != nil {
		return nil, err
	}

	dhsPublic, err := readKey(buf)
	if err != nil {
		return nil, err
	}

	dhsPrivate, err := readKey(buf)
	if err != nil {
		return nil, err
	}

	state.DHs = DHPair{privateKey: dhsPrivate, publicKey: dhsPublic}
	if err = binary.Read(buf, byteOrder, &state.PN); err != nil {
		return nil, err
	}
	var uint32Val uint32

	if err = binary.Read(buf, byteOrder, &uint32Val); err != nil {
		return nil, err
	}
	state.Step = uint(uint32Val)

	if err = binary.Read(buf, byteOrder, &uint32Val); err != nil {
		return nil, err
	}
	state.KeysCount = uint(uint32Val)

	if _, err = buf.Read(state.SendCh.CK[:]); err != nil {
		return nil, err
	}
	if err = binary.Read(buf, byteOrder, &state.SendCh.N); err != nil {
		return nil, err
	}

	if _, err = buf.Read(state.RecvCh.CK[:]); err != nil {
		return nil, err
	}
	if err = binary.Read(buf, byteOrder, &state.RecvCh.N); err != nil {
		return nil, err
	}

	return &state, nil
}

func readKey(buf *bytes.Buffer) (doubleratchet.Key, error) {
	var keyBuf [32]byte
	_, err := buf.Read(keyBuf[:])
	return keyBuf, err
}

func toKey(a []byte) doubleratchet.Key {
	var k [32]byte
	copy(k[:], a)
	return k
}

func generateSessionID() (string, error) {
	var sessionIDBytes [32]byte
	_, err := rand.Read(sessionIDBytes[:])
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sessionIDBytes[:]), nil
}
