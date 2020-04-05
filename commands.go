package breez

import (
	"github.com/breez/breez/lncli"
	"github.com/breez/breez/lnnode"
)

func (a *App) SendCommand(command string) (string, error) {
	newConection, err := lnnode.NewClientConnection(a.cfg)
	if err != nil {
		return "", err
	}
	return lncli.RunCommand(command, newConection)
}
