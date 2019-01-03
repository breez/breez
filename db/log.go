package db

import (
	"github.com/breez/lightninglib/daemon"
	"github.com/btcsuite/btclog"
)

var log btclog.Logger

func init() {
	log = daemon.BackendLog().Logger("BRUI")
}
