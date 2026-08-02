package uniai

import (
	"testing"

	"github.com/quailyquaily/mistermorph/llm"
	uniaiapi "github.com/quailyquaily/uniai"
	uniaichat "github.com/quailyquaily/uniai/chat"
)

func TestBuildChatOptionsMapsReasoningStream(t *testing.T) {
	var received llm.StreamEvent
	req := llm.Request{
		Model:            "gpt-5.4",
		Messages:         []llm.Message{{Role: "user", Content: "test"}},
		ReasoningDetails: true,
		OnStream: func(event llm.StreamEvent) error {
			received = event
			return nil
		},
	}

	opts := buildChatOptionsForTest(
		req,
		"openai_resp",
		"gpt-5.4",
		"",
		"",
		false,
		uniaiapi.ToolsEmulationOff,
		nil,
		"",
		nil,
	)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if !built.Options.ReasoningDetails {
		t.Fatal("ReasoningDetails = false, want true")
	}
	if built.Options.OnStream == nil {
		t.Fatal("OnStream = nil")
	}

	err = built.Options.OnStream(uniaiapi.StreamEvent{
		ReasoningDelta: &uniaiapi.ReasoningDelta{
			Index: 2,
			Type:  uniaiapi.ReasoningDeltaSummary,
			Delta: "inspect first",
		},
	})
	if err != nil {
		t.Fatalf("OnStream: %v", err)
	}
	if received.ReasoningDelta == nil {
		t.Fatal("received.ReasoningDelta = nil")
	}
	if received.ReasoningDelta.Index != 2 ||
		received.ReasoningDelta.Type != llm.ReasoningDeltaSummary ||
		received.ReasoningDelta.Delta != "inspect first" {
		t.Fatalf("received.ReasoningDelta = %#v", received.ReasoningDelta)
	}
}

func TestBuildChatOptionsSkipsUnsupportedReasoningDetails(t *testing.T) {
	req := llm.Request{
		Model:            "gpt-4.1",
		Messages:         []llm.Message{{Role: "user", Content: "test"}},
		ReasoningDetails: true,
		OnStream:         func(llm.StreamEvent) error { return nil },
	}

	opts := buildChatOptionsForTest(
		req,
		"openai",
		"gpt-4.1",
		"",
		"",
		false,
		uniaiapi.ToolsEmulationOff,
		nil,
		"",
		nil,
	)
	built, err := uniaichat.BuildRequest(opts...)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if built.Options.ReasoningDetails {
		t.Fatal("ReasoningDetails = true for unsupported OpenAI Chat Completions model")
	}
}

func TestSupportsReasoningDetails(t *testing.T) {
	budget := 8192
	tests := []struct {
		name      string
		provider  string
		model     string
		effort    string
		budget    *int
		supported bool
	}{
		{name: "OpenAI Responses reasoning model", provider: "openai_resp", model: "gpt-5.4", supported: true},
		{name: "OpenAI Responses non-reasoning model", provider: "openai_resp", model: "gpt-4.1", supported: false},
		{name: "Kimi chat completions", provider: "openai", model: "kimi-k3", supported: true},
		{name: "DeepSeek provider", provider: "deepseek", model: "deepseek-v4", supported: true},
		{name: "Gemini thinking model", provider: "gemini", model: "gemini-2.5-pro", supported: true},
		{name: "Gemini legacy model", provider: "gemini", model: "gemini-2.0-flash", supported: false},
		{name: "Anthropic adaptive thinking", provider: "anthropic", model: "claude-opus-4-7", supported: true},
		{name: "Anthropic budget thinking", provider: "anthropic", model: "claude-3-7-sonnet", budget: &budget, supported: true},
		{name: "Anthropic without reasoning controls", provider: "anthropic", model: "claude-3-7-sonnet", supported: false},
		{name: "Unsupported compatible provider", provider: "xai", model: "grok-4.1-fast-reasoning", effort: "high", supported: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := supportsReasoningDetails(tc.provider, tc.model, tc.effort, tc.budget); got != tc.supported {
				t.Fatalf("supportsReasoningDetails() = %v, want %v", got, tc.supported)
			}
		})
	}
}
