package main

import (
	"fmt"
	"os"
	"path"

	"github.com/lightningnetwork/lnd/signal"

	"github.com/breez/breez/bindings"
)

type breezApp struct{}

func newBreezApp() *breezApp {
	return &breezApp{}
}

type ServicesImpl struct {
}

func (a *ServicesImpl) Notify(notificationEvent []byte) {
	fmt.Println("Daemon sent notification")
	workingDir := os.Getenv("LND_DIR")
	os.Chmod(path.Join(workingDir, "data", "chain", "bitcoin", "simnet", "admin.macaroon"), 0777)
}

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

	rpcBinding := &bindings.RPC{}
	rpcBinding.Start()
	i, err := signal.Intercept()
	if err != nil {
		fmt.Println("Error in signal.Intercept", err)
		os.Exit(1)
	}
	<-i.ShutdownChannel()
	fmt.Println("Shutdown requested")
	os.Exit(0)
}
