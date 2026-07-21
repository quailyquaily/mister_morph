package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/llmstats"
	"github.com/quailyquaily/mistermorph/llm"
)

func (e *Engine) Resume(ctx context.Context, approvalRequestID string) (*Final, *Context, error) {
	return e.resume(ctx, approvalRequestID, RunOptions{})
}

func (e *Engine) ResumeWithOptions(ctx context.Context, approvalRequestID string, opts RunOptions) (*Final, *Context, error) {
	return e.resume(ctx, approvalRequestID, opts)
}

func (e *Engine) resume(ctx context.Context, approvalRequestID string, opts RunOptions) (*Final, *Context, error) {
	if e == nil || e.guard == nil || !e.guard.Enabled() {
		return nil, nil, fmt.Errorf("guard is not enabled")
	}
	if err := e.config.ContextCompaction.Validate(); err != nil {
		return nil, nil, err
	}
	id := strings.TrimSpace(approvalRequestID)
	if id == "" {
		return nil, nil, fmt.Errorf("missing approval_request_id")
	}

	rec, ok, err := e.guard.GetApproval(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("approval not found: %s", id)
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		return nil, nil, fmt.Errorf("approval is expired: %s", id)
	}
	if rec.Status != guard.ApprovalApproved {
		return &Final{
			Output: PendingOutput{
				Status:            "pending",
				ApprovalRequestID: id,
				Message:           fmt.Sprintf("Approval is not approved yet (status=%s).", rec.Status),
			},
		}, nil, nil
	}
	if len(rec.ResumeState) == 0 {
		return nil, nil, fmt.Errorf("approval has no resume_state: %s", id)
	}

	rs, err := unmarshalResumeState(rec.ResumeState)
	if err != nil {
		return nil, nil, err
	}
	if rs.Version != 0 && rs.Version != 1 {
		return nil, nil, fmt.Errorf("unsupported resume_state version: %d", rs.Version)
	}

	ctx = llmstats.WithRunID(ctx, rs.RunID)

	// Verify action hash binding.
	h, err := guard.ActionHash(guard.Action{
		Type:       guard.ActionToolCallPre,
		Identity:   rs.PendingTool.ApprovalIdentity,
		ToolName:   rs.PendingTool.ToolCall.Name,
		ToolParams: rs.PendingTool.ToolCall.Params,
	})
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(rec.ActionHash) == "" {
		return nil, nil, fmt.Errorf("approval has no action_hash: %s", id)
	}
	if strings.TrimSpace(rec.ActionHash) != h {
		return nil, nil, fmt.Errorf("approval action_hash mismatch (expected %s)", rec.ActionHash)
	}

	agentCtx := contextFromSnapshot(rs.AgentCtx)
	log := e.log.With("run_id", rs.RunID, "model", rs.Model)
	toolLog := e.log.With("run_id", rs.RunID)
	checkpointStore := opts.ContextCheckpointStore
	checkpoint := rs.Checkpoint
	hasCheckpoint := rs.HasCheckpoint
	if checkpointStore == nil {
		localStore := newRunLocalCheckpointStore()
		if hasCheckpoint {
			cloned := cloneContextCheckpoint(checkpoint)
			localStore.checkpoint = &cloned
		}
		checkpointStore = localStore
	} else {
		loaded, found, loadErr := checkpointStore.Load(ctx)
		if loadErr != nil {
			return nil, agentCtx, fmt.Errorf("load context checkpoint: %w", loadErr)
		}
		if found != hasCheckpoint || (found && loaded.Revision != checkpoint.Revision) {
			return nil, agentCtx, fmt.Errorf("%w: checkpoint changed while approval was pending", ErrContextCheckpointRevisionConflict)
		}
		if found {
			checkpoint = loaded
		}
	}
	contextWindowTokens := opts.ContextWindowTokens
	if contextWindowTokens <= 0 {
		contextWindowTokens = rs.ContextWindowTokens
	}
	if contextWindowTokens <= 0 {
		if entry, ok := llm.ResolveModelContextWindow(rs.Model); ok {
			contextWindowTokens = entry.ContextWindowTokens
		}
	}
	fixedMessageCount := rs.FixedMessageCount
	if fixedMessageCount <= 0 && len(rs.Messages) > 0 && normalizedMessageRole(rs.Messages[0].Role) == "system" {
		fixedMessageCount = 1
	}
	if opts.SteerSource != nil {
		defer opts.SteerSource.Close()
	}
	if err := ctx.Err(); err != nil {
		return nil, agentCtx, err
	}
	if _, err := e.guard.ConsumeApproval(ctx, id); err != nil {
		return nil, agentCtx, err
	}

	return e.runLoop(ctx, &engineLoopState{
		runID:                   rs.RunID,
		model:                   rs.Model,
		scene:                   rs.Scene,
		log:                     log,
		toolLog:                 toolLog,
		messages:                rs.Messages,
		agentCtx:                agentCtx,
		extraParams:             rs.ExtraParams,
		tools:                   buildLLMTools(e.registry),
		planRequired:            rs.PlanRequired,
		onStream:                opts.OnStream,
		steerSource:             opts.SteerSource,
		parseFailures:           rs.ParseFailures,
		requestedWrites:         ExtractFileWritePaths(agentCtx.Task),
		pendingTool:             &rs.PendingTool,
		approvedActionIdentity:  rs.PendingTool.ApprovalIdentity,
		nextStep:                rs.Step,
		fixedMessageCount:       fixedMessageCount,
		messageBoundaries:       cloneMessageBoundaries(rs.MessageBoundaries),
		checkpointStore:         checkpointStore,
		checkpoint:              checkpoint,
		hasCheckpoint:           hasCheckpoint,
		contextCompaction:       resolveContextCompactionConfig(e.config.ContextCompaction, opts.DisableContextCompaction),
		contextWindowTokens:     contextWindowTokens,
		protectedMessageIndexes: cloneIndexSet(rs.ProtectedMessageIndexes),
		lastMainInputTokens:     rs.LastMainInputTokens,
		lastMainMessageCount:    rs.LastMainMessageCount,
		hasLastMainInputTokens:  rs.HasLastMainInputTokens,
	})
}

func cloneIndexSet(indexes map[int]struct{}) map[int]struct{} {
	if len(indexes) == 0 {
		return nil
	}
	out := make(map[int]struct{}, len(indexes))
	for index := range indexes {
		out[index] = struct{}{}
	}
	return out
}

func cloneMessageBoundaries(boundaries map[int]string) map[int]string {
	if len(boundaries) == 0 {
		return make(map[int]string)
	}
	out := make(map[int]string, len(boundaries))
	for index, boundary := range boundaries {
		out[index] = boundary
	}
	return out
}
