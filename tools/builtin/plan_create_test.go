package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

type stubPlanCreateLLMClient struct {
	reply   string
	err     error
	lastReq llm.Request
}

func (s *stubPlanCreateLLMClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	s.lastReq = req
	if s.err != nil {
		return llm.Result{}, s.err
	}
	return llm.Result{Text: s.reply}, nil
}

func TestPlanCreateExecuteRejectsEmptyStepsAfterNormalization(t *testing.T) {
	client := &stubPlanCreateLLMClient{
		reply: `{"plan":{"steps":[{"step":"   "},{"step":""}]}}`,
	}
	tool := NewPlanCreateTool(client, "gpt-5.2", []string{"bash"}, 6)
	_, err := tool.Execute(context.Background(), map[string]any{"task": "t"})
	if err == nil {
		t.Fatal("expected error for empty normalized steps")
	}
	if !strings.Contains(err.Error(), "empty steps") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanCreateExecuteNormalizesAndDropsEmptySteps(t *testing.T) {
	client := &stubPlanCreateLLMClient{
		reply: `{"plan":{"steps":[{"step":"   ","status":"pending"},{"step":" collect data ","status":""},{"step":"summarize","status":"unknown"}]}}`,
	}
	tool := NewPlanCreateTool(client, "gpt-5.2", []string{"bash"}, 6)
	out, err := tool.Execute(context.Background(), map[string]any{"task": "t"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var parsed struct {
		Plan struct {
			Steps []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"steps"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(parsed.Plan.Steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(parsed.Plan.Steps))
	}
	if parsed.Plan.Steps[0].Step != "collect data" || parsed.Plan.Steps[0].Status != "in_progress" {
		t.Fatalf("steps[0] = %+v, want step=%q status=%q", parsed.Plan.Steps[0], "collect data", "in_progress")
	}
	if parsed.Plan.Steps[1].Step != "summarize" || parsed.Plan.Steps[1].Status != "pending" {
		t.Fatalf("steps[1] = %+v, want step=%q status=%q", parsed.Plan.Steps[1], "summarize", "pending")
	}
}

func TestPlanCreateExecuteOmitsStyleFromSchemaAndPayload(t *testing.T) {
	client := &stubPlanCreateLLMClient{
		reply: `{"plan":{"steps":[{"step":"collect data"}]}}`,
	}
	tool := NewPlanCreateTool(client, "gpt-5.2", []string{"bash"}, 6)

	schema := tool.ParameterSchema()
	if strings.Contains(schema, `"style"`) {
		t.Fatalf("schema should not contain style field: %s", schema)
	}

	if _, err := tool.Execute(context.Background(), map[string]any{
		"task":  "t",
		"style": "terse",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(client.lastReq.Messages) < 2 {
		t.Fatalf("expected user payload message")
	}
	if strings.Contains(client.lastReq.Messages[1].Content, `"style"`) {
		t.Fatalf("payload should not contain style field: %s", client.lastReq.Messages[1].Content)
	}
}

func TestPlanCreateExecuteUsesCapturedPersonaDirectory(t *testing.T) {
	personaDir := t.TempDir()
	identity := "name: Captured Persona\ncreature: test\nvibe: precise\nemoji: test\n"
	if err := os.WriteFile(filepath.Join(personaDir, "identity.yaml"), []byte(identity), 0o600); err != nil {
		t.Fatalf("WriteFile(identity) error = %v", err)
	}
	client := &stubPlanCreateLLMClient{
		reply: `{"plan":{"steps":[{"step":"collect data"}]}}`,
	}
	tool := NewPlanCreateToolWithPersona(client, "gpt-5.2", []string{"bash"}, 6, personaDir)
	if _, err := tool.Execute(context.Background(), map[string]any{"task": "t"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(client.lastReq.Messages) == 0 {
		t.Fatal("system prompt is missing")
	}
	if got := client.lastReq.Messages[0].Content; !strings.Contains(got, "Captured Persona") {
		t.Fatalf("system prompt does not use captured persona: %q", got)
	}
}
