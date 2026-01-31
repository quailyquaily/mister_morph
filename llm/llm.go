package llm

import (
	"context"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64 // USD
}

type Result struct {
	Text     string
	JSON     any
	Usage    Usage
	Duration time.Duration
}

type Request struct {
	Model      string
	Messages   []Message
	ForceJSON  bool
	Parameters map[string]any
}

type Client interface {
	Chat(ctx context.Context, req Request) (Result, error)
}
