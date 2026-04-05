package taskruntime

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/toolsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type stubAllowedSubtaskTool struct {
	name string
}

func (t stubAllowedSubtaskTool) Name() string            { return t.name }
func (t stubAllowedSubtaskTool) Description() string     { return "stub" }
func (t stubAllowedSubtaskTool) ParameterSchema() string { return `{"type":"object"}` }
func (t stubAllowedSubtaskTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestRunSubtaskReturnsEnvelope(t *testing.T) {
	client := &stubTaskRuntimeClient{
		result: llm.Result{Text: `{"type":"final","output":"{\"ok\":true}"}`},
	}
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
		RuntimeToolsConfig: toolsutil.RuntimeToolsRegisterConfig{
			PlanCreate: toolsutil.BuildPlanCreateRegisterConfig(true, 6),
			TodoUpdate: toolsutil.BuildTodoUpdateRegisterConfig(true, t.TempDir(), "contacts"),
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

	ctx := llmstats.WithRunID(context.Background(), "parent_run")
	reg := tools.NewRegistry()
	reg.Register(stubAllowedSubtaskTool{name: "bash"})
	result, err := rt.RunSubtask(ctx, agent.SubtaskRequest{
		Task:         "ping",
		Model:        "gpt-5.4",
		OutputSchema: "subtask.test.v1",
		Registry:     reg,
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil {
		t.Fatal("RunSubtask() result is nil")
	}
	if result.Status != agent.SubtaskStatusDone {
		t.Fatalf("Status = %q, want %q", result.Status, agent.SubtaskStatusDone)
	}
	if result.OutputKind != agent.SubtaskOutputKindJSON {
		t.Fatalf("OutputKind = %q, want %q", result.OutputKind, agent.SubtaskOutputKindJSON)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
	payload, ok := result.Output.(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("Output = %#v, want parsed JSON payload", result.Output)
	}
	if !strings.HasPrefix(result.TaskID, "sub_") {
		t.Fatalf("TaskID = %q, want prefix sub_", result.TaskID)
	}
	if len(client.requests) != 1 {
		t.Fatalf("client requests = %d, want 1", len(client.requests))
	}
	if got := client.requests[0].Scene; got != "spawn.subtask" {
		t.Fatalf("request scene = %q, want spawn.subtask", got)
	}
	if got := client.requests[0].Model; got != "gpt-5.4" {
		t.Fatalf("request model = %q, want gpt-5.4", got)
	}
	if len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "bash" {
		t.Fatalf("request tools = %#v, want only bash", client.requests[0].Tools)
	}
	foundParentRunID := false
	for _, msg := range client.requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "subtask_parent_run_id") && strings.Contains(msg.Content, "parent_run") {
			foundParentRunID = true
			break
		}
	}
	if !foundParentRunID {
		t.Fatalf("expected injected meta to include parent run id, messages=%#v", client.requests[0].Messages)
	}
	foundOutputSchemaInstruction := false
	for _, msg := range client.requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "output_schema=subtask.test.v1") {
			foundOutputSchemaInstruction = true
			break
		}
	}
	if !foundOutputSchemaInstruction {
		t.Fatalf("expected task prompt to include output schema instruction, messages=%#v", client.requests[0].Messages)
	}
}

func TestRunSubtaskDirectPathSkipsLLMAndNormalizesResult(t *testing.T) {
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
		AgentConfig: agent.Config{MaxSteps: 2, ParseRetries: 0, ToolRepeatLimit: 2},
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	result, err := rt.RunSubtask(context.Background(), agent.SubtaskRequest{
		OutputSchema: "subtask.direct.v1",
		RunFunc: func(context.Context) (*agent.SubtaskResult, error) {
			return &agent.SubtaskResult{
				Status:     agent.SubtaskStatusDone,
				OutputKind: agent.SubtaskOutputKindJSON,
				Output: map[string]any{
					"ok": true,
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunSubtask() error = %v", err)
	}
	if result == nil {
		t.Fatal("RunSubtask() result is nil")
	}
	if result.Status != agent.SubtaskStatusDone {
		t.Fatalf("Status = %q, want done", result.Status)
	}
	if result.OutputSchema != "subtask.direct.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.direct.v1", result.OutputSchema)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected direct subtask to skip llm client, got %d requests", len(client.requests))
	}
}
