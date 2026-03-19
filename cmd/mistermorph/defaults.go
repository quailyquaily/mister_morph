package main

import (
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/spf13/viper"
)

func initViperDefaults() {
	configdefaults.Apply(viper.GetViper())
}
