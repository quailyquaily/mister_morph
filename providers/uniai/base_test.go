package uniai

import "testing"

func TestNormalizeOpenAIBaseKeepsCodexBackendPath(t *testing.T) {
	got := normalizeOpenAIBase("https://chatgpt.com/backend-api/codex/")
	if got != "https://chatgpt.com/backend-api/codex" {
		t.Fatalf("normalizeOpenAIBase() = %q", got)
	}
}

func TestNormalizeOpenAIBaseAddsV1ForOpenAICompatibleEndpoints(t *testing.T) {
	got := normalizeOpenAIBase("https://api.example.com")
	if got != "https://api.example.com/v1" {
		t.Fatalf("normalizeOpenAIBase() = %q", got)
	}
}
