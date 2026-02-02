package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mister_morph/guard"
	"github.com/quailyquaily/mister_morph/secrets"
)

func (e *Engine) Resume(ctx context.Context, approvalRequestID string) (*Final, *Context, error) {
	if e == nil || e.guard == nil || !e.guard.Enabled() {
		return nil, nil, fmt.Errorf("guard is not enabled")
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

	ctx = secrets.WithSkillAuthProfilePolicy(ctx, rs.SkillAuthProfiles, rs.EnforceSkillAuth)

	// Verify action hash binding.
	h, err := guard.ActionHash(guard.Action{
		Type:       guard.ActionToolCallPre,
		ToolName:   rs.PendingTool.ToolCall.Name,
		ToolParams: rs.PendingTool.ToolCall.Params,
	})
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(rec.ActionHash) != "" && strings.TrimSpace(rec.ActionHash) != h {
		return nil, nil, fmt.Errorf("approval action_hash mismatch (expected %s)", rec.ActionHash)
	}

	agentCtx := contextFromSnapshot(rs.AgentCtx)
	log := e.log.With("run_id", rs.RunID, "model", rs.Model)

	return e.runLoop(ctx, &engineLoopState{
		runID:               rs.RunID,
		model:               rs.Model,
		log:                 log,
		messages:            rs.Messages,
		agentCtx:            agentCtx,
		extraParams:         rs.ExtraParams,
		planRequired:        rs.PlanRequired,
		parseFailures:       rs.ParseFailures,
		requestedWrites:     ExtractFileWritePaths(agentCtx.Task),
		pendingTool:         &rs.PendingTool,
		approvedPendingTool: true,
		nextStep:            rs.Step,
	})
}
