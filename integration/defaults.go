package integration

import (
	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/spf13/viper"
)

func ApplyViperDefaults(v *viper.Viper) {
	if v == nil {
		v = viper.GetViper()
	}
	configdefaults.Apply(v)
}
