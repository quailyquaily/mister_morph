package llm

import (
	"testing"
)

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

func TestResolveModelContextWindowCatalogExamples(t *testing.T) {
	tests := map[string]int64{
		"gpt-5.6":                          1050000,
		"gpt-5.6-sol":                      1050000,
		"gpt-5.6-terra":                    1050000,
		"gpt-5.6-luna":                     1050000,
		"gpt-5.5-pro":                      1050000,
		"claude-opus-5":                    1000000,
		"claude-sonnet-5":                  1000000,
		"claude-sonnet-4-6":                1000000,
		"claude-haiku-4-5":                 200000,
		"gemini-2.5-pro":                   1048576,
		"kimi-k3":                          1000000,
		"kimi-k2.6":                        262144,
		"MiniMax-M2.7":                     204800,
		"deepseek-chat":                    1000000,
		"openai/gpt-oss-120b":              131072,
		"@cf/moonshotai/kimi-k2.5":         256000,
		"GLM-4.5-AirX":                     128000,
		"grok-4.6":                         500000,
		"grok-4.6-latest":                  500000,
		"grok-4.5-latest":                  500000,
		"grok-4.20-reasoning":              1000000,
		"grok-4.20-multi-agent":            1000000,
		"muse-spark-1.1":                   1000000,
		"mistral-medium-latest":            128000,
		"command-r-plus-08-2024":           128000,
		"gpt-5.4-pro-2026-03-05":           1050000,
		"gemini-3.1-pro-preview":           1048576,
		"claude-opus-4-5-20250929":         1000000,
		"gemini-2.5-flash-preview-09-2025": 1048576,
	}
	for model, want := range tests {
		t.Run(model, func(t *testing.T) {
			got, ok := ResolveModelContextWindow(model)
			if !ok {
				t.Fatalf("ResolveModelContextWindow(%q) found = false, want true", model)
			}
			if got.ContextWindowTokens != want {
				t.Fatalf("context window = %d, want %d", got.ContextWindowTokens, want)
			}
		})
	}
}
