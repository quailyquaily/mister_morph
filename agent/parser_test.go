package agent

import (
	"encoding/json"
	"testing"

	"github.com/quailyquaily/mister_morph/llm"
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
		"final_answer": {
			"thought": "done",
			"output": "hello",
			"sources": ["a", "b"]
		}
	}`
	result := llm.Result{Text: input}
	resp, err := ParseResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RawFinalAnswer == nil {
		t.Fatal("expected RawFinalAnswer to be populated")
	}

	// RawFinalAnswer should contain the raw JSON of the final_answer object
	var m map[string]any
	if err := json.Unmarshal(resp.RawFinalAnswer, &m); err != nil {
		t.Fatalf("RawFinalAnswer is not valid JSON: %v", err)
	}
	if m["thought"] != "done" {
		t.Errorf("expected thought='done', got %v", m["thought"])
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
		"final": {
			"thought": "done",
			"output": "result",
			"truth_assessment": 0.95
		}
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

func TestParseToolCallLeavesRawFinalAnswerNil(t *testing.T) {
	input := `{
		"type": "tool_call",
		"tool_call": {
			"thought": "thinking",
			"tool_name": "search",
			"tool_params": {"q": "test"}
		}
	}`
	result := llm.Result{Text: input}
	resp, err := ParseResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RawFinalAnswer != nil {
		t.Error("expected RawFinalAnswer to be nil for tool_call type")
	}
}
