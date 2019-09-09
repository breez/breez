package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"
	"io/ioutil"
	"os"	
)

func encryptFile(source, dest string, key []byte) error {
	fileContent, err := ioutil.ReadFile(source)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	encryptedContent := aesgcm.Seal(nonce, nonce, fileContent, nil)

	if err = ioutil.WriteFile(dest, encryptedContent, os.ModePerm); err != nil {
		return err
	}

	return nil
}

func decryptFile(source, dest string, key []byte) error {	
	fileContent, err := ioutil.ReadFile(source)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonceSize := aesgcm.NonceSize()
	nonce, cipherContent := fileContent[:nonceSize], fileContent[nonceSize:]

	decryptedContent, err := aesgcm.Open(nil, nonce, cipherContent, nil)
	if err != nil {
		return err
	}

	if err = ioutil.WriteFile(dest, decryptedContent, os.ModePerm); err != nil {
		return err
	}

	return nil
}
