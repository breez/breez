package main

import (
	"fmt"
	"os"

	"github.com/breez/breez/config"
	"github.com/breez/breez/db"
	"github.com/breez/breez/lnnode"
)

func daemonSample() {
	workingDir := os.Getenv("LND_DIR")

	cfg, err := config.GetConfig(workingDir)
	if err != nil {
		fmt.Println("Warning initConfig", err)
		os.Exit(1)
	}

	db, release, err := db.Get(cfg.WorkingDir)
	if err != nil {
		fmt.Println("Failed to create breez db", err)
		os.Exit(1)
	}

	defer release()
	lnDaemon, err := lnnode.NewDaemon(cfg, db)
	if err != nil {
		fmt.Println("Failed to create daemon", err)
		os.Exit(1)
	}

	if err := lnDaemon.Start(); err != nil {
		fmt.Println("Failed to start daemon", err)
		os.Exit(1)
	}
	if err := subscribeEvents(lnDaemon); err != nil {
		fmt.Println("Failed to subscribeEvents daemon", err)
		os.Exit(1)
	}
	lnDaemon.Stop()
}

func subscribeEvents(lnDaemon *lnnode.Daemon) error {
	client, err := lnDaemon.SubscribeEvents()
	if err != nil {
		return err
	}

	defer client.Cancel()

	if err != nil {
		return err
	}
	i := 0
	for {
		select {
		case u := <-client.Updates():
			fmt.Println("************Daemon Event Received")
			switch msg := u.(type) {
			case lnnode.DaemonDownEvent:
				fmt.Println("Shutting down")
				i++
				if i == 4 {
					return nil
				}
				if err := lnDaemon.RestartDaemon(); err != nil {
					fmt.Println("Failed to start daemon")
					return err
				}
			default:
				fmt.Println("************Daemon Event Received: ", msg)
			}
		case <-client.Quit():
			return nil
		}
	}
}
