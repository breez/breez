package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/breez/breez/bindings"
)

type ServicesImpl struct {
}

func (a *ServicesImpl) Notify(notificationEvent []byte) {}

func (a *ServicesImpl) BackupProviderSignIn() (string, error) {
	return "", nil
}

func (a *ServicesImpl) BackupProviderName() string {
	return "gdrive"
}

func main() {
	workingDir := os.Getenv("LND_DIR")
	err := bindings.Init(os.TempDir(), workingDir, &ServicesImpl{})
	if err != nil {
		fmt.Println("Error in binding.Init", err)
		os.Exit(1)
	}

	err = bindings.Start(nil)
	if err != nil {
		fmt.Println("Error in binding.Start", err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')

}
