package consolecmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	"github.com/quailyquaily/mistermorph/internal/configutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	consoleRuntimeEnvPrefix    = "MISTER_MORPH"
	consoleConfigPollInterval  = time.Second
)

type consoleRuntimeOverrides map[string]any

type consoleConfigFingerprint struct {
	exists    bool
	size      int64
	modTimeNS int64
}

func captureConsoleRuntimeOverrides(cmd *cobra.Command) consoleRuntimeOverrides {
	if cmd == nil {
		return nil
	}
	flags := cmd.InheritedFlags()
	if flags == nil {
		return nil
	}

	out := consoleRuntimeOverrides{}
	setString := func(flagName, key string) {
		if !flags.Changed(flagName) {
			return
		}
		value, err := flags.GetString(flagName)
		if err != nil {
			return
		}
		out[key] = value
	}
	setBool := func(flagName, key string) {
		if !flags.Changed(flagName) {
			return
		}
		value, err := flags.GetBool(flagName)
		if err != nil {
			return
		}
		out[key] = value
	}
	setInt := func(flagName, key string) {
		if !flags.Changed(flagName) {
			return
		}
		value, err := flags.GetInt(flagName)
		if err != nil {
			return
		}
		out[key] = value
	}
	setStringArray := func(flagName, key string) {
		if !flags.Changed(flagName) {
			return
		}
		value, err := flags.GetStringArray(flagName)
		if err != nil {
			return
		}
		out[key] = append([]string(nil), value...)
	}

	setString("log-level", "logging.level")
	setString("log-format", "logging.format")
	setBool("log-add-source", "logging.add_source")
	setBool("log-include-thoughts", "logging.include_thoughts")
	setBool("log-include-tool-params", "logging.include_tool_params")
	setBool("log-include-skill-contents", "logging.include_skill_contents")
	setInt("log-max-thought-chars", "logging.max_thought_chars")
	setInt("log-max-json-bytes", "logging.max_json_bytes")
	setInt("log-max-string-value-chars", "logging.max_string_value_chars")
	setInt("log-max-skill-content-chars", "logging.max_skill_content_chars")
	setStringArray("log-redact-key", "logging.redact_keys")

	if len(out) == 0 {
		return nil
	}
	return out
}

func (o consoleRuntimeOverrides) apply(v *viper.Viper) {
	if v == nil {
		return
	}
	for key, value := range o {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		v.Set(key, value)
	}
}

func loadConsoleRuntimeConfig(configPath string, overrides consoleRuntimeOverrides) (*viper.Viper, error) {
	v := viper.New()
	configdefaults.Apply(v)
	v.SetEnvPrefix(consoleRuntimeEnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	overrides.apply(v)

	configPath = pathutil.ExpandHomePath(strings.TrimSpace(configPath))
	if configPath != "" {
		if err := configutil.ReadExpandedConfig(v, configPath, nil); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		expandConfiguredDirKeyOnReader(v, "file_state_dir")
		expandConfiguredDirKeyOnReader(v, "file_cache_dir")
	}
	return v, nil
}

func expandConfiguredDirKeyOnReader(v *viper.Viper, key string) {
	if v == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	raw := strings.TrimSpace(v.GetString(key))
	if raw == "" {
		return
	}
	v.Set(key, pathutil.ExpandHomePath(raw))
}

func fingerprintConfigPath(path string) (consoleConfigFingerprint, error) {
	path = pathutil.ExpandHomePath(strings.TrimSpace(path))
	if path == "" {
		return consoleConfigFingerprint{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return consoleConfigFingerprint{}, nil
		}
		return consoleConfigFingerprint{}, fmt.Errorf("stat config: %w", err)
	}
	return consoleConfigFingerprint{
		exists:    true,
		size:      info.Size(),
		modTimeNS: info.ModTime().UnixNano(),
	}, nil
}
