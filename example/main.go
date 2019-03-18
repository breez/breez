package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/breez/breez"
	"github.com/breez/breez/config"
	"github.com/breez/breez/lnnode"
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
	for {
		notifier := make(chan lnnode.DaemonState)
		d := newDaemon(workingDir, notifier)
		go func() {
			time.Sleep(4 * time.Second)
			fmt.Println("Stopping daemon*******")
			err := d.Stop()
			if err != nil {
				fmt.Println("Error in stopping daemon*******")
			}
		}()
		runDaemon(d, notifier)
	}
	return
	if err := breez.Init(workingDir, &AppServicesImpl{}); err != nil {
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

func newDaemon(workingDir string, notifier chan lnnode.DaemonState) *lnnode.Daemon {

	cfg, err := config.GetConfig(workingDir)
	if err != nil {
		fmt.Println("Error starting breez", err)
		os.Exit(1)
	}

	d, err := lnnode.NewDaemon(cfg, notifier)
	if err != nil {
		fmt.Println("Error starting breez", err)
		os.Exit(1)
	}
	return d
}

func runDaemon(d *lnnode.Daemon, notifier chan lnnode.DaemonState) {

	err := d.Start()
	if err != nil {
		fmt.Println("Daemon Not Started!")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case n := <-notifier:
				fmt.Println("got daemon notification: ", n)
				if n == lnnode.DaemonStopped {
					return
				}
			}
		}
	}()
	wg.Wait()
	fmt.Println("Daemon Stopped!")
}
