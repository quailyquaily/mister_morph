package integration

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/spf13/viper"
)

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

func TestRuntimeSnapshotLoadsInjectedEnvVarOverrides(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg := DefaultConfig()
	cfg.Set("tools.bash.injected_env_vars", []map[string]string{{
		"name":  "CUSTOM_BASH_ENV",
		"value": "fixed-value",
	}})

	rt := New(cfg)
	got := rt.snap.Registry.ToolsBashInjectedEnvVars
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
	got := rt.snap.Registry.ToolsBashInjectedEnvVars
	if len(got) != 1 {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want one entry", got)
	}
	if got[0].Name != "CUSTOM_BASH_ENV" || got[0].Value != "from-parent" {
		t.Fatalf("ToolsBashInjectedEnvVars = %+v, want CUSTOM_BASH_ENV=from-parent", got)
	}
}

func TestConfigAddPromptBlockAppliesTrimmedBlocks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AddPromptBlock("  custom block one  ")
	cfg.AddPromptBlock("")
	cfg.AddPromptBlock("custom block two")

	rt := New(cfg)
	spec := agent.DefaultPromptSpec()
	rt.appendPromptBlocks(&spec)

	if len(spec.Blocks) != 2 {
		t.Fatalf("prompt blocks = %d, want 2", len(spec.Blocks))
	}
	if spec.Blocks[0].Content != "custom block one" {
		t.Fatalf("first prompt block = %q, want %q", spec.Blocks[0].Content, "custom block one")
	}
	if spec.Blocks[1].Content != "custom block two" {
		t.Fatalf("second prompt block = %q, want %q", spec.Blocks[1].Content, "custom block two")
	}
}
