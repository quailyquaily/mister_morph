package configutil

import (
	"os"
	"strings"

	"github.com/spf13/viper"
)

// ExpandEnvStrings walks all config values in v and expands ${ENV_VAR}
// references in string values using os.ExpandEnv. Only keys whose values
// actually changed are written back (via v.Set), preserving viper's
// env/flag priority for unaffected keys.
func ExpandEnvStrings(v *viper.Viper) {
	if v == nil {
		return
	}
	for _, key := range v.AllKeys() {
		raw := v.Get(key)
		if expanded, changed := expandEnvAny(raw); changed {
			v.Set(key, expanded)
		}
	}
}

func expandEnvAny(val any) (any, bool) {
	switch v := val.(type) {
	case string:
		if !strings.Contains(v, "$") {
			return v, false
		}
		expanded := os.ExpandEnv(v)
		return expanded, expanded != v
	case map[string]any:
		changed := false
		for k, item := range v {
			if exp, c := expandEnvAny(item); c {
				v[k] = exp
				changed = true
			}
		}
		return v, changed
	case []any:
		changed := false
		for i, item := range v {
			if exp, c := expandEnvAny(item); c {
				v[i] = exp
				changed = true
			}
		}
		return v, changed
	default:
		return val, false
	}
}
