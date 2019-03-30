package lnnotifier

import (
	"sync"

	"github.com/breez/breez/config"
	"github.com/breez/breez/lnnode"
	breezlog "github.com/breez/breez/log"
	"github.com/breez/lightninglib/lnrpc"
	"github.com/breez/lightninglib/subscribe"
	"github.com/btcsuite/btclog"
)

type LNNodeNotifier struct {
	started int32
	stopped int32
	wg      sync.WaitGroup
	log     btclog.Logger

	lnDaemon        *lnnode.Daemon
	lightningClient lnrpc.LightningClient
	ntfnServer      *subscribe.Server
}

func NewLNNotifier(
	cfg *config.Config,
	lightningClient lnrpc.LightningClient,
	lnDaemon *lnnode.Daemon) (*LNNodeNotifier, error) {
	logBackend, err := breezlog.GetLogBackend(cfg.WorkingDir)
	if err != nil {
		return nil, err
	}

	return &LNNodeNotifier{
		log:             logBackend.Logger("NNTFN"),
		lnDaemon:        lnDaemon,
		lightningClient: lightningClient,
		ntfnServer:      subscribe.NewServer(),
	}, nil
}
