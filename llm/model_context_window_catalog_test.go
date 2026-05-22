package llm

import "testing"

func TestResolveModelContextWindow(t *testing.T) {
	tests := []string{
		"gpt-5.5",
		"openai/gpt-5.5",
		"gpt-5.5-2026-05-22",
		"vendor/models/gpt-5.5-2026-05-22",
	}
	for _, model := range tests {
		t.Run(model, func(t *testing.T) {
			got, ok := ResolveModelContextWindow(model)
			if !ok {
				t.Fatalf("ResolveModelContextWindow(%q) found = false, want true", model)
			}
			if got.ContextWindowTokens != 1050000 {
				t.Fatalf("context window = %d, want 1050000", got.ContextWindowTokens)
			}
			if len(got.Sources) == 0 || got.Sources[0].URL == "" {
				t.Fatalf("sources = %#v, want official source URL", got.Sources)
			}
		})
	}
}

func TestResolveModelContextWindowUnknown(t *testing.T) {
	if _, ok := ResolveModelContextWindow("unknown-model"); ok {
		t.Fatalf("ResolveModelContextWindow(unknown-model) found = true, want false")
	}
}
