package llm

import "testing"

func TestShortModelName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		model string
		want  string
	}{
		{name: "provider namespace", model: "openai/gpt-5.5", want: "gpt-5.5"},
		{name: "plain model", model: "grok-4.5", want: "grok-4.5"},
		{name: "nested namespace", model: "gateway/openai/gpt-5.5", want: "gpt-5.5"},
		{name: "preserves case", model: "  Alibaba/Qwen3.6  ", want: "Qwen3.6"},
		{name: "empty", model: "  ", want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShortModelName(tt.model); got != tt.want {
				t.Fatalf("ShortModelName(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}
