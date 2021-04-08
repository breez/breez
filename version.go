package breez

import "fmt"

const (
	currentVersion = "2021-04-08"
)

func (a *App) CheckVersion() error {
	versions, err := a.ServicesClient.Versions()
	if err != nil {
		return nil
	}
	for _, v := range versions {
		if v == currentVersion {
			return nil
		}
	}
	return fmt.Errorf("bad version")
}
