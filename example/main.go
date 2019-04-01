package main

import (
	"bufio"
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
	workingDir := os.Getenv("LND_DIR")
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
				fmt.Println("App stopped with error: %v", err)
			}
			return
		}
	}

}
