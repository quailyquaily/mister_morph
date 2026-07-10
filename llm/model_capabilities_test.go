package llm

import "testing"

func TestModelSupportsImageParts(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5.2", want: true},
		{model: "gemini-2.5-pro", want: true},
		{model: "gemma-4-vision", want: true},
		{model: "kimi-k2-vision", want: true},
		{model: "qwen3-vl-plus", want: true},
		{model: "muse-spark-1.1", want: true},
		{model: "meta/muse-spark-1.1", want: true},
		{model: "grok-4", want: true},
		{model: "grok-4-fast", want: true},
		{model: "grok-5", want: true},
		{model: "claude-3-7-sonnet", want: true},
		{model: "claude-sonnet-4", want: true},
		{model: "openrouter/gpt-4.1", want: true},
		{model: "google/gemini-2.0-flash", want: true},
		{model: "anthropic/claude-3-7-sonnet", want: true},
		{model: "xai/grok-4", want: true},
		{model: "vendor/models/grok-4-latest", want: true},
		{model: "vendor/models/gpt-4.1", want: true},
		{model: "vendor/models/gemini-2.5-pro", want: true},
		{model: "google/gemma-4-vision", want: true},
		{model: "moonshot/kimi-k2-vision", want: true},
		{model: "alibaba/qwen3-vl-plus", want: true},
		{model: "grok-3", want: false},
		{model: "xai/grok-3", want: false},
		{model: "gemma-3-vision", want: false},
		{model: "claude-2.1", want: false},
		{model: "anthropic/claude-2.1", want: false},
		{model: "claude", want: false},
		{model: "qwen-max", want: false},
		{model: "", want: false},
	}
	for _, tc := range tests {
		if got := ModelSupportsImageParts(tc.model); got != tc.want {
			t.Fatalf("ModelSupportsImageParts(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestModelSupportsWebPTranscode(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gpt-5.2", want: true},
		{model: "openrouter/gpt-4.1", want: true},
		{model: "gemini-2.5-pro", want: true},
		{model: "google/gemini-2.0-flash", want: true},
		{model: "claude-3-7-sonnet", want: true},
		{model: "anthropic/claude-sonnet-4", want: true},
		{model: "grok-4", want: false},
		{model: "qwen-max", want: false},
		{model: "", want: false},
	}
	for _, tc := range tests {
		if got := ModelSupportsWebPTranscode(tc.model); got != tc.want {
			t.Fatalf("ModelSupportsWebPTranscode(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}
