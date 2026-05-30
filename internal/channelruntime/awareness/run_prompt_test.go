package awareness

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/internal/channelruntime/depsutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type awarenessPromptCaptureClient struct {
	requests []llm.Request
}

func (c *awarenessPromptCaptureClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	c.requests = append(c.requests, req)
	return llm.Result{Text: `{"type":"final","output":"ok"}`}, nil
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
