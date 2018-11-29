package bootstrap

import (
	"io"
	"os"
	"path"

	"github.com/breez/breez"
)

var (
	paths = map[string]string{
		"block_headers.bin":      "data/chain/bitcoin",
		"reg_filter_headers.bin": "data/chain/bitcoin",
		"neutrino.db":            "data/chain/bitcoin",
		"wallet.db":              "data/chain/bitcoin",
		"channel.db":             "data/graph",
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
	c, err := breez.GetConfig(workingDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		basename := path.Base(f)
		p, ok := paths[basename]
		if !ok {
			p = defaultPath
		}
		destDir := path.Join(workingDir, p, c.Network)
		err = os.MkdirAll(destDir, 0700)
		if err != nil {
			return err
		}
		err = copyFile(f, path.Join(destDir, basename))
		if err != nil {
			return err
		}
	}
	return nil
}
