package configutil

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// envVarRe matches only the ${NAME} form (not bare $NAME).
// This avoids corrupting values like bcrypt hashes ($2a$10$...) or
// regex patterns that contain literal dollar signs.
var envVarRe = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// expandStrictEnv replaces only ${VAR} references with their environment
// values. Bare $VAR references are left untouched.
// Returns the expanded string and a list of referenced-but-unset variable names.
func expandStrictEnv(s string) (string, []string) {
	var missing []string
	result := envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		name := envVarRe.FindStringSubmatch(match)[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return val
	})
	return result, missing
}

// ReadExpandedConfig reads a config file, expands only ${ENV_VAR}
// references in the raw text, then feeds the result into the provided
// viper instance.
//
// Unset environment variables are replaced with empty strings and
// reported via the optional warn callback. Pass nil to suppress warnings.
func ReadExpandedConfig(v *viper.Viper, path string, warn func(format string, args ...any)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	expanded, missing := expandStrictEnv(string(raw))
	if len(missing) > 0 && warn != nil {
		warn("config %s: unset environment variable(s) replaced with empty string: %s",
			filepath.Base(path), strings.Join(missing, ", "))
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "" {
		ext = "yaml"
	}
	v.SetConfigType(ext)
	return v.ReadConfig(strings.NewReader(expanded))
}
