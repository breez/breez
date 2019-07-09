package config

import (
	"errors"
	"path"
	"sync"

	flags "github.com/jessevdk/go-flags"
)

const (
	configFile = "breez.conf"
)

var (
	cfg         *Config
	configError error
	once        sync.Once
)

/*
JobConfig hodls the job configuration
*/
type JobConfig struct {
	ConnectedPeers     []string `long:"peer"`
	AssertFilterHeader string   `long:"assertfilterheader"`
}

/*
Config holds the breez configuration
*/
type Config struct {
	WorkingDir         string
	RoutingNodeHost    string `long:"routingnodehost"`
	RoutingNodePubKey  string `long:"routingnodepubkey"`
	BreezServer        string `long:"breezserver"`
	Network            string `long:"network"`
	GrpcKeepAlive      bool   `long:"grpckeepalive"`
	BootstrapURL       string `long:"bootstrap"`
	ClosedChannelsURL  string `long:"closedchannelsurl"`
	BugReportURL       string `long:"bugreporturl"`
	BugReportURLSecret string `long:"bugreporturlsecret"`

	//Job Options
	JobCfg JobConfig `group:"Job Options"`
}

// GetConfig returns the config object
func GetConfig(workingDir string) (*Config, error) {
	once.Do(func() {
		configError = initConfig(workingDir)
	})
	return cfg, configError
}

func initConfig(workingDir string) error {
	c := &Config{WorkingDir: workingDir}
	if err := flags.IniParse(path.Join(workingDir, configFile), c); err != nil {
		return err
	}
	if len(c.RoutingNodeHost) == 0 || len(c.RoutingNodePubKey) == 0 {
		return errors.New("Breez must have routing node defined in the configuration file")
	}

	cfg = c
	return nil
}
