package conf

import (
	"bytes"
	_ "embed"

	"github.com/spf13/viper"
)

const (
	DefaultCompileConfigFileName = ".lahijan.conf.default.yaml"
	RuntimeConfigFileName        = ".lahijan.conf.yaml"
)

//go:embed .lahijan.conf.default.yaml
var defaultConfig []byte

type configSetupInfo struct {
	FoundConfigFile bool
}

func SetupConfig() (*configSetupInfo, error) {
	info := &configSetupInfo{
		FoundConfigFile: false,
	}
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(bytes.NewReader(defaultConfig)); err != nil {
		return info, err
	}
	viper.SetConfigName(DefaultCompileConfigFileName)
	viper.AddConfigPath("/etc/lahijan")
	viper.AddConfigPath("$HOME/.lahijan")
	viper.AddConfigPath(".")

	info.FoundConfigFile = true
	if err := viper.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			info.FoundConfigFile = false
		} else {
			return info, err
		}
	}
	viper.SetEnvPrefix("lahijan")
	viper.AutomaticEnv()
	return info, nil
}
