package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
	"github.com/quailyquaily/mistermorph/tools"
)

type execDirectSubtaskRunner struct {
	req SubtaskRequest
}

func (r *execDirectSubtaskRunner) RunSubtask(ctx context.Context, req SubtaskRequest) (*SubtaskResult, error) {
	r.req = req
	if req.RunFunc == nil {
		return nil, nil
	}
	return req.RunFunc(ctx)
}

func TestACPSpawnTool_Execute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := &execDirectSubtaskRunner{}
	var gotPrompt string

	tool := newACPSpawnTool(acpSpawnToolDeps{
		LookupAgent: func(name string) (acpclient.AgentConfig, bool) {
			if name != "codex" {
				return acpclient.AgentConfig{}, false
			}
			return acpclient.AgentConfig{
				Name:       "codex",
				Enable:     true,
				Type:       "stdio",
				Command:    "helper",
				CWD:        dir,
				ReadRoots:  []string{"."},
				WriteRoots: []string{"."},
			}, true
		},
		Runner: runner,
		RunPrompt: func(_ context.Context, cfg acpclient.PreparedAgentConfig, req acpclient.RunRequest) (acpclient.RunResult, error) {
			if cfg.CWD != dir {
				t.Fatalf("prepared cwd = %q, want %q", cfg.CWD, dir)
			}
			gotPrompt = req.Prompt
			return acpclient.RunResult{
				SessionID:  "sess_1",
				StopReason: "end_turn",
				Output:     `{"ok":true}`,
			}, nil
		},
	})

	raw, err := tool.Execute(context.Background(), map[string]any{
		"agent":           "codex",
		"task":            "inspect the repo",
		"output_schema":   "subtask.test.v1",
		"observe_profile": "web_extract",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.req.OutputSchema != "subtask.test.v1" {
		t.Fatalf("runner.req.OutputSchema = %q, want %q", runner.req.OutputSchema, "subtask.test.v1")
	}
	if runner.req.ObserveProfile != ObserveProfileWebExtract {
		t.Fatalf("runner.req.ObserveProfile = %q, want %q", runner.req.ObserveProfile, ObserveProfileWebExtract)
	}
	if gotPrompt == "" || gotPrompt == "inspect the repo" {
		t.Fatalf("gotPrompt = %q, want output schema requirement appended", gotPrompt)
	}

	var result SubtaskResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if result.Status != SubtaskStatusDone {
		t.Fatalf("result.Status = %q, want %q", result.Status, SubtaskStatusDone)
	}
	if result.OutputSchema != "subtask.test.v1" {
		t.Fatalf("result.OutputSchema = %q, want %q", result.OutputSchema, "subtask.test.v1")
	}
	if result.OutputKind != SubtaskOutputKindJSON {
		t.Fatalf("result.OutputKind = %q, want %q", result.OutputKind, SubtaskOutputKindJSON)
	}
}

func TestACPSpawnTool_PreservesWhitespaceOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := &execDirectSubtaskRunner{}

	tool := newACPSpawnTool(acpSpawnToolDeps{
		LookupAgent: func(name string) (acpclient.AgentConfig, bool) {
			if name != "codex" {
				return acpclient.AgentConfig{}, false
			}
			return acpclient.AgentConfig{
				Name:       "codex",
				Enable:     true,
				Type:       "stdio",
				Command:    "helper",
				CWD:        dir,
				ReadRoots:  []string{"."},
				WriteRoots: []string{"."},
			}, true
		},
		Runner: runner,
		RunPrompt: func(_ context.Context, cfg acpclient.PreparedAgentConfig, req acpclient.RunRequest) (acpclient.RunResult, error) {
			return acpclient.RunResult{
				SessionID:  "sess_1",
				StopReason: "end_turn",
				Output:     " hello \n",
			}, nil
		},
	})

	raw, err := tool.Execute(context.Background(), map[string]any{
		"agent": "codex",
		"task":  "echo exactly",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result SubtaskResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json.Unmarshal(result) error = %v", err)
	}
	if got, _ := result.Output.(string); got != " hello \n" {
		t.Fatalf("result.Output = %q, want %q", got, " hello \n")
	}
}

func TestACPSpawnTool_CanBeDisabled(t *testing.T) {
	t.Parallel()

	e := New(newMockClient(finalResponse("ok")), tools.NewRegistry(), baseCfg(), DefaultPromptSpec())
	if _, ok := e.registry.Get(acpSpawnToolName); ok {
		t.Fatal("acp_spawn should not be registered by default")
	}
}

func TestACPSpawnTool_CanBeEnabled(t *testing.T) {
	t.Parallel()

	e := New(
		newMockClient(finalResponse("ok")),
		tools.NewRegistry(),
		baseCfg(),
		DefaultPromptSpec(),
		WithACPSpawnToolEnabled(true),
		WithACPAgents([]acpclient.AgentConfig{{Name: "codex", Enable: true, Type: "stdio", Command: "helper"}}),
	)
	if _, ok := e.registry.Get(acpSpawnToolName); !ok {
		t.Fatal("acp_spawn should be registered when enabled")
	}
}
