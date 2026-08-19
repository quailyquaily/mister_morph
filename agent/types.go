package agent

import (
	"encoding/json"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

const (
	TypeToolCall    = "tool_call"
	TypePlan        = "plan"
	TypeFinal       = "final"
	TypeFinalAnswer = "final_answer"
)

type ToolCall struct {
	ID               string         `json:"tool_call_id,omitempty"`
	Type             string         `json:"tool_call_type,omitempty"`
	Thought          string         `json:"thought"`
	Name             string         `json:"tool_name"`
	Params           map[string]any `json:"tool_params"`
	RawArguments     string         `json:"raw_arguments,omitempty"`
	ThoughtSignature string         `json:"thought_signature,omitempty"`
}

type PlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status,omitempty"` // pending|in_progress|completed
}

type PlanSteps []PlanStep

func (s *PlanSteps) UnmarshalJSON(data []byte) error {
	// Accept both:
	//  - ["step 1", "step 2"]
	//  - [{"step":"step 1","status":"pending"}, ...]
	var asStrings []string
	if err := json.Unmarshal(data, &asStrings); err == nil {
		out := make([]PlanStep, 0, len(asStrings))
		for _, v := range asStrings {
			out = append(out, PlanStep{Step: v})
		}
		*s = out
		return nil
	}

	var asSteps []PlanStep
	if err := json.Unmarshal(data, &asSteps); err != nil {
		return err
	}
	*s = asSteps
	return nil
}

type Plan struct {
	Thought string    `json:"reasoning,omitempty"`
	Steps   PlanSteps `json:"steps,omitempty"`
}

type PlanStepUpdate struct {
	CompletedIndex int
	CompletedStep  string
	StartedIndex   int
	StartedStep    string
	Reason         string
}

type Final struct {
	Thought       string `json:"reasoning,omitempty"`
	Output        any    `json:"output,omitempty"`
	Plan          *Plan  `json:"plan,omitempty"`
	Reaction      string `json:"reaction,omitempty"`
	IsLightweight bool   `json:"is_lightweight,omitempty"`
}

type SteerSource interface {
	Drain() []string
	DrainAndClose() []string
	Close()
}

type AgentResponse struct {
	Type           string          `json:"type"`
	ToolCall       *ToolCall       `json:"tool_call,omitempty"`
	ToolCalls      []ToolCall      `json:"tool_calls,omitempty"`
	Plan           *Plan           `json:"plan,omitempty"`
	Final          *Final          `json:"final,omitempty"`
	FinalAnswer    *Final          `json:"final_answer,omitempty"`
	RawFinalAnswer json.RawMessage `json:"-"`
}

func (r *AgentResponse) FinalPayload() *Final {
	if r.Final != nil {
		return r.Final
	}
	return r.FinalAnswer
}

func (r *AgentResponse) PlanPayload() *Plan {
	return r.Plan
}

type Step struct {
	StepNumber  int
	Thought     string
	Action      string
	ActionInput map[string]any
	Observation string
	Error       error
	Duration    time.Duration
}

type RunOptions struct {
	Model   string
	Scene   string
	History []llm.Message
	Meta    map[string]any
	// CurrentMessage, when set, is appended as the final user turn after meta and history.
	CurrentMessage *llm.Message
	// OnStream receives provider stream events for each model call in this run.
	OnStream llm.StreamHandler
	// ReasoningDetails requests readable reasoning from providers that support it.
	ReasoningDetails bool
	// SteerSource provides user input that arrived while this run was active.
	// Items are injected at safe checkpoints before model calls.
	SteerSource SteerSource
	// SkipTaskMessage suppresses appending task as a trailing user message.
	// Useful when the current user input is represented elsewhere and no raw task fallback should be added.
	SkipTaskMessage bool
	// ContextWindowTokens is resolved from the route selected for this run.
	ContextWindowTokens int64
	// ContextCheckpointStore persists the checkpoint for this conversation. A
	// run-local store is used when this is nil.
	ContextCheckpointStore ContextCheckpointStore
	// HistoryBoundaries aligns with History and identifies the external item
	// covered when that rendered history message is compacted.
	HistoryBoundaries []string
	// CurrentMessageBoundary identifies the current external inbound message.
	CurrentMessageBoundary string
	// DisableContextCompaction explicitly disables active and passive compaction
	// for this run. Awareness uses this option.
	DisableContextCompaction bool
	// ContextCompactionOnly forces one checkpoint compaction and returns without
	// making a main-loop model request. Conversation runtimes use it for
	// /ctx compact.
	ContextCompactionOnly bool
}
