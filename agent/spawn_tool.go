package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/tools"
)

const spawnToolName = "spawn"

type spawnTool struct {
	engine *Engine
}

func (t *spawnTool) Name() string { return spawnToolName }

func (t *spawnTool) Description() string {
	return "Spawn a sub-agent to handle a self-contained sub-task. " +
		"The sub-agent runs with its own context and a restricted set of tools you specify. " +
		"This call blocks until the sub-agent completes and returns a structured JSON envelope. " +
		"Use this to parallelise independent work items."
}

func (t *spawnTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Task prompt for the sub-agent.",
			},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Whitelist of tool names the sub-agent can use. Cannot include 'spawn'.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override for the sub-agent. Defaults to the parent's model.",
			},
			"output_schema": map[string]any{
				"type":        "string",
				"description": "Optional schema identifier for the child task's structured output.",
			},
		},
		"required": []string{"task", "tools"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *spawnTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	task, _ := params["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("missing required param: task")
	}

	rawTools, _ := params["tools"].([]any)
	if len(rawTools) == 0 {
		return "", fmt.Errorf("missing required param: tools (must be a non-empty array of tool names)")
	}

	subRegistry := tools.NewRegistry()
	for _, raw := range rawTools {
		name, ok := raw.(string)
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, spawnToolName) {
			continue
		}
		if tool, found := t.engine.registry.Get(name); found {
			subRegistry.Register(tool)
		}
	}
	if len(subRegistry.All()) == 0 {
		return "", fmt.Errorf("none of the requested tools are available in the parent registry")
	}

	model, _ := params["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(t.engine.config.DefaultModel)
	}
	outputSchema, _ := params["output_schema"].(string)
	outputSchema = strings.TrimSpace(outputSchema)

	req := SubtaskRequest{
		Task:         task,
		Model:        model,
		OutputSchema: outputSchema,
		Registry:     subRegistry,
	}

	runner := t.engine.subtaskRunner
	if runner == nil {
		return "", fmt.Errorf("subtask runner unavailable")
	}
	result, err := runner.RunSubtask(ctx, req)
	if err != nil {
		if result == nil {
			result = FailedSubtaskResult("", err)
		}
	}
	if result == nil {
		result = FailedSubtaskResult("", fmt.Errorf("subtask returned nil result"))
	}

	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
