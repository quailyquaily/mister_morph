//go:build wailsdesktop

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

const desktopCheckUpdateArg = "--check-update"

type desktopRuntimeConfig struct {
	AutoUpdate desktopAutoUpdateConfig
}

type desktopAutoUpdateConfig struct {
	Enabled bool
}

func defaultDesktopRuntimeConfig() desktopRuntimeConfig {
	return desktopRuntimeConfig{}
}

func loadDesktopRuntimeConfig(path string) (desktopRuntimeConfig, error) {
	cfg := defaultDesktopRuntimeConfig()
	path = strings.TrimSpace(path)
	if path == "" {
		return cfg, nil
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return cfg, fmt.Errorf("read desktop config: %w", err)
	}
	cfg.AutoUpdate.Enabled = v.GetBool("auto_update.enabled")
	return cfg, nil
}

func resolveDesktopConfigPath(args []string) (string, bool) {
	if explicit := strings.TrimSpace(extractConfigPathFromArgs(args)); explicit != "" {
		return filepath.Clean(pathutil.ExpandHomePath(explicit)), true
	}

	defaultPath := pathutil.DefaultConfigPath()
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, false
	}
	return "", false
}

func printDesktopConfigPath(scope string, cfgPath string, explicit bool) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "desktop"
	}
	source := "auto"
	if explicit {
		source = "explicit"
	}
	cfgPath = strings.TrimSpace(cfgPath)
	if cfgPath == "" {
		cfgPath = "(none)"
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s config path [%s]: %s\n", scope, source, cfgPath)
}

func extractConfigPathFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	for i := 0; i < len(args); i++ {
		item := strings.TrimSpace(args[i])
		if item == "" {
			continue
		}
		if item == "--config" && i+1 < len(args) {
			return strings.TrimSpace(pathutil.ExpandHomePath(args[i+1]))
		}
		if strings.HasPrefix(item, "--config=") {
			return strings.TrimSpace(pathutil.ExpandHomePath(strings.TrimPrefix(item, "--config=")))
		}
	}
	return ""
}

func hasDesktopCheckUpdateArg(args []string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == desktopCheckUpdateArg {
			return true
		}
	}
	return false
}
