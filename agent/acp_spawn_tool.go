package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/acpclient"
)

const acpSpawnToolName = "acp_spawn"

type acpSpawnTool struct {
	deps acpSpawnToolDeps
}

func newACPSpawnTool(deps acpSpawnToolDeps) *acpSpawnTool {
	return &acpSpawnTool{deps: deps}
}

func (t *acpSpawnTool) Name() string { return acpSpawnToolName }

func (t *acpSpawnTool) Description() string {
	return "Spawn an external ACP agent to handle a self-contained sub-task. " +
		"The ACP agent runs over stdio, receives a single prompt turn, and returns a structured subtask envelope."
}

func (t *acpSpawnTool) ParameterSchema() string {
	s := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{
				"type":        "string",
				"description": "ACP agent profile name from acp.agents.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Task prompt for the external ACP agent.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Optional working directory override for the ACP session.",
			},
			"output_schema": map[string]any{
				"type":        "string",
				"description": "Optional schema identifier for the child task's structured output.",
			},
			"observe_profile": map[string]any{
				"type":        "string",
				"description": "Optional local observer profile for this child task. Supported values: default, long_shell, web_extract.",
			},
		},
		"required": []string{"agent", "task"},
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

func (t *acpSpawnTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	agentName, _ := params["agent"].(string)
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "", fmt.Errorf("missing required param: agent")
	}
	task, _ := params["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("missing required param: task")
	}
	if t.deps.LookupAgent == nil {
		return "", fmt.Errorf("acp agent lookup is unavailable")
	}
	cfg, ok := t.deps.LookupAgent(agentName)
	if !ok {
		return "", fmt.Errorf("unknown acp agent profile: %s", agentName)
	}
	cwd, _ := params["cwd"].(string)
	prepared, err := acpclient.PrepareAgentConfig(cfg, cwd)
	if err != nil {
		return "", err
	}

	outputSchema, _ := params["output_schema"].(string)
	outputSchema = strings.TrimSpace(outputSchema)
	observeProfile, _ := params["observe_profile"].(string)

	runner := t.deps.Runner
	if runner == nil {
		return "", fmt.Errorf("subtask runner unavailable")
	}
	runPrompt := t.deps.RunPrompt
	if runPrompt == nil {
		runPrompt = acpclient.RunPrompt
	}

	result, err := runner.RunSubtask(ctx, SubtaskRequest{
		OutputSchema:   outputSchema,
		ObserveProfile: NormalizeObserveProfile(observeProfile),
		RunFunc: func(runCtx context.Context) (*SubtaskResult, error) {
			runResult, runErr := runPrompt(runCtx, prepared, acpclient.RunRequest{
				Prompt:   BuildSubtaskTask(task, outputSchema),
				Observer: newACPObserver(runCtx, NormalizeObserveProfile(observeProfile)),
			})
			if runErr != nil {
				return nil, runErr
			}
			return subtaskResultFromACPResult("", outputSchema, runResult), nil
		},
	})
	if err != nil {
		if result == nil {
			result = FailedSubtaskResult("", err)
		}
	}
	if result == nil {
		result = FailedSubtaskResult("", fmt.Errorf("acp subtask returned nil result"))
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func newACPObserver(ctx context.Context, profile ObserveProfile) acpclient.Observer {
	return acpclient.ObserverFunc(func(eventCtx context.Context, event acpclient.Event) {
		if eventCtx == nil {
			eventCtx = ctx
		}
		switch event.Kind {
		case acpclient.EventKindAgentMessageChunk:
			if event.Text == "" {
				return
			}
			EmitEvent(eventCtx, nil, Event{
				Kind:     EventKindToolOutput,
				ToolName: acpSpawnToolName,
				Profile:  string(profile),
				Stream:   "agent",
				Text:     event.Text,
				Status:   "running",
			})
		case acpclient.EventKindToolCallStart:
			EmitEvent(eventCtx, nil, Event{
				Kind:     EventKindToolStart,
				ToolName: acpToolDisplayName(event),
				Profile:  string(profile),
				Status:   normalizeACPEventStatus(event.Status, "running"),
			})
			if event.Text != "" {
				EmitEvent(eventCtx, nil, Event{
					Kind:     EventKindToolOutput,
					ToolName: acpToolDisplayName(event),
					Profile:  string(profile),
					Stream:   "acp",
					Text:     event.Text,
					Status:   normalizeACPEventStatus(event.Status, "running"),
				})
			}
		case acpclient.EventKindToolCallUpdate:
			if event.Text == "" {
				return
			}
			EmitEvent(eventCtx, nil, Event{
				Kind:     EventKindToolOutput,
				ToolName: acpToolDisplayName(event),
				Profile:  string(profile),
				Stream:   "acp",
				Text:     event.Text,
				Status:   normalizeACPEventStatus(event.Status, "running"),
			})
		case acpclient.EventKindToolCallDone:
			EmitEvent(eventCtx, nil, Event{
				Kind:     EventKindToolDone,
				ToolName: acpToolDisplayName(event),
				Profile:  string(profile),
				Status:   normalizeACPEventStatus(event.Status, "done"),
				Error:    acpToolErrorText(event),
			})
		}
	})
}

func normalizeACPEventStatus(status string, fallback string) string {
	status = strings.TrimSpace(status)
	if status != "" {
		return status
	}
	return strings.TrimSpace(fallback)
}

func acpToolDisplayName(event acpclient.Event) string {
	if title := strings.TrimSpace(event.Title); title != "" {
		return title
	}
	if kind := strings.TrimSpace(event.ToolKind); kind != "" {
		return kind
	}
	if id := strings.TrimSpace(event.ToolCallID); id != "" {
		return id
	}
	return "acp_tool"
}

func acpToolErrorText(event acpclient.Event) string {
	if strings.TrimSpace(strings.ToLower(event.Status)) != "failed" {
		return ""
	}
	return strings.TrimSpace(event.Text)
}

func subtaskResultFromACPResult(taskID string, outputSchema string, result acpclient.RunResult) *SubtaskResult {
	stopReason := strings.TrimSpace(result.StopReason)
	if stopReason == "end_turn" {
		if strings.TrimSpace(outputSchema) != "" {
			return SubtaskResultFromFinal(taskID, outputSchema, &Final{Output: result.Output})
		}
		out := result.Output
		res := &SubtaskResult{
			TaskID:       strings.TrimSpace(taskID),
			Status:       SubtaskStatusDone,
			Summary:      "subtask completed",
			OutputKind:   SubtaskOutputKindText,
			OutputSchema: "",
			Output:       out,
			Error:        "",
		}
		if summary := summarizeSubtaskText(out); summary != "" {
			res.Summary = summary
		}
		return res
	}
	msg := "acp stop reason: " + stopReason
	if stopReason == "" {
		msg = "acp task failed"
	}
	out := FailedSubtaskResult(taskID, fmt.Errorf("%s", msg))
	out.Output = result.Output
	if strings.TrimSpace(result.Output) != "" {
		out.OutputKind = SubtaskOutputKindText
	}
	if strings.TrimSpace(outputSchema) != "" {
		out.OutputSchema = strings.TrimSpace(outputSchema)
	}
	return out
}
