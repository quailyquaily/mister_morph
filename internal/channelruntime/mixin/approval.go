package mixin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

const mixinRuntimeClosedApprovalError = "Mixin runtime closed while approval was pending. Task failed."

type mixinApprovalManager struct {
	bus         *busruntime.Inproc
	store       daemonruntime.TaskView
	workersCtx  context.Context
	logger      *slog.Logger
	runner      *runtimecore.ConversationRunner[string, mixinJob]
	pending     *runtimecore.PendingApprovalRegistry[mixinJob]
	generations *runtimecore.RuntimeGenerationManager
}

func newMixinApprovalManager(bus *busruntime.Inproc, store daemonruntime.TaskView, generations *runtimecore.RuntimeGenerationManager, workersCtx context.Context, logger *slog.Logger) *mixinApprovalManager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &mixinApprovalManager{bus: bus, store: store, generations: generations, workersCtx: workersCtx, logger: logger}
	m.pending = runtimecore.NewPendingApprovalRegistry(func(claim runtimecore.PendingApprovalClaim[mixinJob]) {
		job := claim.Job
		retainGeneration := false
		defer func() {
			if !retainGeneration {
				job.releaseGeneration()
			}
		}()
		err := runtimecore.ExpirePendingApproval(context.Background(), job.approvalGuard(), store, claim.ID, job.TaskID, "mixin:expiry")
		if errors.Is(err, runtimecore.ErrApprovalCommitIndeterminate) || errors.Is(err, runtimecore.ErrApprovalTaskFinalizationFailed) {
			if restoreErr := m.pending.RestoreClaim(claim, time.Now().Add(runtimecore.PendingApprovalRetryDelay)); restoreErr == nil {
				retainGeneration = true
				logger.Warn("mixin_approval_expiry_retry", "approval_request_id", claim.ID, "task_id", job.TaskID, "error", err.Error())
				return
			}
		}
		if err != nil && !errors.Is(err, guard.ErrApprovalNotPending) {
			logger.Error("mixin_approval_expiry_error", "approval_request_id", claim.ID, "task_id", job.TaskID, "error", err.Error())
		}
	})
	return m
}

func (m *mixinApprovalManager) listApprovals(ctx context.Context, req daemonruntime.ApprovalListRequest) (daemonruntime.ApprovalListResponse, error) {
	lease, bundle, err := m.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalListResponse{}, err
	}
	defer lease.Release()
	return runtimecore.ListPendingApprovals(ctx, m.store, bundle.TaskRuntime.SharedGuard, req, "mixin")
}

func (m *mixinApprovalManager) getApproval(ctx context.Context, approvalID string) (daemonruntime.ApprovalInfo, bool, error) {
	lease, bundle, err := m.captureRuntimeGeneration()
	if err != nil {
		return daemonruntime.ApprovalInfo{}, false, err
	}
	defer lease.Release()
	return runtimecore.GetApprovalInfo(ctx, bundle.TaskRuntime.SharedGuard, approvalID, "mixin")
}

func (m *mixinApprovalManager) captureRuntimeGeneration() (*runtimecore.RuntimeGenerationLease, *runtimecore.ChannelRuntimeBundle, error) {
	if m == nil || m.generations == nil {
		return nil, nil, fmt.Errorf("mixin runtime generation is unavailable")
	}
	lease, err := m.generations.Capture()
	if err != nil {
		return nil, nil, err
	}
	bundle := lease.Bundle()
	if bundle == nil || bundle.TaskRuntime == nil {
		lease.Release()
		return nil, nil, fmt.Errorf("mixin runtime generation is unavailable")
	}
	return lease, bundle, nil
}

func (m *mixinApprovalManager) approve(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	taskID, resumed, err := m.apply(ctx, req.ApprovalRequestID, true, strings.TrimSpace(req.Actor), nil)
	if err != nil {
		if taskID != "" {
			return daemonruntime.ApprovalDecisionResponse{
				ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID), TaskID: taskID,
				Status: string(guard.ApprovalApproved), Error: strings.TrimSpace(err.Error()),
			}, nil
		}
		return daemonruntime.ApprovalDecisionResponse{}, err
	}
	return daemonruntime.ApprovalDecisionResponse{
		ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID), TaskID: taskID,
		Status: string(guard.ApprovalApproved), Resumed: resumed,
	}, nil
}

func (m *mixinApprovalManager) deny(ctx context.Context, req daemonruntime.ApprovalDecisionRequest) (daemonruntime.ApprovalDecisionResponse, error) {
	taskID, resumed, err := m.apply(ctx, req.ApprovalRequestID, false, strings.TrimSpace(req.Actor), nil)
	if err != nil {
		return daemonruntime.ApprovalDecisionResponse{}, err
	}
	return daemonruntime.ApprovalDecisionResponse{
		ApprovalRequestID: strings.TrimSpace(req.ApprovalRequestID), TaskID: taskID,
		Status: string(guard.ApprovalDenied), Resumed: resumed,
	}, nil
}

func parseMixinApprovalCommand(text string) (approvalID string, approved bool, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 {
		return "", false, false
	}
	switch strings.ToLower(fields[0]) {
	case "/approve":
		return fields[1], true, true
	case "/deny":
		return fields[1], false, true
	default:
		return "", false, false
	}
}

func mixinApprovalRequestText(rec guard.ApprovalRecord) string {
	parts := []string{"Approval required"}
	if toolName := strings.TrimSpace(rec.ToolName); toolName != "" {
		parts = append(parts, "Tool: "+toolName)
	}
	if len(rec.Reasons) > 0 {
		parts = append(parts, "Reasons:")
		for _, reason := range rec.Reasons {
			if reason = strings.TrimSpace(reason); reason != "" {
				parts = append(parts, "- "+reason)
			}
		}
	}
	if summary := strings.TrimSpace(rec.ActionSummaryRedacted); summary != "" {
		parts = append(parts, "Action: "+summary)
	}
	params := runtimecore.ApprovalToolParams(rec)
	if len(params) > 0 {
		parts = append(parts, "Parameters:")
		keys := make([]string, 0, len(params))
		for key := range params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s: %v", key, params[key]))
		}
	}
	if id := strings.TrimSpace(rec.ID); id != "" {
		parts = append(parts, "", "/approve "+id, "/deny "+id)
	}
	return strings.Join(parts, "\n")
}

func mixinApprovalResultText(approved bool) string {
	if approved {
		return "Approved. Resuming task."
	}
	return "Approval denied. Task canceled."
}

func (m *mixinApprovalManager) register(approvalID string, job mixinJob) error {
	if m == nil || m.pending == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	g := job.approvalGuard()
	if g == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	rec, found, err := g.GetApproval(context.Background(), strings.TrimSpace(approvalID))
	if err != nil {
		return err
	}
	if !found {
		return guard.ErrApprovalNotFound
	}
	displaced, replaced, err := m.pending.Register(rec.ID, job, rec.ExpiresAt)
	if replaced {
		displaced.releaseGeneration()
	}
	return err
}

func (m *mixinApprovalManager) notify(ctx context.Context, approvalID string, job mixinJob) error {
	g := job.approvalGuard()
	if g == nil {
		return fmt.Errorf("approvals are unavailable")
	}
	rec, found, err := g.GetApproval(ctx, strings.TrimSpace(approvalID))
	if err != nil {
		return err
	}
	if !found {
		return guard.ErrApprovalNotFound
	}
	_, err = publishMixinBusOutbound(ctx, m.bus, job.ConversationID, mixinReplyRecipient(job.ChatType, job.FromUserID), mixinApprovalRequestText(rec), job.MessageID, "mixin:approval:"+rec.ID)
	return err
}

func (m *mixinApprovalManager) apply(ctx context.Context, approvalID string, approved bool, actor string, authorize func(mixinJob) bool) (string, bool, error) {
	if m == nil || m.pending == nil {
		return "", false, fmt.Errorf("approvals are unavailable")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return "", false, daemonruntime.BadRequest("approval_request_id is required")
	}
	claim, state, err := m.pending.Claim(approvalID)
	if err != nil {
		return "", false, err
	}
	if state == runtimecore.PendingApprovalClaimInFlight {
		return claim.Job.TaskID, false, runtimecore.ErrPendingApprovalClaimInFlight
	}
	if state == runtimecore.PendingApprovalClaimMissing {
		return "", false, daemonruntime.BadRequest("approval is not pending")
	}
	job := claim.Job
	retainGeneration := false
	defer func() {
		if !retainGeneration {
			job.releaseGeneration()
		}
	}()
	defer m.pending.CompleteClaim(claim)

	g := job.approvalGuard()
	if g == nil {
		return job.TaskID, false, fmt.Errorf("approvals are unavailable")
	}
	rec, found, err := g.GetApproval(ctx, approvalID)
	if err != nil || !found {
		if err == nil {
			err = guard.ErrApprovalNotFound
		}
		return job.TaskID, false, err
	}
	if authorize != nil && !authorize(job) {
		if restoreErr := m.pending.RestoreClaim(claim, rec.ExpiresAt); restoreErr != nil {
			return job.TaskID, false, errors.Join(daemonruntime.BadRequest("approval sender is not authorized"), restoreErr)
		}
		retainGeneration = true
		return job.TaskID, false, daemonruntime.BadRequest("approval sender is not authorized")
	}
	if rec.Status != guard.ApprovalPending {
		return job.TaskID, false, daemonruntime.BadRequest("approval is not pending")
	}
	if !rec.ExpiresAt.IsZero() && time.Now().UTC().After(rec.ExpiresAt) {
		_ = runtimecore.ExpirePendingApproval(ctx, g, m.store, approvalID, job.TaskID, "mixin:expiry")
		return job.TaskID, false, daemonruntime.BadRequest("approval is expired")
	}
	status := guard.ApprovalDenied
	if approved {
		status = guard.ApprovalApproved
	}
	commit, pendingRec, resolveErr := runtimecore.ResolveApprovalCommit(ctx, g, approvalID, status, strings.TrimSpace(actor), "")
	if resolveErr != nil {
		if commit == runtimecore.ApprovalCommitPending {
			if restoreErr := m.pending.RestoreClaim(claim, pendingRec.ExpiresAt); restoreErr == nil {
				retainGeneration = true
				return job.TaskID, false, resolveErr
			}
		}
		_, _ = runtimecore.FailPendingApprovalTask(m.store, job.TaskID, approvalID, "approval resume failed: "+resolveErr.Error())
		return job.TaskID, false, resolveErr
	}
	if !approved {
		finishedAt := time.Now().UTC()
		err := m.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
			info.Status = daemonruntime.TaskCanceled
			info.Error = mixinApprovalResultText(false)
			info.FinishedAt = &finishedAt
			runtimecore.ClearTaskPendingApprovalFields(info)
			info.ApprovalRequestID = approvalID
		})
		return job.TaskID, false, err
	}
	if m.runner == nil {
		_, _ = runtimecore.FailPendingApprovalTask(m.store, job.TaskID, approvalID, "approval resume failed: runner unavailable")
		return job.TaskID, false, fmt.Errorf("approval runner is unavailable")
	}
	job.ResumeApprovalID = approvalID
	resumedAt := time.Now().UTC()
	if err := m.store.Update(job.TaskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskQueued
		info.Error = ""
		info.ResumedAt = &resumedAt
		runtimecore.ClearTaskPendingApprovalFields(info)
	}); err != nil {
		return job.TaskID, false, err
	}
	if err := m.runner.Enqueue(m.workersCtx, job.ConversationKey, func(version uint64) mixinJob {
		job.Version = version
		return job
	}); err != nil {
		_, _ = runtimecore.FailPendingApprovalTask(m.store, job.TaskID, approvalID, "approval resume failed: "+err.Error())
		return job.TaskID, false, err
	}
	retainGeneration = true
	return job.TaskID, true, nil
}

func (m *mixinApprovalManager) close() {
	if m == nil || m.pending == nil {
		return
	}
	for _, handle := range m.pending.Close() {
		_, _ = runtimecore.FailPendingApprovalTask(m.store, handle.Job.TaskID, handle.ID, mixinRuntimeClosedApprovalError)
		handle.Job.releaseGeneration()
	}
}
