package chatcmd

import (
	"context"
	"log/slog"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/taskruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type chatLifecycleAuditSink struct {
	closeCalls int
}

func (*chatLifecycleAuditSink) Emit(context.Context, guard.AuditEvent) error {
	return nil
}

func (s *chatLifecycleAuditSink) Close() error {
	s.closeCalls++
	return nil
}

func TestBuildChatToolRegistryAcceptsNilResolvedRegistry(t *testing.T) {
	registry := buildChatToolRegistry(Dependencies{
		RegistryFromViper: func() *tools.Registry { return nil },
	}, nil)
	if registry == nil {
		t.Fatal("buildChatToolRegistry() returned nil")
	}
}

func TestChatSessionCleanupClosesTaskRuntimeGuard(t *testing.T) {
	sink := &chatLifecycleAuditSink{}
	route := llmutil.ResolvedRoute{ClientConfig: llmconfig.ClientConfig{Provider: "test", Model: "test-model"}}
	runtime, err := taskruntime.NewRunPreparer(depsutil.CommonDependencies{
		Logger:          func() (*slog.Logger, error) { return slog.Default(), nil },
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) { return route, nil },
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) { return &chatWeightedStubClient{}, nil },
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
		Guard: func(*slog.Logger) (*guard.Guard, error) {
			return guard.New(guard.Config{Enabled: true}, sink, nil), nil
		},
	}, taskruntime.BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewRunPreparer() error = %v", err)
	}
	sess := &chatSession{taskRuntime: runtime}

	sess.cleanup()
	if sink.closeCalls != 1 {
		t.Fatalf("guard close calls = %d, want 1", sink.closeCalls)
	}
}
