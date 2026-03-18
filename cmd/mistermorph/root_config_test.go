package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestResolveConfigFile_ExplicitFlagWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreConfigKey(t)
	viper.Set("config", "~/custom.yaml")

	got, explicit := resolveConfigFile()
	want := filepath.Join(home, "custom.yaml")
	if got != filepath.Clean(want) {
		t.Fatalf("resolveConfigFile() path = %q, want %q", got, filepath.Clean(want))
	}
	if !explicit {
		t.Fatalf("resolveConfigFile() explicit = false, want true")
	}
}

func TestResolveConfigFile_DefaultOrderPrefersCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreConfigKey(t)
	viper.Set("config", "")

	wd := t.TempDir()
	restoreWD(t, wd)
	if err := os.WriteFile("config.yaml", []byte("llm:\n  provider: openai\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	morphDir := filepath.Join(home, ".morph")
	if err := os.MkdirAll(morphDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(~/.morph) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(morphDir, "config.yaml"), []byte("llm:\n  provider: anthropic\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(~/.morph/config.yaml) error = %v", err)
	}

	got, explicit := resolveConfigFile()
	if got != filepath.Clean("config.yaml") {
		t.Fatalf("resolveConfigFile() path = %q, want %q", got, filepath.Clean("config.yaml"))
	}
	if explicit {
		t.Fatalf("resolveConfigFile() explicit = true, want false")
	}
}

func TestResolveConfigFile_DefaultFallsBackToHomeMorph(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreConfigKey(t)
	viper.Set("config", "")

	wd := t.TempDir()
	restoreWD(t, wd)

	morphDir := filepath.Join(home, ".morph")
	if err := os.MkdirAll(morphDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(~/.morph) error = %v", err)
	}
	homeCfg := filepath.Join(morphDir, "config.yaml")
	if err := os.WriteFile(homeCfg, []byte("llm:\n  provider: openai\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(~/.morph/config.yaml) error = %v", err)
	}

	got, explicit := resolveConfigFile()
	if got != filepath.Clean(homeCfg) {
		t.Fatalf("resolveConfigFile() path = %q, want %q", got, filepath.Clean(homeCfg))
	}
	if explicit {
		t.Fatalf("resolveConfigFile() explicit = true, want false")
	}
}

func TestResolveConfigFile_DefaultMissingReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	restoreConfigKey(t)
	viper.Set("config", "")

	wd := t.TempDir()
	restoreWD(t, wd)

	got, explicit := resolveConfigFile()
	if got != "" {
		t.Fatalf("resolveConfigFile() path = %q, want empty", got)
	}
	if explicit {
		t.Fatalf("resolveConfigFile() explicit = true, want false")
	}
}

func TestDeprecatedConfigWarnings_ServerListenFromConfig(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  listen: 127.0.0.1:8787\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config.yaml) error = %v", err)
	}

	cfg := viper.New()
	cfg.SetConfigFile(cfgPath)
	if err := cfg.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig() error = %v", err)
	}

	warnings := deprecatedConfigWarnings(cfg, nil)
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1", len(warnings))
	}
	if !strings.Contains(warnings[0], "server.listen is deprecated") {
		t.Fatalf("warning = %q, want deprecation text", warnings[0])
	}
}

func TestDeprecatedConfigWarnings_ServerListenFromEnv(t *testing.T) {
	cfg := viper.New()
	cfg.Set("server.listen", "127.0.0.1:8787")

	warnings := deprecatedConfigWarnings(cfg, func(key string) (string, bool) {
		if key == envPrefix+"_SERVER_LISTEN" {
			return "127.0.0.1:8787", true
		}
		return "", false
	})
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1", len(warnings))
	}
}

func restoreConfigKey(t *testing.T) {
	t.Helper()
	prev, had := viper.Get("config"), viper.IsSet("config")
	t.Cleanup(func() {
		if had {
			viper.Set("config", prev)
			return
		}
		viper.Set("config", nil)
	})
}

func restoreWD(t *testing.T, wd string) {
	t.Helper()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("Chdir(%q) error = %v", wd, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}
