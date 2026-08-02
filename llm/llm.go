package llm

import (
	"context"
	"time"
)

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	Parts            []Part     `json:"parts,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

type CacheControl struct {
	TTL string `json:"ttl,omitempty"`
}

const (
	PartTypeText        = "text"
	PartTypeImageURL    = "image_url"
	PartTypeImageBase64 = "image_base64"
)

type Part struct {
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	URL          string        `json:"url,omitempty"`
	DataBase64   string        `json:"data_base64,omitempty"`
	MIMEType     string        `json:"mime_type,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type Tool struct {
	Name           string        `json:"name"`
	Description    string        `json:"description,omitempty"`
	ParametersJSON string        `json:"parameters_json,omitempty"`
	CacheControl   *CacheControl `json:"cache_control,omitempty"`
}

type ToolCall struct {
	ID               string         `json:"id,omitempty"`
	Type             string         `json:"type,omitempty"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	RawArguments     string         `json:"raw_arguments,omitempty"`
	ThoughtSignature string         `json:"thought_signature,omitempty"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cache        UsageCache
	Cost         *UsageCost
}

type UsageCache struct {
	CachedInputTokens        int
	CacheCreationInputTokens int
	Details                  map[string]int
}

type UsageCost struct {
	Currency           string
	Estimated          bool
	Input              float64
	CachedInput        float64
	CacheCreationInput float64
	Output             float64
	Total              float64
}

type StreamToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	ArgsChunk string
}

type ReasoningDeltaType string

const (
	ReasoningDeltaSummary  ReasoningDeltaType = "summary"
	ReasoningDeltaThinking ReasoningDeltaType = "thinking"
)

type ReasoningDelta struct {
	Index int
	Type  ReasoningDeltaType
	Delta string
}

type StreamEvent struct {
	Delta          string
	ReasoningDelta *ReasoningDelta
	ToolCallDelta  *StreamToolCallDelta
	Usage          *Usage
	Done           bool
}

type StreamHandler func(event StreamEvent) error

type Result struct {
	Text      string
	Parts     []Part
	Messages  []Message
	JSON      any
	ToolCalls []ToolCall
	Usage     Usage
	Duration  time.Duration
}

type Request struct {
	Model             string
	InferenceProvider string
	Scene             string
	Messages          []Message
	Tools             []Tool
	ForceJSON         bool
	Parameters        map[string]any
	DebugFn           func(label, payload string)
	ReasoningDetails  bool
	OnStream          StreamHandler
}

type Client interface {
	Chat(ctx context.Context, req Request) (Result, error)
}
