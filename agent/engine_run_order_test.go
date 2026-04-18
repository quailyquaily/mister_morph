package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestRun_MetaPrecedesHistoryAndCurrentMessageIsLast(t *testing.T) {
	t.Parallel()

	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	current := &llm.Message{Role: "user", Content: "CURRENT_TURN"}
	_, _, err := e.Run(context.Background(), "RAW_TASK_SHOULD_NOT_APPEAR", RunOptions{
		History: []llm.Message{{Role: "user", Content: "HISTORY_CONTEXT"}},
		Meta: map[string]any{
			"trigger": "telegram",
		},
		CurrentMessage: current,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("messages[0].role = %q, want system", msgs[0].Role)
	}
	if !strings.Contains(msgs[1].Content, "mister_morph_meta") {
		t.Fatalf("messages[1] = %q, want injected meta", msgs[1].Content)
	}
	meta := decodeInjectedMeta(t, msgs[1].Content)
	if got := strings.TrimSpace(asString(meta["trigger"])); got != "telegram" {
		t.Fatalf("meta trigger = %q, want telegram", got)
	}
	if got := strings.TrimSpace(asString(meta["host_os"])); got == "" {
		t.Fatalf("meta host_os should be set: %#v", meta)
	}
	if msgs[2].Content != "HISTORY_CONTEXT" {
		t.Fatalf("messages[2] = %q, want history", msgs[2].Content)
	}
	if msgs[3].Content != "CURRENT_TURN" {
		t.Fatalf("messages[3] = %q, want current turn", msgs[3].Content)
	}
	if requestContains(calls, 0, "RAW_TASK_SHOULD_NOT_APPEAR") {
		t.Fatalf("raw task should not be appended when CurrentMessage is set")
	}
}

func TestRun_AppendsRawTaskWhenCurrentMessageIsNil(t *testing.T) {
	t.Parallel()

	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "RAW_TASK", RunOptions{
		History: []llm.Message{{Role: "user", Content: "HISTORY_CONTEXT"}},
		Meta: map[string]any{
			"trigger": "telegram",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d, want 4", len(msgs))
	}
	if msgs[3].Content != "RAW_TASK" {
		t.Fatalf("messages[3] = %q, want raw task", msgs[3].Content)
	}
}

func TestRun_MemoryContextIsInjectedBeforeHistory(t *testing.T) {
	t.Parallel()

	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "RAW_TASK", RunOptions{
		MemoryContext: "memory snapshot",
		History:       []llm.Message{{Role: "user", Content: "HISTORY_CONTEXT"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("chat calls = %d, want 1", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) != 5 {
		t.Fatalf("messages len = %d, want 5", len(msgs))
	}
	if !strings.Contains(msgs[1].Content, "mister_morph_meta") {
		t.Fatalf("messages[1] = %q, want injected meta", msgs[1].Content)
	}
	meta := decodeInjectedMeta(t, msgs[1].Content)
	if got := strings.TrimSpace(asString(meta["host_os"])); got == "" {
		t.Fatalf("meta host_os should be set: %#v", meta)
	}
	if msgs[2].Role != "user" || !strings.Contains(msgs[2].Content, "[[ Runtime Memory ]]") {
		t.Fatalf("messages[2] = %#v, want runtime memory message", msgs[2])
	}
	if !strings.Contains(msgs[2].Content, "memory snapshot") {
		t.Fatalf("messages[2] = %q, want memory snapshot", msgs[2].Content)
	}
	if msgs[3].Content != "HISTORY_CONTEXT" {
		t.Fatalf("messages[3] = %q, want history", msgs[3].Content)
	}
	if msgs[4].Content != "RAW_TASK" {
		t.Fatalf("messages[4] = %q, want raw task", msgs[4].Content)
	}
}

func decodeInjectedMeta(t *testing.T, raw string) map[string]any {
	t.Helper()

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(meta) error = %v; raw=%q", err, raw)
	}
	meta := envelope["mister_morph_meta"]
	if meta == nil {
		t.Fatalf("decoded meta missing mister_morph_meta: %#v", envelope)
	}
	return meta
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
