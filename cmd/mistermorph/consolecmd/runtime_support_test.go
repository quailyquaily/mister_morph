package consolecmd

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/viper"
)

func TestConsoleAwarenessRegistryUsesAwarenessStaticTools(t *testing.T) {
	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("tools.contacts_send.enabled", true)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	baseRegistry, awarenessRegistry, mcpHost := buildConsoleRegistriesFromReader(context.Background(), logger, reader)
	if mcpHost != nil {
		t.Cleanup(func() { _ = mcpHost.Close() })
	}

	if _, ok := baseRegistry.Get(toolsutil.BuiltinContactsSend); ok {
		t.Fatalf("base registry includes %q, want excluded outside awareness", toolsutil.BuiltinContactsSend)
	}
	if _, ok := awarenessRegistry.Get(toolsutil.BuiltinContactsSend); !ok {
		t.Fatalf("awareness registry missing %q", toolsutil.BuiltinContactsSend)
	}
}

func TestManagedRuntimeDepsExposeConsoleAwarenessRegistry(t *testing.T) {
	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("tools.contacts_send.enabled", true)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	deps, cleanup := buildManagedRuntimeDepsFromReader(logger, reader)
	t.Cleanup(cleanup)

	if deps.AwarenessRegistry == nil {
		t.Fatal("AwarenessRegistry = nil")
	}
	if _, ok := deps.Registry().Get(toolsutil.BuiltinContactsSend); ok {
		t.Fatalf("base registry includes %q, want excluded outside awareness", toolsutil.BuiltinContactsSend)
	}
	if _, ok := deps.AwarenessRegistry().Get(toolsutil.BuiltinContactsSend); !ok {
		t.Fatalf("awareness registry missing %q", toolsutil.BuiltinContactsSend)
	}
}
