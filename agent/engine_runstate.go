package agent

import (
	"encoding/json"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

type resumeStateV1 struct {
	Version int `json:"v"`

	RunID string `json:"run_id"`
	Model string `json:"model"`
	Step  int    `json:"step"`

	PlanRequired  bool `json:"plan_required"`
	ParseFailures int  `json:"parse_failures"`

	SkillAuthProfiles []string `json:"skill_auth_profiles,omitempty"`
	EnforceSkillAuth  bool     `json:"enforce_skill_auth,omitempty"`

	Messages    []llm.Message   `json:"messages"`
	ExtraParams map[string]any  `json:"extra_params,omitempty"`
	AgentCtx    contextSnapshot `json:"agent_ctx"`

	PendingTool pendingToolSnapshot `json:"pending_tool"`
}

type pendingToolSnapshot struct {
	AssistantText      string     `json:"assistant_text"`
	AssistantTextAdded bool       `json:"assistant_text_added,omitempty"`
	ToolCall           ToolCall   `json:"tool_call"`
	RemainingToolCalls []ToolCall `json:"remaining_tool_calls,omitempty"`
}

type contextSnapshot struct {
	Task     string         `json:"task"`
	MaxSteps int            `json:"max_steps"`
	Plan     *Plan          `json:"plan,omitempty"`
	Metrics  *Metrics       `json:"metrics,omitempty"`
	Steps    []stepSnapshot `json:"steps,omitempty"`
}

type stepSnapshot struct {
	StepNumber  int            `json:"step"`
	Thought     string         `json:"thought,omitempty"`
	Action      string         `json:"action,omitempty"`
	ActionInput map[string]any `json:"action_input,omitempty"`
	Observation string         `json:"observation,omitempty"`
	Error       string         `json:"error,omitempty"`
	DurationMs  int64          `json:"duration_ms,omitempty"`
}

func snapshotFromContext(c *Context) contextSnapshot {
	if c == nil {
		return contextSnapshot{}
	}
	out := contextSnapshot{
		Task:     c.Task,
		MaxSteps: c.MaxSteps,
		Plan:     c.Plan,
		Metrics:  c.Metrics,
	}
	if len(c.Steps) == 0 {
		return out
	}
	steps := make([]stepSnapshot, 0, len(c.Steps))
	for _, s := range c.Steps {
		var errStr string
		if s.Error != nil {
			errStr = s.Error.Error()
		}
		steps = append(steps, stepSnapshot{
			StepNumber:  s.StepNumber,
			Thought:     s.Thought,
			Action:      s.Action,
			ActionInput: s.ActionInput,
			Observation: s.Observation,
			Error:       errStr,
			DurationMs:  s.Duration.Milliseconds(),
		})
	}
	out.Steps = steps
	return out
}

func contextFromSnapshot(s contextSnapshot) *Context {
	c := NewContext(s.Task, s.MaxSteps)
	c.Plan = s.Plan
	if s.Metrics != nil {
		c.Metrics = s.Metrics
	}
	for _, ss := range s.Steps {
		var err error
		if ss.Error != "" {
			err = &stringError{msg: ss.Error}
		}
		c.Steps = append(c.Steps, Step{
			StepNumber:  ss.StepNumber,
			Thought:     ss.Thought,
			Action:      ss.Action,
			ActionInput: ss.ActionInput,
			Observation: ss.Observation,
			Error:       err,
			Duration:    time.Duration(ss.DurationMs) * time.Millisecond,
		})
	}
	return c
}

type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

func marshalResumeState(st resumeStateV1) ([]byte, error) {
	st.Version = 1
	return json.Marshal(st)
}

func unmarshalResumeState(b []byte) (resumeStateV1, error) {
	var st resumeStateV1
	if err := json.Unmarshal(b, &st); err != nil {
		return resumeStateV1{}, err
	}
	return st, nil
}
