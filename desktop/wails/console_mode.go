//go:build wailsdesktop

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/cmd/mistermorph/consolecmd"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

func maybeRunDesktopConsoleServe(args []string) (bool, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) != desktopConsoleServeArgV1 {
		return false, nil
	}

	serveArgs := append([]string(nil), args[1:]...)
	if err := initDesktopConsoleViper(serveArgs); err != nil {
		return true, err
	}

	cmd := consolecmd.New()
	cmd.SetArgs(append([]string{"serve"}, serveArgs...))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return true, cmd.Execute()
}

func initDesktopConsoleViper(args []string) error {
	viper.Reset()
	initDesktopConsoleDefaults()

	viper.SetEnvPrefix("MISTER_MORPH")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	cfgPath, explicit := resolveDesktopConfigPath(args)
	if strings.TrimSpace(cfgPath) == "" {
		return nil
	}

	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		if !explicit && isDesktopConfigNotFound(err) {
			return nil
		}
		return fmt.Errorf("read config: %w", err)
	}
	return nil
}

func initDesktopConsoleDefaults() {
	viper.SetDefault("llm.provider", "openai")
	viper.SetDefault("llm.endpoint", "https://api.openai.com")
	viper.SetDefault("llm.model", "gpt-5.2")
	viper.SetDefault("llm.api_key", "")

	viper.SetDefault("file_state_dir", "~/.morph")

	viper.SetDefault("console.listen", "127.0.0.1:9080")
	viper.SetDefault("console.base_path", "/")
	viper.SetDefault("console.static_dir", "")
	viper.SetDefault("console.password", "")
	viper.SetDefault("console.password_hash", "")
	viper.SetDefault("console.session_ttl", 12*time.Hour)
	viper.SetDefault("console.endpoints", []map[string]any{})
	viper.SetDefault("console.setup_mode", false)
	viper.SetDefault("console.setup_require_llm", true)
}

func resolveDesktopConfigPath(args []string) (string, bool) {
	if explicit := strings.TrimSpace(extractConfigPathFromArgs(args)); explicit != "" {
		return filepath.Clean(pathutil.ExpandHomePath(explicit)), true
	}

	for _, candidate := range []string{"config.yaml", "~/.morph/config.yaml"} {
		resolved := filepath.Clean(pathutil.ExpandHomePath(candidate))
		if _, err := os.Stat(resolved); err == nil {
			return resolved, false
		}
	}
	return "", false
}

func isDesktopConfigNotFound(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var notFound viper.ConfigFileNotFoundError
	return errors.As(err, &notFound)
}
