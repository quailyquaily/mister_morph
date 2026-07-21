package consolecmd

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/spf13/viper"
)

type consoleLifecycleAuditSink struct {
	closeCalls int
}

func (*consoleLifecycleAuditSink) Emit(context.Context, guard.AuditEvent) error {
	return nil
}

func (s *consoleLifecycleAuditSink) Close() error {
	s.closeCalls++
	return nil
}

func TestConsoleAwarenessRegistryUsesAwarenessStaticTools(t *testing.T) {
	reader := viper.New()
	reader.Set("file_state_dir", t.TempDir())
	reader.Set("tools.contacts_send.enabled", true)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	baseRegistry, awarenessRegistry, mcpHost, err := buildConsoleRegistriesFromReader(context.Background(), logger, reader)
	if err != nil {
		t.Fatalf("buildConsoleRegistriesFromReader() error = %v", err)
	}
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
	reader.Set("guard.enabled", true)
	reader.Set("tools.contacts_send.enabled", true)
	reader.Set("acp.agents", []any{map[string]any{
		"name":    "test-agent",
		"command": "test-command",
	}})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	deps, cleanup, err := buildManagedRuntimeDepsFromReader(logger, reader)
	if err != nil {
		t.Fatalf("buildManagedRuntimeDepsFromReader() error = %v", err)
	}
	t.Cleanup(cleanup)

	if deps.AwarenessRegistry == nil {
		t.Fatal("AwarenessRegistry = nil")
	}
	if deps.ACPAgents == nil {
		t.Fatal("ACPAgents = nil")
	}
	agents := deps.ACPAgents()
	if len(agents) != 1 || agents[0].Name != "test-agent" {
		t.Fatalf("ACPAgents() = %#v, want configured agent", agents)
	}
	if _, ok := deps.Registry().Get(toolsutil.BuiltinContactsSend); ok {
		t.Fatalf("base registry includes %q, want excluded outside awareness", toolsutil.BuiltinContactsSend)
	}
	if _, ok := deps.AwarenessRegistry().Get(toolsutil.BuiltinContactsSend); !ok {
		t.Fatalf("awareness registry missing %q", toolsutil.BuiltinContactsSend)
	}
	firstGuard, err := deps.Guard(logger)
	if err != nil {
		t.Fatalf("first Guard() error = %v", err)
	}
	secondGuard, err := deps.Guard(logger)
	if err != nil {
		t.Fatalf("second Guard() error = %v", err)
	}
	if firstGuard == nil || secondGuard == nil {
		t.Fatal("Guard() returned nil for enabled guard")
	}
	defer func() {
		_ = firstGuard.Close()
		_ = secondGuard.Close()
	}()
	if firstGuard == secondGuard {
		t.Fatal("Guard() returned one shared instance, want caller-owned instances")
	}
}
