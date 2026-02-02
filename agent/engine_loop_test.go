package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/quailyquaily/mister_morph/llm"
)

// --- log-capturing handler ---

type logRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type capturingHandler struct {
	mu      sync.Mutex
	records []logRecord
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{Level: r.Level, Message: r.Message, Attrs: make(map[string]any)}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}
func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(name string) slog.Handler       { return h }

func (h *capturingHandler) allRecords() []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]logRecord, len(h.records))
	copy(out, h.records)
	return out
}

func (h *capturingHandler) countByMessage(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Message == msg {
			n++
		}
	}
	return n
}

// --- helpers for these tests ---

func finalResponseWithThought(thought, output string) llm.Result {
	return llm.Result{
		Text: fmt.Sprintf(`{"type":"final","final":{"thought":%q,"output":%q}}`, thought, output),
	}
}

func toolCallResponseWithThought(thought, toolName string) llm.Result {
	return llm.Result{
		Text: fmt.Sprintf(`{"type":"tool_call","tool_call":{"thought":%q,"tool_name":%q,"tool_params":{}}}`, thought, toolName),
	}
}

// ============================================================
// BUG-4: Duplicate logging tests
// ============================================================

func TestFinalThought_NoDuplicateLog(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)

	client := newMockClient(finalResponseWithThought("my final thought", "done"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec(),
		WithLogger(logger),
		WithLogOptions(LogOptions{IncludeThoughts: true}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "final" with thought content should appear exactly once at Info level.
	// "final_thought" at Debug level should NOT appear when IncludeThoughts
	// is true (it's redundant).
	finalCount := handler.countByMessage("final")
	if finalCount != 1 {
		t.Errorf("expected exactly 1 'final' log entry, got %d", finalCount)
	}
	finalThoughtCount := handler.countByMessage("final_thought")
	if finalThoughtCount != 0 {
		t.Errorf("expected 0 'final_thought' log entries (duplicate), got %d", finalThoughtCount)
	}
}

func TestToolThought_NoDuplicateLog(t *testing.T) {
	handler := &capturingHandler{}
	logger := slog.New(handler)

	reg := baseRegistry()
	reg.Register(&mockTool{name: "search", result: "found it"})

	client := newMockClient(
		toolCallResponseWithThought("my tool thought", "search"),
		finalResponseWithThought("done thinking", "done"),
	)
	e := New(client, reg, baseCfg(), DefaultPromptSpec(),
		WithLogger(logger),
		WithLogOptions(LogOptions{IncludeThoughts: true}),
	)

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "tool_thought" should appear exactly once (Info), not twice (Info+Debug).
	toolThoughtCount := handler.countByMessage("tool_thought")
	if toolThoughtCount != 1 {
		t.Errorf("expected exactly 1 'tool_thought' log entry, got %d", toolThoughtCount)
	}
}

// ============================================================
// RES-5: Observation truncation tests
// ============================================================

func TestLongObservation_TruncatedInMessages(t *testing.T) {
	reg := baseRegistry()
	longOutput := strings.Repeat("x", 300_000) // 300 KB
	reg.Register(&mockTool{name: "search", result: longOutput})

	client := newMockClient(
		toolCallResponse("search"),
		finalResponse("done"),
	)
	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(calls))
	}

	// The second call should contain the tool result message, which must be truncated.
	secondCall := calls[1]
	for _, msg := range secondCall.Messages {
		if strings.HasPrefix(msg.Content, "Tool Result (search):") {
			if len(msg.Content) > 200_000 {
				t.Errorf("observation in message history should be truncated, got length %d", len(msg.Content))
			}
			return
		}
	}
	t.Fatal("expected to find a 'Tool Result (search):' message in second LLM call")
}

func TestLongObservation_UTF8SafeTruncation(t *testing.T) {
	reg := baseRegistry()

	// Build a ~300 KB string using 4-byte emoji (🎉) so that the 128 KB
	// boundary is very likely to fall inside a multi-byte character.
	emoji := "🎉"                                           // 4 bytes
	repeatCount := (300*1024)/len(emoji) + 1                // enough to exceed 300 KB
	longOutput := strings.Repeat(emoji, repeatCount)        // all multi-byte
	reg.Register(&mockTool{name: "search", result: longOutput})

	client := newMockClient(
		toolCallResponse("search"),
		finalResponse("done"),
	)
	e := New(client, reg, baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "test", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := client.allCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(calls))
	}

	// Find the truncated tool result in the second LLM call.
	secondCall := calls[1]
	for _, msg := range secondCall.Messages {
		if strings.HasPrefix(msg.Content, "Tool Result (search):") {
			if !utf8.ValidString(msg.Content) {
				t.Fatal("truncated observation is not valid UTF-8")
			}
			// Should be truncated (< 200 KB).
			if len(msg.Content) > 200_000 {
				t.Errorf("observation should be truncated, got length %d", len(msg.Content))
			}
			return
		}
	}
	t.Fatal("expected to find a 'Tool Result (search):' message in second LLM call")
}
