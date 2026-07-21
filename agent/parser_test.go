package agent

import (
	"encoding/json"
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
)

func TestAgentResponseHasRawFinalAnswerField(t *testing.T) {
	var resp AgentResponse
	if resp.RawFinalAnswer != nil {
		t.Error("expected RawFinalAnswer to default to nil")
	}
}

func TestParseFinalAnswerPopulatesRawFinalAnswer(t *testing.T) {
	input := `{
		"type": "final_answer",
		"reasoning": "done",
		"output": "hello",
		"sources": ["a", "b"]
	}`
	result := llm.Result{Text: input}
	resp, err := ParseResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be populated")
	}

	// RawFinalAnswer should contain the raw JSON payload without the top-level type.
	var m map[string]any
	if err := json.Unmarshal(resp.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer is not valid JSON: %v", err)
	}
	if m["reasoning"] != "done" {
		t.Errorf("expected reasoning='done', got %v", m["reasoning"])
	}
	// Domain-specific field should be preserved
	sources, ok := m["sources"]
	if !ok {
		t.Fatal("expected 'sources' field in RawFinalAnswer")
	}
	arr, ok := sources.([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("expected sources to be array of length 2, got %v", sources)
	}
}

func TestParseFinalPopulatesRawFinalAnswer(t *testing.T) {
	input := `{
		"type": "final",
		"reasoning": "done",
		"output": "result",
		"truth_assessment": 0.95
	}`
	result := llm.Result{Text: input}
	resp, err := ParseResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be populated for 'final' type")
	}

	var m map[string]any
	if err := json.Unmarshal(resp.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer is not valid JSON: %v", err)
	}
	if m["truth_assessment"] != 0.95 {
		t.Errorf("expected truth_assessment=0.95, got %v", m["truth_assessment"])
	}
}

func TestParseToolCallRejected(t *testing.T) {
	input := `{
		"type": "tool_call",
		"tool_call": {
			"thought": "thinking",
			"tool_name": "search",
			"tool_params": {"q": "test"}
		}
	}`
	result := llm.Result{Text: input}
	_, err := ParseResponse(result)
	if err == nil {
		t.Fatal("expected tool_call to be rejected")
	}
}

func TestParseResponseTriesAllJSONCandidatesBeforeSchemaFailure(t *testing.T) {
	result := llm.Result{Text: `preface {"note":"not an agent response"} then {"type":"final","output":"done"}`}

	resp, err := ParseResponse(result)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if resp.Final == nil || resp.Final.Output != "done" {
		t.Fatalf("ParseResponse() = %#v, want final output done", resp)
	}
}
