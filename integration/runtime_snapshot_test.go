package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

func TestRuntimeSnapshotValidatesAndCarriesDefaultWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Set("workspace_dir", workspaceDir)

	rt, err := NewChecked(cfg)
	if err != nil {
		t.Fatalf("NewChecked() error = %v", err)
	}
	if rt.snap.DefaultWorkspaceDir != workspaceDir {
		t.Fatalf("snapshot workspace = %q, want %q", rt.snap.DefaultWorkspaceDir, workspaceDir)
	}
	if got := rt.sharedDependencies(rt.snapshot()).DefaultWorkspaceDir; got != workspaceDir {
		t.Fatalf("common dependency workspace = %q, want %q", got, workspaceDir)
	}
	if got := rt.snap.StaticRegistry.Common.PathRoots.WorkspaceDir; got != "" {
		t.Fatalf("static registry workspace = %q, want empty before a run is prepared", got)
	}
}

func TestRuntimeSnapshotRejectsInvalidDefaultWorkspace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Set("workspace_dir", filepath.Join(t.TempDir(), "missing"))

	rt, err := NewChecked(cfg)
	if rt != nil {
		t.Fatal("NewChecked() returned a runtime for invalid workspace_dir")
	}
	if err == nil || !strings.Contains(err.Error(), "workspace dir does not exist") {
		t.Fatalf("NewChecked() error = %v, want workspace validation error", err)
	}
}

func TestRuntimeRegistryUsesDefaultWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Set("workspace_dir", workspaceDir)
	cfg.Set("file_cache_dir", t.TempDir())
	cfg.Set("tools.write_file.enabled", true)
	rt, err := NewChecked(cfg)
	if err != nil {
		t.Fatalf("NewChecked() error = %v", err)
	}

	writeFile, ok := rt.NewRegistry().Get("write_file")
	if !ok {
		t.Fatal("runtime registry is missing write_file")
	}
	if _, err := writeFile.Execute(context.Background(), map[string]any{
		"path":    "registry-output.txt",
		"content": "workspace output",
	}); err != nil {
		t.Fatalf("write_file.Execute() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workspaceDir, "registry-output.txt"))
	if err != nil {
		t.Fatalf("ReadFile(default workspace output) error = %v", err)
	}
	if string(data) != "workspace output" {
		t.Fatalf("output = %q, want workspace output", data)
	}
}

func TestRuntimeSnapshotFreezesRequestTimeout(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg := DefaultConfig()
	cfg.Set("llm.request_timeout", 5*time.Second)

	rt := New(cfg)

	if got := rt.RequestTimeout(); got != 5*time.Second {
		t.Fatalf("RequestTimeout() = %v, want %v", got, 5*time.Second)
	}

	viper.Set("llm.request_timeout", 99*time.Second)
	if got := rt.RequestTimeout(); got != 5*time.Second {
		t.Fatalf("RequestTimeout() changed after viper mutation: got %v, want %v", got, 5*time.Second)
	}
}

func TestRuntimeSnapshotFreezesRegistryToolConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg := DefaultConfig()
	cfg.Set("tools.write_file.enabled", false)

	rt := New(cfg)

	reg := rt.NewRegistry()
	if _, ok := reg.Get("write_file"); ok {
		t.Fatalf("write_file should be disabled by snapshot config")
	}

	viper.Set("tools.write_file.enabled", true)
	reg = rt.NewRegistry()
	if _, ok := reg.Get("write_file"); ok {
		t.Fatalf("write_file should remain disabled after viper mutation")
	}
}

func TestRuntimeSnapshotIgnoresGlobalViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("llm.request_timeout", 77*time.Second)

	rt := New(DefaultConfig())
	if got := rt.RequestTimeout(); got != 90*time.Second {
		t.Fatalf("RequestTimeout() = %v, want %v", got, 90*time.Second)
	}
}

func TestRuntimeSnapshotCarriesAgentSettingsIntoChannelDependencies(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg := DefaultConfig()
	cfg.Set("llm.model", "integration-model")
	runtime := New(cfg)

	viper.Set("llm.model", "global-model")
	reader := runtime.telegramDependencies(runtime.snapshot()).AgentSettingsReader
	if reader == nil {
		t.Fatal("channel dependencies are missing agent settings reader")
	}
	if got := reader.GetString("llm.model"); got != "integration-model" {
		t.Fatalf("channel settings model = %q, want integration-model", got)
	}
}

func TestRuntimeSnapshotLoadsInjectedEnvVarOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg := DefaultConfig()
	cfg.Set("tools.bash.injected_env_vars", []map[string]string{{
		"name":  "CUSTOM_BASH_ENV",
		"value": "fixed-value",
	}})

	rt := New(cfg)
	got := rt.snap.StaticRegistry.Bash.InjectedEnvVars
	if len(got) != 1 {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want one entry", got)
	}
	if got[0].Name != "CUSTOM_BASH_ENV" || got[0].Value != "fixed-value" {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want CUSTOM_BASH_ENV=fixed-value", got)
	}
}

func TestRuntimeSnapshotLoadsInjectedEnvVarScalarOverride(t *testing.T) {
	t.Setenv("CUSTOM_BASH_ENV", "from-parent")

	cfg := DefaultConfig()
	cfg.Set("tools.bash.injected_env_vars", "CUSTOM_BASH_ENV")

	rt := New(cfg)
	got := rt.snap.StaticRegistry.Bash.InjectedEnvVars
	if len(got) != 1 {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want one entry", got)
	}
	if got[0].Name != "CUSTOM_BASH_ENV" || got[0].Value != "from-parent" {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want CUSTOM_BASH_ENV=from-parent", got)
	}
}

func TestRuntimeSnapshotLoadsBashPathExtra(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Set("tools.bash.path_extra", []string{"/opt/tools/bin", " /custom/bin "})

	rt := New(cfg)
	got := rt.snap.StaticRegistry.Bash.PathExtra
	want := []string{"/opt/tools/bin", " /custom/bin "}
	if len(got) != len(want) {
		t.Fatalf("ToolsBashPathExtra = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ToolsBashPathExtra = %#v, want %#v", got, want)
		}
	}
}

func TestRuntimeSnapshotLoadsCoderPathExtra(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Set("tools.coder.path_extra", []string{"/opt/coder/bin", " /custom/coder/bin "})

	rt := New(cfg)
	got := rt.snap.Registry.ToolsCoderPathExtra
	want := []string{"/opt/coder/bin", " /custom/coder/bin "}
	if len(got) != len(want) {
		t.Fatalf("ToolsCoderPathExtra = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ToolsCoderPathExtra = %#v, want %#v", got, want)
		}
	}
}

func TestConfigAddPromptBlockAppliesTrimmedBlocks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddPromptBlock("  custom block one  ")
	cfg.AddPromptBlock("")
	cfg.AddPromptBlock("custom block two")

	rt := New(cfg)
	spec := agent.DefaultPromptSpec()
	baseBlocks := len(spec.Blocks)
	rt.appendPromptBlocks(&spec)

	if len(spec.Blocks) != baseBlocks+2 {
		t.Fatalf("prompt blocks = %d, want %d", len(spec.Blocks), baseBlocks+2)
	}
	if spec.Blocks[baseBlocks].Content != "custom block one" {
		t.Fatalf("first injected prompt block = %q, want %q", spec.Blocks[baseBlocks].Content, "custom block one")
	}
	if spec.Blocks[baseBlocks+1].Content != "custom block two" {
		t.Fatalf("second injected prompt block = %q, want %q", spec.Blocks[baseBlocks+1].Content, "custom block two")
	}
}
