package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/tools"
)

type planCreateTool struct {
	client       llm.Client
	defaultModel string
	toolNames    []string
}

func registerPlanTool(reg *tools.Registry, client llm.Client, defaultModel string) {
	if reg == nil {
		return
	}
	names := toolNames(reg)
	names = append(names, "plan_create")
	reg.Register(newPlanCreateTool(client, defaultModel, names))
}

func toolNames(reg *tools.Registry) []string {
	all := reg.All()
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.Name())
	}
	return out
}

func newPlanCreateTool(client llm.Client, defaultModel string, toolNames []string) *planCreateTool {
	return &planCreateTool{
		client:       client,
		defaultModel: strings.TrimSpace(defaultModel),
		toolNames:    toolNames,
	}
}

func (t *planCreateTool) Name() string { return "plan_create" }

func (t *planCreateTool) Description() string {
	return "Generate a concise execution plan for a task as JSON (plan object with thought/summary/steps/risks/questions). Use when you want a plan before execution."
}

func (t *planCreateTool) ParameterSchema() string {
	s := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Task description to plan for.",
			},
			"max_steps": map[string]any{
				"type":        "integer",
				"description": "Maximum number of steps (default: 6).",
			},
			"style": map[string]any{
				"type":        "string",
				"description": "Optional style hint (e.g., terse, detailed).",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override for plan generation.",
			},
		},
		"required": []string{"task"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

type planCreatePlan struct {
	Thought    string          `json:"thought"`
	Summary    string          `json:"summary"`
	Steps      agent.PlanSteps `json:"steps"`
	Risks      []string        `json:"risks"`
	Questions  []string        `json:"questions"`
	Completion string          `json:"completion"`
}

type planCreateOutput struct {
	Plan planCreatePlan `json:"plan"`
}

type planCreateLegacy struct {
	Thought   string          `json:"thought"`
	Summary   string          `json:"summary"`
	Steps     agent.PlanSteps `json:"steps"`
	Risks     []string        `json:"risks"`
	Questions []string        `json:"questions"`
}

func (t *planCreateTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t == nil || t.client == nil {
		return "", fmt.Errorf("plan_create unavailable (missing llm client)")
	}
	task, _ := params["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("missing required param: task")
	}

	maxSteps := 6
	if v, ok := params["max_steps"]; ok {
		switch x := v.(type) {
		case int:
			if x > 0 {
				maxSteps = x
			}
		case int64:
			if x > 0 {
				maxSteps = int(x)
			}
		case float64:
			if x > 0 {
				maxSteps = int(x)
			}
		}
	}

	style, _ := params["style"].(string)
	style = strings.TrimSpace(style)

	model, _ := params["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = t.defaultModel
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	payload := map[string]any{
		"task":            task,
		"max_steps":       maxSteps,
		"style":           style,
		"available_tools": t.toolNames,
		"constraints": []string{
			"Use only available_tools when describing steps that involve tools.",
			"Keep steps executable and concise.",
			"Assume required credentials are already configured when a skill references an auth_profile; do not add steps asking the user to confirm keys unless a tool error explicitly indicates missing configuration.",
		},
	}
	payloadJSON, _ := json.Marshal(payload)

	sys := strings.TrimSpace(`
You generate a concise execution plan.
Return ONLY JSON:
{
  "plan": {
    "thought": "brief reasoning (optional)",
    "summary": "1-2 sentence overview",
    "steps": [{"step":"step 1","status":"in_progress"},{"step":"step 2","status":"pending"}],
    "risks": ["optional"],
    "questions": ["optional clarifying questions"],
    "completion": "optional definition of done"
  }
}
Rules:
- Steps should be actionable and ordered.
- Keep within max_steps.
- If no questions, return an empty array.
`)

	res, err := t.client.Chat(ctx, llm.Request{
		Model:     model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: string(payloadJSON)},
		},
		Parameters: map[string]any{
			"max_tokens": 400,
		},
	})
	if err != nil {
		return "", err
	}

	var out planCreateOutput
	if err := jsonutil.DecodeWithFallback(res.Text, &out); err != nil {
		return "", fmt.Errorf("invalid plan_create response")
	}

	if strings.TrimSpace(out.Plan.Summary) == "" && len(out.Plan.Steps) == 0 {
		var legacy planCreateLegacy
		if err := jsonutil.DecodeWithFallback(res.Text, &legacy); err == nil {
			out.Plan.Thought = legacy.Thought
			out.Plan.Summary = legacy.Summary
			out.Plan.Steps = legacy.Steps
			out.Plan.Risks = legacy.Risks
			out.Plan.Questions = legacy.Questions
		}
	}

	out.Plan.Thought = strings.TrimSpace(out.Plan.Thought)
	out.Plan.Summary = strings.TrimSpace(out.Plan.Summary)
	if len(out.Plan.Steps) > maxSteps {
		out.Plan.Steps = out.Plan.Steps[:maxSteps]
	}
	out.Plan.Steps = normalizePlanSteps(out.Plan.Steps)

	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

func normalizePlanSteps(steps agent.PlanSteps) agent.PlanSteps {
	if len(steps) == 0 {
		return steps
	}
	for i := range steps {
		steps[i].Step = strings.TrimSpace(steps[i].Step)
		if steps[i].Step == "" {
			steps[i].Step = fmt.Sprintf("step %d", i+1)
		}
		status := strings.ToLower(strings.TrimSpace(steps[i].Status))
		if status == "" {
			if i == 0 {
				steps[i].Status = "in_progress"
			} else {
				steps[i].Status = "pending"
			}
			continue
		}
		switch status {
		case "pending", "in_progress", "completed":
			steps[i].Status = status
		default:
			steps[i].Status = "pending"
		}
	}
	return steps
}
