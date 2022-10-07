package breez

import (
	"fmt"
	"strings"
)

const (
	currentVersion = "2022-04-06"
)

func (a *App) CheckVersion() error {
	versions, err := a.ServicesClient.Versions()
	if err != nil {
		return err
	}
	currentVersionExists := false
	var messages []string
	for _, v := range versions {
		if v == currentVersion {
			currentVersionExists = true
		}
		if strings.Contains(v, "legacy-zeroconf") {
			messages = append(messages, "no new channels")
		}
	}

	if !currentVersionExists {
		messages = append(messages, "bad version")
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf(strings.Join(messages, ","))
}
