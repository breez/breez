package breez

/*
Log a message to lightninglib's logging system
*/
func (a *App) Log(msg string, lvl string) {
	switch lvl {
	case "FINEST":
	case "FINER":
	case "FINE":
		a.log.Tracef(msg)
	case "CONFIG":
		a.log.Debugf(msg)
	case "INFO":
		a.log.Infof(msg)
	case "WARNING":
		a.log.Warnf(msg)
	case "SEVERE":
		a.log.Errorf(msg)
	case "SHOUT":
		a.log.Criticalf(msg)
	default:
		a.log.Infof(msg)
	}
}

/*
GetLogPath returns the log file path.
*/
func GetLogPath() string {
	return a.cfg.workingDir + "/logs/bitcoin/" + a.cfg.Network + "/lnd.log"
}
