package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type noopSubtaskClient struct{}

func (noopSubtaskClient) Chat(context.Context, llm.Request) (llm.Result, error) {
	return llm.Result{}, nil
}

type stubSubtaskRunner struct {
	req    SubtaskRequest
	result *SubtaskResult
}

func (s *stubSubtaskRunner) RunSubtask(_ context.Context, req SubtaskRequest) (*SubtaskResult, error) {
	s.req = req
	return s.result, nil
}

type stubSubtaskTool struct {
	name string
}

func (t stubSubtaskTool) Name() string            { return t.name }
func (t stubSubtaskTool) Description() string     { return "stub" }
func (t stubSubtaskTool) ParameterSchema() string { return `{"type":"object"}` }
func (t stubSubtaskTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

type subtaskContextProbeTool struct{}

func (subtaskContextProbeTool) Name() string            { return "probe" }
func (subtaskContextProbeTool) Description() string     { return "probe" }
func (subtaskContextProbeTool) ParameterSchema() string { return `{"type":"object"}` }
func (subtaskContextProbeTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	_, ok := SubtaskRunnerFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("subtask runner missing from tool context")
	}
	return "ok", nil
}

func TestSubtaskResultFromFinal_Text(t *testing.T) {
	result := SubtaskResultFromFinal("sub_123", "", &Final{Output: "hello\nworld"})
	if result.TaskID != "sub_123" {
		t.Fatalf("TaskID = %q, want sub_123", result.TaskID)
	}
	if result.Status != SubtaskStatusDone {
		t.Fatalf("Status = %q, want %q", result.Status, SubtaskStatusDone)
	}
	if result.OutputKind != SubtaskOutputKindText {
		t.Fatalf("OutputKind = %q, want %q", result.OutputKind, SubtaskOutputKindText)
	}
	out, ok := result.Output.(string)
	if !ok {
		t.Fatalf("Output type = %T, want string", result.Output)
	}
	if out != "hello\nworld" {
		t.Fatalf("Output = %q, want hello\\nworld", out)
	}
	if result.Summary != "hello" {
		t.Fatalf("Summary = %q, want hello", result.Summary)
	}
}

func TestSubtaskResultFromFinal_JSON(t *testing.T) {
	result := SubtaskResultFromFinal("sub_456", "subtask.test.v1", &Final{Output: map[string]any{"ok": true}})
	if result.OutputKind != SubtaskOutputKindJSON {
		t.Fatalf("OutputKind = %q, want %q", result.OutputKind, SubtaskOutputKindJSON)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
	if result.Summary != "subtask completed with structured output" {
		t.Fatalf("Summary = %q", result.Summary)
	}
	payload, ok := result.Output.(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("Output = %#v, want map with ok=true", result.Output)
	}
}

func TestSubtaskResultFromFinal_StringifiedJSONCompatibility(t *testing.T) {
	result := SubtaskResultFromFinal("sub_789", "subtask.test.v1", &Final{Output: `{"ok":true,"value":42}`})
	if result.Status != SubtaskStatusDone {
		t.Fatalf("Status = %q, want %q", result.Status, SubtaskStatusDone)
	}
	if result.OutputKind != SubtaskOutputKindJSON {
		t.Fatalf("OutputKind = %q, want %q", result.OutputKind, SubtaskOutputKindJSON)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
	payload, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output type = %T, want map", result.Output)
	}
	if payload["ok"] != true || payload["value"] != float64(42) {
		t.Fatalf("Output = %#v, want parsed JSON payload", result.Output)
	}
}

func TestSubtaskResultFromFinal_InvalidStructuredStringFails(t *testing.T) {
	result := SubtaskResultFromFinal("sub_bad", "subtask.test.v1", &Final{Output: "not json"})
	if result.Status != SubtaskStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, SubtaskStatusFailed)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
	if !strings.Contains(result.Error, "requires JSON output") {
		t.Fatalf("Error = %q, want JSON contract failure", result.Error)
	}
}

func TestSpawnToolUsesInjectedRunner(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(stubSubtaskTool{name: "url_fetch"})
	reg.Register(stubSubtaskTool{name: "bash"})

	runner := &stubSubtaskRunner{
		result: &SubtaskResult{
			TaskID:       "sub_test",
			Status:       SubtaskStatusDone,
			Summary:      "done",
			OutputKind:   SubtaskOutputKindJSON,
			OutputSchema: "subtask.test.v1",
			Output: map[string]any{
				"value": "ok",
			},
			Error: "",
		},
	}

	engine := New(
		noopSubtaskClient{},
		reg,
		Config{DefaultModel: "gpt-5.2"},
		DefaultPromptSpec(),
		WithSubtaskRunner(runner),
	)
	rawTool, ok := engine.registry.Get("spawn")
	if !ok {
		t.Fatal("spawn tool not registered")
	}
	tool, ok := rawTool.(*spawnTool)
	if !ok {
		t.Fatalf("spawn tool type = %T, want *spawnTool", rawTool)
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"task":          "fetch something",
		"tools":         []any{"url_fetch"},
		"model":         "gpt-5.4",
		"output_schema": "subtask.test.v1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if runner.req.Task != "fetch something" {
		t.Fatalf("runner task = %q, want fetch something", runner.req.Task)
	}
	if runner.req.Model != "gpt-5.4" {
		t.Fatalf("runner model = %q, want gpt-5.4", runner.req.Model)
	}
	if runner.req.OutputSchema != "subtask.test.v1" {
		t.Fatalf("runner output schema = %q, want subtask.test.v1", runner.req.OutputSchema)
	}
	if runner.req.Registry == nil {
		t.Fatal("runner registry is nil")
	}
	if names := runner.req.Registry.ToolNames(); names != "url_fetch" {
		t.Fatalf("runner registry tool names = %q, want url_fetch", names)
	}

	var result SubtaskResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output error = %v, raw=%q", err, out)
	}
	if result.TaskID != "sub_test" {
		t.Fatalf("result TaskID = %q, want sub_test", result.TaskID)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("result OutputSchema = %q, want subtask.test.v1", result.OutputSchema)
	}
}

func TestEngineInjectsDefaultSubtaskRunnerIntoToolContext(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(subtaskContextProbeTool{})

	client := newMockClient(
		toolCallResponse("probe"),
		finalResponse("done"),
	)
	e := New(client, reg, Config{MaxSteps: 3}, DefaultPromptSpec())

	final, _, err := e.Run(context.Background(), "test runner context", RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final == nil || final.Output != "done" {
		t.Fatalf("unexpected final = %#v", final)
	}
}
