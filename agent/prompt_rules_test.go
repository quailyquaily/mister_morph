package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/quailyquaily/mistermorph/tools"
)

type schemaMarkerTool struct{}

func (t schemaMarkerTool) Name() string        { return "schema_marker" }
func (t schemaMarkerTool) Description() string { return "marker tool description" }
func (t schemaMarkerTool) ParameterSchema() string {
	return "SCHEMA_MARKER"
}
func (t schemaMarkerTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "ok", nil
}

func TestBuildSystemPrompt_UsesToolSummaries(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(schemaMarkerTool{})

	prompt := BuildSystemPrompt(reg, DefaultPromptSpec())
	if !strings.Contains(prompt, "marker tool description") {
		t.Fatalf("expected tool description to be present in prompt")
	}
	if strings.Contains(prompt, "SCHEMA_MARKER") {
		t.Fatalf("expected tool schema to be omitted from prompt")
	}
}

func TestPromptRules_NoURL_NoInjection(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "summarize this text", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := client.allCalls()[0].Messages[0].Content
	if strings.Contains(prompt, rulePreferURLFetch) {
		t.Fatalf("unexpected URL rule in prompt")
	}
}

func TestPromptRules_SingleURL_Injection(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "visit https://example.com then summarize", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := client.allCalls()[0].Messages[0].Content
	if !strings.Contains(prompt, rulePreferURLFetch) {
		t.Fatalf("expected prefer-url_fetch rule in prompt")
	}
	if !strings.Contains(prompt, ruleURLFetchFail) {
		t.Fatalf("expected url_fetch failure rule in prompt")
	}
	if strings.Contains(prompt, ruleBatchURLFetch) {
		t.Fatalf("did not expect batch rule for single URL")
	}
}

func TestPromptRules_MultiURL_BatchRule(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "visit https://a.com and https://b.com", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := client.allCalls()[0].Messages[0].Content
	if !strings.Contains(prompt, ruleBatchURLFetch) {
		t.Fatalf("expected batch url_fetch rule for multi-URL task")
	}
}

func TestPromptRules_BinaryURL_DownloadPathRule(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "visit https://example.com/report.pdf", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := client.allCalls()[0].Messages[0].Content
	if !strings.Contains(prompt, rulePreferDownload) {
		t.Fatalf("expected download_path rule for binary URL")
	}
	if strings.Contains(prompt, ruleRangeProbe) {
		t.Fatalf("did not expect range probe rule for binary-only URL")
	}
}

func TestPromptRules_NonBinaryURL_RangeRule(t *testing.T) {
	client := newMockClient(finalResponse("ok"))
	e := New(client, baseRegistry(), baseCfg(), DefaultPromptSpec())

	_, _, err := e.Run(context.Background(), "visit https://example.com/page", RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := client.allCalls()[0].Messages[0].Content
	if !strings.Contains(prompt, ruleRangeProbe) {
		t.Fatalf("expected range probe rule for non-binary URL")
	}
}
