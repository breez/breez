package account

import (
	"fmt"

	"github.com/nbd-wtf/go-nostr"
)

func(a *Service) GetKeyPair()(string, error){

	needsBackup := false
	
	nostrPrivateKey , err := a.breezDB.FetchNostrPrivKey(func() (string){
		needsBackup = true
		return nostr.GeneratePrivateKey()
	})
	if err != nil {
		return nostrPrivateKey, fmt.Errorf("failed to fetch nostrprivatekey %w", err)
	}

	if needsBackup {
		a.requestBackup()
	}

	nostrPublicKey , err := nostr.GetPublicKey(nostrPrivateKey)
	if err != nil {
		return "" , fmt.Errorf("failed to fetch nostrpubkey %w", err)
	}

	nostrKeyPair := nostrPrivateKey + "_"  + nostrPublicKey
	

	return nostrKeyPair , nil
}

func(a *Service) StoreImportedKey(privateKey string)(error){
	return a.breezDB.StoreNostrPrivKey(privateKey)
}

func (a* Service) DeleteKey()(error){
	return a.breezDB.DeletePresentKey();
}




