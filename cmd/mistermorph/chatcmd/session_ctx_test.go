package chatcmd

import (
	"encoding/json"
	"testing"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

func TestChatSessionContextStatus(t *testing.T) {
	sess := &chatSession{
		mainRoute: llmutil.ResolvedRoute{
			Values: llmutil.RuntimeValues{
				Provider: "openai",
				Model:    "gpt-5.2",
			},
		},
		mainCfg: llmconfig.ClientConfig{
			Provider: "openai",
			Model:    "gpt-5.2",
		},
		toolRegistry:   tools.NewRegistry(),
		basePromptSpec: agent.DefaultPromptSpec(),
		promptSpec:     agent.DefaultPromptSpec(),
		lastContextBudget: &agent.ContextBudgetState{
			CompressionCount: 2,
		},
	}

	raw, err := sess.contextStatus([]llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})
	if err != nil {
		t.Fatalf("contextStatus() error = %v", err)
	}

	var payload struct {
		CurrentTokens    int `json:"current_tokens"`
		ContextWindow    int `json:"context_window"`
		CompressionCount int `json:"compression_count"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("parse contextStatus JSON: %v", err)
	}
	if payload.CurrentTokens <= 0 {
		t.Fatalf("current_tokens = %d, want > 0", payload.CurrentTokens)
	}
	if payload.ContextWindow != 400000 {
		t.Fatalf("context_window = %d, want 400000", payload.ContextWindow)
	}
	if payload.CompressionCount != 2 {
		t.Fatalf("compression_count = %d, want 2", payload.CompressionCount)
	}
}
