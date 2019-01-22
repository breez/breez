package bootstrap

import (
	"io"
	"os"
	"path"
	"strings"

	"github.com/breez/breez/config"
)

var (
	paths = map[string]string{
		"block_headers.bin":      "data/chain/bitcoin/{{network}}",
		"reg_filter_headers.bin": "data/chain/bitcoin/{{network}}",
		"neutrino.db":            "data/chain/bitcoin/{{network}}",
		"wallet.db":              "data/chain/bitcoin/{{network}}",
		"channel.db":             "data/graph/{{network}}",
		"breez.db":               "",
	}
	defaultPath = "data/chain/bitcoin"
)

func copyFile(src, dest string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()

	to, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	return err
}

// PutFiles restore each received files in the right directory
// before lightninglib starts
func PutFiles(workingDir string, files []string) error {
	c, err := config.GetConfig(workingDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		basename := path.Base(f)
		p, ok := paths[basename]
		if !ok {
			p = defaultPath
		}
		destDir := path.Join(workingDir, strings.Replace(p, "{{network}}", c.Network, -1))
		if destDir != workingDir {
			err = os.MkdirAll(destDir, 0700)
			if err != nil {
				return err
			}
		}
		err = copyFile(f, path.Join(destDir, basename))
		if err != nil {
			return err
		}
	}
	return nil
}
