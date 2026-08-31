package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func rootCommandForTest(t *testing.T) *cobra.Command {
	t.Helper()
	runtime := newRootRuntime()
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime.command
}

func TestShouldPrepareRootRegistry(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "mistermorph run", want: true},
		{path: "mistermorph chat", want: true},
		{path: "mistermorph telegram", want: true},
		{path: "mistermorph slack", want: true},
		{path: "mistermorph line", want: true},
		{path: "mistermorph lark", want: true},
		{path: "mistermorph mixin", want: true},
		{path: "mistermorph tools", want: true},
		{path: "mistermorph console serve", want: false},
		{path: "mistermorph version", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			parts := strings.Fields(tt.path)
			root := &cobra.Command{Use: parts[0]}
			current := root
			for _, name := range parts[1:] {
				child := &cobra.Command{Use: name}
				current.AddCommand(child)
				current = child
			}
			if got := shouldPrepareRootRegistry(current); got != tt.want {
				t.Fatalf("shouldPrepareRootRegistry(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestRootCommandRejectsExplicitMissingConfig(t *testing.T) {
	resetRootConfigForTest(t)

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	cmd := rootCommandForTest(t)
	cmd.SetArgs([]string{"--config", missing, "run", "--task", "test"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	_, err := cmd.ExecuteC()
	if err == nil {
		t.Fatal("ExecuteC() error = nil, want explicit config error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("ExecuteC() error = %q, want config path %q", err, missing)
	}
}

func TestRootCommandRejectsMalformedConfigForChat(t *testing.T) {
	resetRootConfigForTest(t)

	path := writeMalformedConfig(t)
	cmd := rootCommandForTest(t)
	cmd.SetArgs([]string{"--config", path, "chat"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	_, err := cmd.ExecuteC()
	if err == nil {
		t.Fatal("ExecuteC() error = nil, want malformed config error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("ExecuteC() error = %q, want config path %q", err, path)
	}
}

func TestRootCommandRejectsMalformedConfigForRun(t *testing.T) {
	resetRootConfigForTest(t)

	path := writeMalformedConfig(t)
	cmd := rootCommandForTest(t)
	cmd.SetArgs([]string{"--config", path, "run", "--task", "test"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	_, err := cmd.ExecuteC()
	if err == nil {
		t.Fatal("ExecuteC() error = nil, want malformed config error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("ExecuteC() error = %q, want config path %q", err, path)
	}
}

func TestRootPreflightAppliesCLISecretBeforeResolvingConfigSecret(t *testing.T) {
	resetRootConfigForTest(t)
	workspaceDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := "workspace_dir: " + workspaceDir + "\nllm:\n  provider: openai\n  model: gpt-test\n  api_key: secret://os/b_LsX7HLzAR3OShG7YjRcw\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.Set("config", configPath)
	cmd := rootCommandForTest(t)
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Flags().Set("api-key", "cli-api-key"); err != nil {
		t.Fatal(err)
	}

	if err := runRootPreflight(run, nil); err != nil {
		t.Fatalf("runRootPreflight() error = %v", err)
	}
	if got := viper.GetString("llm.api_key"); got != "cli-api-key" {
		t.Fatalf("llm.api_key = %q, want CLI override", got)
	}
}

func TestRootCommandAllowsMalformedConfigOnlyForConsoleRepair(t *testing.T) {
	resetRootConfigForTest(t)

	path := writeMalformedConfig(t)
	cmd := rootCommandForTest(t)
	cmd.SetArgs([]string{"--config", path, "console", "serve"})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	serve, _, err := cmd.Find([]string{"console", "serve"})
	if err != nil {
		t.Fatalf("Find(console serve) error = %v", err)
	}
	if err := cmd.PersistentFlags().Set("config", path); err != nil {
		t.Fatalf("Set(config) error = %v", err)
	}
	if err := cmd.PersistentPreRunE(serve, nil); err != nil {
		t.Fatalf("PersistentPreRunE(console serve) error = %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "repair mode") || !strings.Contains(got, path) {
		t.Fatalf("stderr = %q, want visible repair-mode config error", got)
	}
}

func TestLoadRootConfigValidatesWorkspaceDir(t *testing.T) {
	resetRootConfigForTest(t)
	workspaceDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("workspace_dir: "+workspaceDir+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	viper.Set("config", configPath)

	if err := loadRootConfig(nil); err != nil {
		t.Fatalf("loadRootConfig() error = %v", err)
	}
	if got := viper.GetString("workspace_dir"); got != workspaceDir {
		t.Fatalf("workspace_dir = %q, want %q", got, workspaceDir)
	}
}

func TestLoadRootConfigRejectsInvalidWorkspaceDir(t *testing.T) {
	resetRootConfigForTest(t)
	missingDir := filepath.Join(t.TempDir(), "missing")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("workspace_dir: "+missingDir+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	viper.Set("config", configPath)

	err := loadRootConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "workspace dir does not exist") {
		t.Fatalf("loadRootConfig() error = %v, want missing workspace error", err)
	}
}

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

func TestResolveConfigFile_DefaultOrderIgnoresCWD(t *testing.T) {
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
	want := filepath.Join(morphDir, "config.yaml")
	if got != filepath.Clean(want) {
		t.Fatalf("resolveConfigFile() path = %q, want %q", got, filepath.Clean(want))
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

func resetRootConfigForTest(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(viper.Reset)
}

func writeMalformedConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("llm: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	return path
}
