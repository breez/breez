package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/breez/breez"
	"github.com/breez/breez/sync"
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
	workingDir := os.Getenv("LND_DIR")
	app, err := breez.NewApp(workingDir, &AppServicesImpl{})
	if err != nil {
		fmt.Println("Error creating App", err)
		os.Exit(1)
	}
	if err := app.Start(); err != nil {
		fmt.Println("Error creating App", err)
		os.Exit(1)
	}
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')

}

func runJob(workingDir string) (r bool, err error) {
	job, err := sync.NewJob(workingDir)
	if err != nil {
		return false, err
	}

	return job.Run()
}
