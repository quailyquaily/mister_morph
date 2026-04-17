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
	result   llm.Result
}

func (c *stubTaskRuntimeClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	if c.result.Text == "" && c.result.JSON == nil && len(c.result.ToolCalls) == 0 && len(c.result.Parts) == 0 {
		return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
	}
	return c.result, nil
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
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("CreateLLMClient calls = %d, want 1", createCalls)
	}
	if rt.BootstrapMainClient != rt.PlanClient {
		t.Fatal("PlanClient should reuse BootstrapMainClient for same profile")
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
		PromptAugment: func(spec *agent.PromptSpec, _ *tools.Registry) {
			spec.Blocks = append(spec.Blocks, agent.PromptBlock{Content: "integration block"})
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
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
	msgs := client.requests[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	systemPrompt := msgs[0].Content
	if !strings.Contains(systemPrompt, "channel block") {
		t.Fatalf("system prompt missing prompt augment block: %q", systemPrompt)
	}
	if strings.Contains(systemPrompt, "memory snapshot") {
		t.Fatalf("system prompt should not contain memory snapshot: %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "integration block") {
		t.Fatalf("system prompt missing common prompt augment block: %q", systemPrompt)
	}
	if !strings.Contains(msgs[1].Content, "mister_morph_meta") {
		t.Fatalf("messages[1] = %q, want injected meta", msgs[1].Content)
	}
	if msgs[2].Role != "user" || !strings.Contains(msgs[2].Content, "[[ Runtime Memory ]]") {
		t.Fatalf("messages[2] = %#v, want runtime memory message", msgs[2])
	}
	if !strings.Contains(msgs[2].Content, "memory snapshot") {
		t.Fatalf("messages[2] = %q, want memory snapshot", msgs[2].Content)
	}
	if msgs[3].Content != "ping" {
		t.Fatalf("messages[3] = %q, want task", msgs[3].Content)
	}
}

func TestBootstrapLeavesMainModelEmptyWhenRouteModelMissing(t *testing.T) {
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    "",
				},
			}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return &stubTaskRuntimeClient{}, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, _ string, _ []string) (agent.PromptSpec, []string, error) {
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if rt.BootstrapMainModel != "" {
		t.Fatalf("BootstrapMainModel = %q, want empty", rt.BootstrapMainModel)
	}
}

func TestRunResolvesMainModelLate(t *testing.T) {
	client := &stubTaskRuntimeClient{}
	currentModel := "gpt-5.2"
	rt, err := Bootstrap(depsutil.CommonDependencies{
		Logger: func() (*slog.Logger, error) {
			return slog.Default(), nil
		},
		LogOptions: func() agent.LogOptions {
			return agent.LogOptions{}
		},
		ResolveLLMRoute: func(string) (llmutil.ResolvedRoute, error) {
			return llmutil.ResolvedRoute{
				ClientConfig: llmconfig.ClientConfig{
					Provider: "openai",
					Model:    currentModel,
				},
			}, nil
		},
		CreateLLMClient: func(llmutil.ResolvedRoute) (llm.Client, error) {
			return client, nil
		},
		Registry: func() *tools.Registry {
			return tools.NewRegistry()
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, _ string, _ llm.Client, model string, _ []string) (agent.PromptSpec, []string, error) {
			if strings.TrimSpace(model) == "" {
				t.Fatal("PromptSpec received empty model")
			}
			return agent.DefaultPromptSpec(), nil, nil
		},
	}, BootstrapOptions{
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if _, err := rt.Run(context.Background(), RunRequest{Task: "first"}); err != nil {
		t.Fatalf("Run(first) error = %v", err)
	}
	currentModel = "gpt-4.1-mini"
	if _, err := rt.Run(context.Background(), RunRequest{Task: "second"}); err != nil {
		t.Fatalf("Run(second) error = %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("client requests = %d, want 2", len(client.requests))
	}
	if got := client.requests[0].Model; got != "gpt-5.2" {
		t.Fatalf("first request model = %q, want gpt-5.2", got)
	}
	if got := client.requests[1].Model; got != "gpt-4.1-mini" {
		t.Fatalf("second request model = %q, want gpt-4.1-mini", got)
	}
}
