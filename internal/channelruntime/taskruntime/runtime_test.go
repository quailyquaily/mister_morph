package taskruntime

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type stubTaskRuntimeClient struct {
	requests []llm.Request
}

func (c *stubTaskRuntimeClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
}

func TestBootstrapReusesMainClientForSamePlanProfile(t *testing.T) {
	var createCalls int
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			createCalls++
			return &stubTaskRuntimeClient{}, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig:          agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
		DefaultModelFallback: "gpt-5.2",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("CreateLLMClient calls = %d, want 1", createCalls)
	}
	if rt.MainClient != rt.PlanClient {
		t.Fatal("PlanClient should reuse MainClient for same profile")
	}
}

func TestRunAppliesPromptAugmentAndMemoryHooks(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	route := llmutil.ResolvedRoute{
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
	}
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return route, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig:          agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
		DefaultModelFallback: "gpt-5.2",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	var (
		prepareCalls int
		recordCalls  int
		notifyCalls  int
	)
	result, err := rt.Run(context.Background(), RunRequest{
		Task:  "ping",
		Model: "gpt-5.4",
		Scene: "test.loop",
		PromptAugment: func(spec *agent.PromptSpec, _ *tools.Registry) {
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: "channel block"})
		},
		Memory: MemoryHooks{
			Source:            "test",
			SubjectID:         "test:main",
			InjectionEnabled:  true,
			InjectionMaxItems: 3,
			PrepareInjection: func(maxItems int) (string, error) {
				prepareCalls++
				if maxItems != 3 {
					t.Fatalf("PrepareInjection maxItems = %d, want 3", maxItems)
				}
				return "memory snapshot", nil
			},
			ShouldRecord: func(*agent.Final) bool {
				return true
			},
			Record: func(_ *agent.Final, finalOutput string) error {
				recordCalls++
				if strings.TrimSpace(finalOutput) != "ok" {
					t.Fatalf("finalOutput = %q, want ok", finalOutput)
				}
				return nil
			},
			NotifyRecorded: func() {
				notifyCalls++
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Final == nil {
		t.Fatal("Run() final is nil")
	}
	if prepareCalls != 1 {
		t.Fatalf("PrepareInjection calls = %d, want 1", prepareCalls)
	}
	if recordCalls != 1 {
		t.Fatalf("Record calls = %d, want 1", recordCalls)
	}
	if notifyCalls != 1 {
		t.Fatalf("NotifyRecorded calls = %d, want 1", notifyCalls)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if client.requests[0].Model != "gpt-5.4" {
		t.Fatalf("request model = %q, want gpt-5.4", client.requests[0].Model)
	}
	if client.requests[0].Scene != "test.loop" {
		t.Fatalf("request scene = %q, want test.loop", client.requests[0].Scene)
	}
	var systemPrompt string
	for _, msg := range client.requests[0].Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			break
		}
	}
	if !strings.Contains(systemPrompt, "channel block") {
		t.Fatalf("system prompt missing prompt augment block: %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "memory snapshot") {
		t.Fatalf("system prompt missing memory snapshot: %q", systemPrompt)
	}
}
