package jsonutil

import (
	"errors"
	"testing"
)

func TestDecodeWithFallbackDirectJSON(t *testing.T) {
	var out struct {
		KeepIndices []int `json:"keep_indices"`
	}
	err := DecodeWithFallback(`{"keep_indices":[0,2]}`, &out)
	if err != nil {
		t.Fatalf("DecodeWithFallback() error = %v", err)
	}
	if len(out.KeepIndices) != 2 || out.KeepIndices[0] != 0 || out.KeepIndices[1] != 2 {
		t.Fatalf("unexpected keep_indices: %#v", out.KeepIndices)
	}
}

func TestDecodeWithFallbackCodeFenceJSON(t *testing.T) {
	var out struct {
		Status string `json:"status"`
	}
	err := DecodeWithFallback("```json\n{\"status\":\"ok\"}\n```", &out)
	if err != nil {
		t.Fatalf("DecodeWithFallback() error = %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("status = %q, want ok", out.Status)
	}
}

func TestDecodeWithFallbackEmptyInput(t *testing.T) {
	var out map[string]any
	err := DecodeWithFallback(" \n\t ", &out)
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("expected ErrEmptyInput, got %v", err)
	}
}

func TestDecodeWithFallbackRejectsInvalidInput(t *testing.T) {
	var out map[string]any
	err := DecodeWithFallback("not a json payload", &out)
	if err == nil {
		t.Fatalf("expected error for invalid input")
	}
}

func TestFindJSONCandidatesReturnsAllValidPayloads(t *testing.T) {
	text := `preface {"note":"not an agent response"} then {"type":"final","output":"done"}`
	candidates, err := FindJSONCandidates(text)
	if err != nil {
		t.Fatalf("FindJSONCandidates() error = %v", err)
	}
	if len(candidates) < 2 {
		t.Fatalf("FindJSONCandidates() returned %d candidates, want at least 2", len(candidates))
	}
}

func TestFindJSONCandidatesReturnsDirectPayloadOnce(t *testing.T) {
	candidates, err := FindJSONCandidates("  {\"status\":\"ok\"}  ")
	if err != nil {
		t.Fatalf("FindJSONCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("FindJSONCandidates() returned %d candidates, want 1", len(candidates))
	}
	if got := string(candidates[0]); got != `{"status":"ok"}` {
		t.Fatalf("candidate = %q, want direct payload", got)
	}
}
