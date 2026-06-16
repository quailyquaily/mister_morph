package consolecmd

import (
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
)

func TestUpdateConsoleActivityProgressMergesToolLifecycle(t *testing.T) {
	var progress *consoleActivityProgress

	progress, changed := updateConsoleActivityProgress(progress, agent.Event{
		Kind:       agent.EventKindToolStart,
		ActivityID: "tool:1",
		ToolName:   "web_search",
		Status:     "running",
		Args: map[string]any{
			"q": "mistermorph",
		},
	})
	if !changed || progress == nil {
		t.Fatalf("first update changed=%v progress=%#v, want non-nil progress", changed, progress)
	}

	progress, changed = updateConsoleActivityProgress(progress, agent.Event{
		Kind:       agent.EventKindToolDone,
		ActivityID: "tool:1",
		ToolName:   "web_search",
		Status:     "done",
	})
	if !changed {
		t.Fatal("tool done should update activity progress")
	}
	if progress.Current == nil {
		t.Fatal("progress.Current = nil")
	}
	if progress.Current.Status != "done" {
		t.Fatalf("progress.Current.Status = %q, want done", progress.Current.Status)
	}
	if progress.Current.Args["q"] != "mistermorph" {
		t.Fatalf("progress.Current.Args[q] = %#v, want mistermorph", progress.Current.Args["q"])
	}
	if len(progress.History) != 1 {
		t.Fatalf("len(progress.History) = %d, want 1", len(progress.History))
	}
	if _, err := time.Parse(time.RFC3339Nano, progress.Current.At); err != nil {
		t.Fatalf("progress.Current.At = %q, want RFC3339Nano timestamp: %v", progress.Current.At, err)
	}
}

func TestUpdateConsoleActivityProgressTracksSubtaskHistory(t *testing.T) {
	var progress *consoleActivityProgress

	progress, _ = updateConsoleActivityProgress(progress, agent.Event{
		Kind:       agent.EventKindSubtaskStart,
		ActivityID: "sub_1",
		TaskID:     "sub_1",
		Mode:       "agent",
		Profile:    string(agent.ObserveProfileWebExtract),
		Status:     "running",
	})
	progress, _ = updateConsoleActivityProgress(progress, agent.Event{
		Kind:       agent.EventKindSubtaskDone,
		ActivityID: "sub_1",
		TaskID:     "sub_1",
		Status:     agent.SubtaskStatusDone,
		Summary:    "collected results",
		OutputKind: agent.SubtaskOutputKindJSON,
	})
	if progress == nil || progress.Current == nil {
		t.Fatalf("progress = %#v, want current entry", progress)
	}
	if progress.Current.Kind != "subtask" {
		t.Fatalf("progress.Current.Kind = %q, want subtask", progress.Current.Kind)
	}
	if progress.Current.Name != "sub_1" {
		t.Fatalf("progress.Current.Name = %q, want sub_1", progress.Current.Name)
	}
	if progress.Current.Summary != "collected results" {
		t.Fatalf("progress.Current.Summary = %q, want collected results", progress.Current.Summary)
	}
	if progress.Current.OutputKind != agent.SubtaskOutputKindJSON {
		t.Fatalf("progress.Current.OutputKind = %q, want %q", progress.Current.OutputKind, agent.SubtaskOutputKindJSON)
	}
}

func TestUpdateConsoleActivityProgressAppendsCoderOutput(t *testing.T) {
	var progress *consoleActivityProgress

	progress, _ = updateConsoleActivityProgress(progress, agent.Event{
		Kind:       agent.EventKindToolStart,
		ActivityID: "tool:coder",
		ToolName:   "coder",
		Status:     "running",
	})
	progress, changed := updateConsoleActivityProgress(progress, agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "coder",
		Stream:   "codex",
		Text:     "alpha",
		Status:   "running",
	})
	if !changed {
		t.Fatal("coder output should update activity progress")
	}
	progress, _ = updateConsoleActivityProgress(progress, agent.Event{
		Kind:     agent.EventKindToolOutput,
		ToolName: "coder",
		Stream:   "codex",
		Text:     "\nbeta",
		Status:   "running",
	})
	if progress == nil || progress.Current == nil {
		t.Fatalf("progress = %#v, want current entry", progress)
	}
	if progress.Current.ID != "tool:coder" {
		t.Fatalf("progress.Current.ID = %q, want tool:coder", progress.Current.ID)
	}
	if progress.Current.Stream != "codex" {
		t.Fatalf("progress.Current.Stream = %q, want codex", progress.Current.Stream)
	}
	if progress.Current.Output != "alpha\nbeta" {
		t.Fatalf("progress.Current.Output = %q, want alpha newline beta", progress.Current.Output)
	}
	if progress.Current.Status != "running" {
		t.Fatalf("progress.Current.Status = %q, want running", progress.Current.Status)
	}
}
