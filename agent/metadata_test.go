package agent

import (
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

func TestRun_InsertsMetaMessageBeforeTask(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(t.Context(), "do the thing", RunOptions{
		Meta: map[string]any{
			"trigger": "daemon",
			"foo":     "bar",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := client.allCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 llm call, got %d", len(calls))
	}
	msgs := calls[0].Messages
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}
	meta := msgs[len(msgs)-2]
	task := msgs[len(msgs)-1]
	if meta.Role != "user" {
		t.Fatalf("expected meta role user, got %q", meta.Role)
	}
	if !strings.Contains(meta.Content, "\"mister_morph_meta\"") {
		t.Fatalf("expected meta message to contain mister_morph_meta, got: %s", meta.Content)
	}
	if task.Content != "do the thing" {
		t.Fatalf("expected task message last, got: %q", task.Content)
	}
}

func TestRun_TruncatesMetaTo4KB(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, tools.NewRegistry(), baseCfg(), DefaultPromptSpec())

	huge := strings.Repeat("x", 10*1024)
	_, _, err := e.Run(t.Context(), "do the thing", RunOptions{
		Meta: map[string]any{
			"trigger": "daemon",
			"huge":    huge,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := client.allCalls()
	msgs := calls[0].Messages
	meta := msgs[len(msgs)-2]
	if len(meta.Content) > maxInjectedMetaBytes {
		t.Fatalf("expected meta <= %d bytes, got %d", maxInjectedMetaBytes, len(meta.Content))
	}
	if !strings.Contains(meta.Content, "\"truncated\"") {
		t.Fatalf("expected truncated marker, got: %s", meta.Content)
	}
}

func TestDefaultPromptSpec_IncludesMetaRule(t *testing.T) {
	spec := DefaultPromptSpec()
	joined := strings.Join(spec.Rules, "\n")
	if !strings.Contains(joined, "mister_morph_meta") {
		t.Fatalf("expected DefaultPromptSpec rules to mention mister_morph_meta")
	}
}
