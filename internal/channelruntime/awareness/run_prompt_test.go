package awareness

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathroots"
	"github.com/quailyquaily/mistermorph/internal/shellenv"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
	"github.com/quailyquaily/mistermorph/tools/builtin"
)

type awarenessPromptCaptureClient struct {
	requests []llm.Request
}

func (c *awarenessPromptCaptureClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
}

type awarenessContextLengthClient struct {
	requests []llm.Request
}

func (c *awarenessContextLengthClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{}, llm.MarkContextLengthError(errors.New("context too long"))
}

type awarenessPromptMockTool struct {
	name string
}

func (t awarenessPromptMockTool) Name() string            { return t.name }
func (t awarenessPromptMockTool) Description() string     { return "mock tool" }
func (t awarenessPromptMockTool) ParameterSchema() string { return "{}" }
func (t awarenessPromptMockTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestRunAwarenessTaskUsesFinalOnlyResponsePrompt(t *testing.T) {
	client := &awarenessPromptCaptureClient{}
	baseReg := tools.NewRegistry()
	baseReg.Register(awarenessPromptMockTool{name: "plan_create"})

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorHeartbeat,
		Client:       client,
		Model:        "test-model",
		Task:         "heartbeat task",
		BaseRegistry: baseReg,
		Config:       agent.Config{MaxSteps: 1},
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	systemPrompt := client.requests[0].Messages[0].Content
	if strings.Contains(systemPrompt, "Option 1: Plan") {
		t.Fatalf("system prompt should not include plan response option: %s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "## Response Rules") {
		t.Fatalf("system prompt should not include response rules: %s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "[[ Awareness Rules ]]") {
		t.Fatalf("system prompt missing awareness rules: %s", systemPrompt)
	}
}

func TestRunAwarenessTaskDisablesContextCompaction(t *testing.T) {
	client := &awarenessContextLengthClient{}
	var events []agent.Event
	ctx := agent.WithEventSinkContext(context.Background(), agent.EventSinkFunc(func(_ context.Context, event agent.Event) {
		events = append(events, event)
	}))

	_, err := runAwarenessTask(ctx, depsutil.CommonDependencies{
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorHeartbeat,
		Client:       client,
		Model:        "test-model",
		Task:         "heartbeat task",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
	})
	if !llm.IsContextLengthError(err) {
		t.Fatalf("runAwarenessTask() error = %v, want context-length error", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	for _, event := range events {
		if event.Kind == agent.EventKindContextCompactionStart {
			t.Fatalf("awareness emitted context compaction event: %#v", event)
		}
	}
}

func TestRunAwarenessTaskAddsCoderFromExplicitTrigger(t *testing.T) {
	client := &awarenessPromptCaptureClient{}

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		ToolTriggers: func(string) map[string]bool {
			return map[string]bool{toolsutil.BuiltinCoder: true}
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorHeartbeat,
		Client:       client,
		Model:        "test-model",
		Task:         "$coder inspect",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	if !requestHasTool(client.requests[0], toolsutil.BuiltinCoder) {
		t.Fatalf("request tools missing coder: %#v", client.requests[0].Tools)
	}
}

func TestRunAwarenessTaskRegistersBashFromBashEnvWithoutExplicitTrigger(t *testing.T) {
	client := &awarenessPromptCaptureClient{}
	var triggeredTools map[string]bool

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		ToolTriggers: func(string) map[string]bool {
			return nil
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			triggeredTools = triggers
			if triggers[toolsutil.BuiltinBash] {
				reg.Register(builtin.NewBashTool(true, 0, 0, pathroots.PathRoots{}))
			}
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       client,
		Model:        "test-model",
		Task:         "generate weekly report",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
		BashEnv: []cron.BashEnvRef{
			{Name: "REPORT_MODE", Value: "weekly"},
		},
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}
	if triggeredTools == nil || !triggeredTools[toolsutil.BuiltinBash] {
		t.Fatalf("triggered tools = %#v, want bash trigger", triggeredTools)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	if !requestHasTool(client.requests[0], toolsutil.BuiltinBash) {
		t.Fatalf("request tools missing bash: %#v", client.requests[0].Tools)
	}
}

func TestRunAwarenessTaskPatchesBashEnvAfterTriggeredBashRegistration(t *testing.T) {
	client := &awarenessPromptCaptureClient{}
	var runRegistry *tools.Registry

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		ToolTriggers: func(string) map[string]bool {
			return map[string]bool{toolsutil.BuiltinBash: true}
		},
		RegisterTriggeredStaticTools: func(reg *tools.Registry, triggers map[string]bool) {
			if triggers[toolsutil.BuiltinBash] {
				reg.Register(builtin.NewBashTool(true, 0, 0, pathroots.PathRoots{}))
			}
			runRegistry = reg
		},
		PromptSpec: func(context.Context, *slog.Logger, agent.LogOptions, string, llm.Client, string, []string) (agent.PromptSpec, []string, error) {
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       client,
		Model:        "test-model",
		Task:         "$bash echo cron",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
		BashEnv: []cron.BashEnvRef{
			{Name: "API_KEY", Value: "task-key"},
		},
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.requests))
	}
	if !requestHasTool(client.requests[0], toolsutil.BuiltinBash) {
		t.Fatalf("request tools missing bash: %#v", client.requests[0].Tools)
	}
	if runRegistry == nil {
		t.Fatal("run registry was not captured")
	}
	bashTool, ok := runRegistry.Get(toolsutil.BuiltinBash)
	if !ok {
		t.Fatal("run registry missing bash tool")
	}
	patched, ok := bashTool.(*builtin.BashTool)
	if !ok {
		t.Fatalf("bash tool type = %T, want *builtin.BashTool", bashTool)
	}
	want := []shellenv.InjectedEnvVar{{Name: "API_KEY", Value: "task-key"}}
	if len(patched.InjectedEnvVars) != len(want) {
		t.Fatalf("injected env = %#v, want %#v", patched.InjectedEnvVars, want)
	}
	for i, got := range patched.InjectedEnvVars {
		if got.Name != want[i].Name || got.Value != want[i].Value {
			t.Fatalf("injected env[%d] = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestRunAwarenessTaskParsesThinkCommandBeforePromptAndToolTriggers(t *testing.T) {
	baseClient := &awarenessPromptCaptureClient{}
	thinkClient := &awarenessPromptCaptureClient{}
	thinkRoute := llmutil.ResolvedRoute{
		Purpose: llmutil.RoutePurposeThink,
		Profile: "reasoning",
		Values: llmutil.RuntimeValues{
			ReasoningEffortRaw: "low",
		},
		ClientConfig: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "think-model",
		},
	}
	var created []llmutil.ResolvedRoute
	var promptTask string
	var triggerTask string

	_, err := runAwarenessTask(context.Background(), depsutil.CommonDependencies{
		ResolveLLMRoute: func(purpose string) (llmutil.ResolvedRoute, error) {
			if purpose != llmutil.RoutePurposeThink {
				t.Fatalf("route purpose = %q, want think", purpose)
			}
			return thinkRoute, nil
		},
		CreateLLMClient: func(route llmutil.ResolvedRoute) (llm.Client, error) {
			created = append(created, route)
			return thinkClient, nil
		},
		ToolTriggers: func(task string) map[string]bool {
			triggerTask = task
			return map[string]bool{toolsutil.BuiltinCoder: true}
		},
		PromptSpec: func(_ context.Context, _ *slog.Logger, _ agent.LogOptions, task string, client llm.Client, model string, _ []string) (agent.PromptSpec, []string, error) {
			promptTask = task
			if client != thinkClient {
				t.Fatalf("prompt client = %T, want think client", client)
			}
			if model != "think-model" {
				t.Fatalf("prompt model = %q, want think-model", model)
			}
			return agent.PromptSpec{Identity: "identity"}, nil, nil
		},
	}, awarenessTaskOptions{
		Behavior:     awarenessutil.BehaviorCron,
		Client:       baseClient,
		Model:        "awareness-model",
		Task:         "/think use $coder to inspect",
		BaseRegistry: tools.NewRegistry(),
		Config:       agent.Config{MaxSteps: 1},
	})
	if err != nil {
		t.Fatalf("runAwarenessTask() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created routes = %d, want 1", len(created))
	}
	if created[0].Values.ReasoningEffortRaw != llmutil.ReasoningEffortXHigh {
		t.Fatalf("reasoning effort = %q, want xhigh", created[0].Values.ReasoningEffortRaw)
	}
	if promptTask != "use $coder to inspect" {
		t.Fatalf("prompt task = %q, want stripped task", promptTask)
	}
	if triggerTask != "use $coder to inspect" {
		t.Fatalf("trigger task = %q, want stripped task", triggerTask)
	}
	if len(baseClient.requests) != 0 {
		t.Fatalf("base client request count = %d, want 0", len(baseClient.requests))
	}
	if len(thinkClient.requests) != 1 {
		t.Fatalf("think client request count = %d, want 1", len(thinkClient.requests))
	}
	if thinkClient.requests[0].Model != "think-model" {
		t.Fatalf("request model = %q, want think-model", thinkClient.requests[0].Model)
	}
	msgs := thinkClient.requests[0].Messages
	if got := msgs[len(msgs)-1].Content; got != "use $coder to inspect" {
		t.Fatalf("task message = %q, want stripped task", got)
	}
	if !requestHasTool(thinkClient.requests[0], toolsutil.BuiltinCoder) {
		t.Fatalf("request tools missing coder: %#v", thinkClient.requests[0].Tools)
	}
}

func requestHasTool(req llm.Request, name string) bool {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
