package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

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
	go func() {
		time.Sleep(25 * time.Second)
		os.Exit(0)
	}()
	res, err := runJob(workingDir)
	if err != nil {
		fmt.Println("Error in job", err)
	}
	fmt.Println("Breach detected: ", res)
	os.Exit(0)
	//tag:
	app, err := breez.NewApp(workingDir, &AppServicesImpl{})
	if err != nil {
		fmt.Println("Error creating App", err)
		os.Exit(1)
	}
	if err := app.Start(); err != nil {
		fmt.Println("Error creating App", err)
		os.Exit(1)
	}

	for {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter text: ")
		str, _ := reader.ReadString('\n')
		if str == "stop\n" {
			if err := app.Stop(); err != nil {
				fmt.Println("App stopped with error ", err)
			}
			return
		}
	}
}

func runJob(workingDir string) (r bool, err error) {
	job, err := sync.NewJob(workingDir)
	if err != nil {
		return false, err
	}

	return job.Run()
}
