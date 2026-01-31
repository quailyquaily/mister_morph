package agent

import (
	"encoding/json"
	"time"

	"github.com/quailyquaily/mister_morph/llm"
)

const (
	TypeToolCall    = "tool_call"
	TypePlan        = "plan"
	TypeFinal       = "final"
	TypeFinalAnswer = "final_answer"
)

type ToolCall struct {
	Thought string         `json:"thought"`
	Name    string         `json:"tool_name"`
	Params  map[string]any `json:"tool_params"`
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
	Thought    string    `json:"thought,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Steps      PlanSteps `json:"steps,omitempty"`
	Risks      []string  `json:"risks,omitempty"`
	Questions  []string  `json:"questions,omitempty"`
	Completion string    `json:"completion,omitempty"`
}

type Final struct {
	Thought string `json:"thought,omitempty"`
	Output  any    `json:"output,omitempty"`
	Plan    *Plan  `json:"plan,omitempty"`
}

type AgentResponse struct {
	Type        string    `json:"type"`
	ToolCall    *ToolCall `json:"tool_call,omitempty"`
	Plan        *Plan     `json:"plan,omitempty"`
	Final       *Final    `json:"final,omitempty"`
	FinalAnswer *Final    `json:"final_answer,omitempty"`
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
	History []llm.Message
}

type RawJSON json.RawMessage
