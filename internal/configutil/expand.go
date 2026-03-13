package configutil

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// ReadExpandedConfig reads a YAML config file, expands all ${ENV_VAR}
// references in the raw text via os.ExpandEnv, then feeds the result
// into the provided viper instance.
//
// This approach is simpler and more complete than walking the parsed
// tree: it handles all value types and avoids viper.Set priority issues.
func ReadExpandedConfig(v *viper.Viper, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	expanded := os.ExpandEnv(string(raw))
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" {
		ext = "yaml"
	}
	v.SetConfigType(ext)
	return v.ReadConfig(strings.NewReader(expanded))
}
