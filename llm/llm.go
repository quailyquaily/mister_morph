package llm

import (
	"context"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type Tool struct {
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ParametersJSON string `json:"parameters_json,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64 // USD
}

type Result struct {
	Text      string
	JSON      any
	ToolCalls []ToolCall
	Usage     Usage
	Duration  time.Duration
}

type Request struct {
	Model      string
	Messages   []Message
	Tools      []Tool
	ForceJSON  bool
	Parameters map[string]any
}

type Client interface {
	Chat(ctx context.Context, req Request) (Result, error)
}
