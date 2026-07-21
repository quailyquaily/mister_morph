package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/jsonutil"
	"github.com/quailyquaily/mistermorph/internal/promptprofile"
	"github.com/quailyquaily/mistermorph/llm"
)

type planCreateTool struct {
	client          llm.Client
	defaultModel    string
	defaultMaxSteps int
	toolNames       []string
	personaDir      string
}

func NewPlanCreateTool(client llm.Client, defaultModel string, toolNames []string, defaultMaxSteps int) *planCreateTool {
	return NewPlanCreateToolWithPersona(client, defaultModel, toolNames, defaultMaxSteps, "")
}

func NewPlanCreateToolWithPersona(client llm.Client, defaultModel string, toolNames []string, defaultMaxSteps int, personaDir string) *planCreateTool {
	return &planCreateTool{
		client:          client,
		defaultModel:    strings.TrimSpace(defaultModel),
		defaultMaxSteps: defaultMaxSteps,
		toolNames:       toolNames,
		personaDir:      strings.TrimSpace(personaDir),
	}
}

func (t *planCreateTool) Name() string { return "plan_create" }

func (t *planCreateTool) Description() string {
	return "Generate a concise execution plan for a task as JSON (plan object with thought/steps). Use when you want a plan before execution."
}

func (t *planCreateTool) ParameterSchema() string {
	maxSteps := t.defaultMaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}
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
				"description": fmt.Sprintf("Maximum number of steps (default: %d).", maxSteps),
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
	Thought string          `json:"thought"`
	Steps   agent.PlanSteps `json:"steps"`
}

type planCreateOutput struct {
	Plan planCreatePlan `json:"plan"`
}

func planCreateIdentity(personaDir string) string {
	personaDir = strings.TrimSpace(personaDir)
	if personaDir == "" {
		return ""
	}
	spec := agent.DefaultPromptSpec()
	promptprofile.ApplyPersonaIdentity(&spec, nil, personaDir)
	return strings.TrimSpace(spec.Identity)
}

func buildPlanCreateSystemPrompt(personaDir string) string {
	base := strings.TrimSpace(`
You generate a concise execution plan.
Return ONLY JSON:
{
  "plan": {
    "thought": "brief reasoning",
    "steps": [{"step":"brief reasoning step 1","status":"in_progress"},{"step":"brief reasoning step 2","status":"pending"}]
  }
}
Rules:
- Steps should be actionable and ordered.
- Keep within max_steps.
- Follow the your IDENTITY and SOUL for generate the 'thought' and 'step'.
- Always use the same language in 'thought' and 'step' as the 'task', and keep them conversational and concise, in plain-text.
- tools name should be wrapped with backtick quotes.
`)
	identity := planCreateIdentity(personaDir)
	if identity == "" {
		return base
	}
	return identity + "\n\n" + base
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

	maxSteps := t.defaultMaxSteps
	if maxSteps <= 0 {
		maxSteps = 6
	}
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

	model, _ := params["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = t.defaultModel
	}
	if model == "" {
		return "", fmt.Errorf("the model is empty, can't use the llm for plan_create tool.")
	}

	payload := map[string]any{
		"task":            task,
		"max_steps":       maxSteps,
		"available_tools": t.toolNames,
		"constraints": []string{
			"Use only available_tools when describing steps that involve tools.",
			"Keep steps executable and concise.",
			"Assume required credentials are already configured when a skill references an auth_profile; do not add steps asking the user to confirm keys unless a tool error explicitly indicates missing configuration.",
		},
	}
	payloadJSON, _ := json.Marshal(payload)

	sys := buildPlanCreateSystemPrompt(t.personaDir)

	res, err := t.client.Chat(ctx, llm.Request{
		Model:     model,
		ForceJSON: true,
		Messages: []llm.Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: string(payloadJSON)},
		},
		Parameters: map[string]any{
			"max_tokens": 4096,
		},
	})
	if err != nil {
		return "", err
	}

	var out planCreateOutput
	if err := jsonutil.DecodeWithFallback(res.Text, &out); err != nil {
		return "", fmt.Errorf("invalid plan_create response")
	}

	out.Plan.Thought = strings.TrimSpace(out.Plan.Thought)
	normalized := &agent.Plan{Steps: out.Plan.Steps}
	agent.NormalizePlanSteps(normalized)
	out.Plan.Steps = normalized.Steps
	if len(out.Plan.Steps) == 0 {
		return "", fmt.Errorf("invalid plan_create response: empty steps")
	}
	if len(out.Plan.Steps) > maxSteps {
		out.Plan.Steps = out.Plan.Steps[:maxSteps]
	}

	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
