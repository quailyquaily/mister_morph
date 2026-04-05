package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmstats"
)

type localSubtaskRunner struct {
	engine *Engine
}

func (r *localSubtaskRunner) RunSubtask(ctx context.Context, req SubtaskRequest) (*SubtaskResult, error) {
	if r == nil || r.engine == nil {
		return nil, fmt.Errorf("subtask runner is unavailable")
	}

	taskID, runCtx, meta := PrepareSubtaskContext(ctx, req.Meta)
	log := r.engine.log
	if log != nil {
		log.Info("subtask_start", "task_id", taskID, "mode", localSubtaskMode(req), "output_schema", strings.TrimSpace(req.OutputSchema))
	}

	var result *SubtaskResult
	if req.RunFunc != nil {
		directResult, err := req.RunFunc(runCtx)
		if err != nil {
			result = FailedSubtaskResult(taskID, err)
		} else {
			result = NormalizeDirectSubtaskResult(taskID, req.OutputSchema, directResult)
		}
	} else {
		result = r.runAgentSubtask(runCtx, meta, taskID, req)
	}

	if log != nil && result != nil {
		log.Info("subtask_done", "task_id", taskID, "status", result.Status, "output_kind", result.OutputKind)
	}
	return result, nil
}

func (r *localSubtaskRunner) runAgentSubtask(ctx context.Context, meta map[string]any, taskID string, req SubtaskRequest) *SubtaskResult {
	client := r.engine.client
	var cleanup func()
	if r.engine.subClientFactory != nil {
		client, cleanup = r.engine.subClientFactory("spawn")
	}
	if cleanup != nil {
		defer cleanup()
	}

	subOpts := []Option{WithLogger(r.engine.log)}
	if r.engine.guard != nil {
		subOpts = append(subOpts, WithGuard(r.engine.guard))
	}

	subEngine := New(client, req.Registry, Config{
		MaxSteps:        r.engine.config.MaxSteps,
		MaxTokenBudget:  r.engine.config.MaxTokenBudget,
		ParseRetries:    r.engine.config.ParseRetries,
		ToolRepeatLimit: r.engine.config.ToolRepeatLimit,
		DefaultModel:    req.resolvedModel(r.engine.config.DefaultModel),
		ToolCallTimeout: r.engine.config.ToolCallTimeout,
	}, r.engine.spec, subOpts...)

	final, _, err := subEngine.Run(ctx, BuildSubtaskTask(req.Task, req.OutputSchema), RunOptions{
		Model: req.resolvedModel(r.engine.config.DefaultModel),
		Scene: "spawn.subtask",
		Meta:  meta,
	})
	if err != nil {
		return FailedSubtaskResult(taskID, err)
	}
	return SubtaskResultFromFinal(taskID, req.OutputSchema, final)
}

func PrepareSubtaskContext(ctx context.Context, meta map[string]any) (string, context.Context, map[string]any) {
	taskID := llmstats.NewSyntheticRunID("sub")
	runCtx := llmstats.WithRunID(ctx, taskID)
	runCtx = WithSubtaskDepth(runCtx, SubtaskDepthFromContext(runCtx)+1)

	outMeta := cloneSubtaskMeta(meta)
	outMeta["trigger"] = "subtask.spawn"
	outMeta["subtask_task_id"] = taskID
	if parentRunID := strings.TrimSpace(llmstats.RunIDFromContext(ctx)); parentRunID != "" {
		outMeta["subtask_parent_run_id"] = parentRunID
	}
	return taskID, runCtx, outMeta
}

func cloneSubtaskMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func localSubtaskMode(req SubtaskRequest) string {
	if req.RunFunc != nil {
		return "direct"
	}
	return "agent"
}

func NormalizeDirectSubtaskResult(taskID string, outputSchema string, result *SubtaskResult) *SubtaskResult {
	if result == nil {
		return FailedSubtaskResult(taskID, fmt.Errorf("subtask returned nil result"))
	}
	out := *result
	if strings.TrimSpace(out.TaskID) == "" {
		out.TaskID = strings.TrimSpace(taskID)
	}
	if strings.TrimSpace(out.Status) == "" {
		out.Status = SubtaskStatusDone
	}
	if strings.TrimSpace(out.OutputKind) == "" {
		switch out.Output.(type) {
		case string:
			out.OutputKind = SubtaskOutputKindText
		default:
			out.OutputKind = SubtaskOutputKindJSON
		}
	}
	if strings.TrimSpace(out.OutputSchema) == "" && strings.TrimSpace(outputSchema) != "" && out.OutputKind == SubtaskOutputKindJSON {
		out.OutputSchema = strings.TrimSpace(outputSchema)
	}
	if strings.TrimSpace(out.Summary) == "" {
		if out.Status == SubtaskStatusFailed {
			out.Summary = "subtask failed"
		} else if out.OutputKind == SubtaskOutputKindJSON {
			out.Summary = "subtask completed with structured output"
		} else if s, ok := out.Output.(string); ok {
			out.Summary = summarizeSubtaskText(s)
		}
		if strings.TrimSpace(out.Summary) == "" && out.Status != SubtaskStatusFailed {
			out.Summary = "subtask completed"
		}
	}
	return &out
}

func (req SubtaskRequest) resolvedModel(defaultModel string) string {
	model := strings.TrimSpace(req.Model)
	if model != "" {
		return model
	}
	return strings.TrimSpace(defaultModel)
}
