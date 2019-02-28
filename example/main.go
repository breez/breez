package main

import (
	"fmt"
	"os"

	"github.com/breez/breez"
)

type Auth struct {
	Token string
}

func (a *Auth) SignIn() (string, error) {
	return a.Token, nil
}

type AppServicesImpl struct {
}

func (a *AppServicesImpl) BackupProviderName() string {
	return "gdrive"
}

func (a *AppServicesImpl) BackupProviderSignIn() (string, error) {
	return "", nil
}

func main() {
	if err := breez.Init(os.Getenv("LND_DIR"), &AppServicesImpl{}); err != nil {
		fmt.Println("Error Init breez", err)
		os.Exit(1)
	}
	notifChannel, err := breez.Start()
	if err != nil {
		fmt.Println("Error starting breez", err)
		os.Exit(1)
	}
	go func() {
		for {
			<-notifChannel
		}
	}()
	breez.WaitDaemonShutdown()
}
