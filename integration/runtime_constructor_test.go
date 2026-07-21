package integration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/mcphost"
	"github.com/quailyquaily/mistermorph/llm"
)

func TestNewCheckedRejectsInvalidRuntimeConfig(t *testing.T) {
	cfg := invalidRuntimeConfig(t)

	rt, err := NewChecked(cfg)
	if rt != nil {
		t.Fatal("NewChecked() returned a runtime for invalid config")
	}
	if err == nil || !strings.Contains(err.Error(), "unknown logging.level") {
		t.Fatalf("NewChecked() error = %v, want logging validation error", err)
	}
}

func TestLegacyNewExposesRuntimeInitError(t *testing.T) {
	rt := New(invalidRuntimeConfig(t))
	if rt == nil {
		t.Fatal("New() returned nil; the compatibility constructor must keep returning a runtime")
	}
	if err := rt.Err(); err == nil || !strings.Contains(err.Error(), "unknown logging.level") {
		t.Fatalf("Runtime.Err() = %v, want logging validation error", err)
	}
}

func TestNilRuntimeErr(t *testing.T) {
	var rt *Runtime
	if err := rt.Err(); !errors.Is(err, errRuntimeNil) {
		t.Fatalf("nil Runtime.Err() = %v, want %v", err, errRuntimeNil)
	}
}

func TestRunTaskWithOptionsRejectsInvalidRuntimeBeforeSideEffects(t *testing.T) {
	clientBuildCalls := 0
	mcpConnectCalls := 0
	rt := newRuntime(invalidRuntimeConfig(t), runtimeBuildDependencies{
		buildClient: func(llmconfig.ClientConfig, llmutil.RuntimeValues) (llm.Client, error) {
			clientBuildCalls++
			return nil, nil
		},
		connectMCP: func(context.Context, []mcphost.ServerConfig, *slog.Logger) (mcpRegistration, error) {
			mcpConnectCalls++
			return mcpRegistration{}, nil
		},
	})
	journalDir := rt.snapshot().Paths.JournalDir
	if _, err := os.Stat(journalDir); !os.IsNotExist(err) {
		t.Fatalf("journal path exists before run: %q, error=%v", journalDir, err)
	}

	result, err := rt.RunTaskWithOptions(context.Background(), "ping", RunTaskOptions{PersistTask: true})
	if err == nil || !strings.Contains(err.Error(), "unknown logging.level") {
		t.Fatalf("RunTaskWithOptions() error = %v, want runtime init error", err)
	}
	if result.TaskID != "" || result.RunID != "" {
		t.Fatalf("RunTaskWithOptions() generated ids before validation: %+v", result)
	}
	if _, statErr := os.Stat(journalDir); !os.IsNotExist(statErr) {
		t.Fatalf("RunTaskWithOptions() created journal before validation: %q, error=%v", journalDir, statErr)
	}
	if clientBuildCalls != 0 || mcpConnectCalls != 0 {
		t.Fatalf("resource acquisition calls = client:%d MCP:%d, want 0", clientBuildCalls, mcpConnectCalls)
	}
}

func invalidRuntimeConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Set("file_state_dir", t.TempDir())
	cfg.Set("file_cache_dir", t.TempDir())
	cfg.Set("logging.level", "invalid-level")
	return cfg
}
