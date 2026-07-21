package main

import (
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/viper"
)

type registryConfig = toolsutil.StaticRegistryConfig

func loadRegistryConfigFromViper() (registryConfig, error) {
	configdefaults.Apply(viper.GetViper())
	return toolsutil.StaticRegistryConfigFromReader(viper.GetViper())
}
