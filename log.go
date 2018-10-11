package breez

import (
	"github.com/breez/lightninglib/daemon"
)

var log = daemon.BackendLog().Logger("BRUI")

/*
Log a message to lightninglib's logging system
*/
func Log(msg string, lvl string) {
	if isReady {
		switch lvl {
		case "FINEST":
		case "FINER":
		case "FINE":
			log.Tracef(msg)
		case "CONFIG":
			log.Debugf(msg)
		case "INFO":
			log.Infof(msg)
		case "WARNING":
			log.Warnf(msg)
		case "SEVERE":
			log.Errorf(msg)
		case "SHOUT":
			log.Criticalf(msg)
		default:
			log.Infof(msg)
		}
	}
}

func GetLogPath() (string) {
	return appWorkingDir + "/logs/bitcoin/" + cfg.Network + "/lnd.log"
}
