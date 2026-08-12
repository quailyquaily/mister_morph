package core

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/guard"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

// ApprovalToolParams returns the exact invocation bound to this approval record.
func ApprovalToolParams(rec guard.ApprovalRecord) map[string]any {
	pending, ok := agent.PendingApprovalToolFromResumeState(rec.ResumeState)
	if !ok || !strings.EqualFold(pending.Name, strings.TrimSpace(rec.ToolName)) {
		return nil
	}
	return pending.Params
}

// GetApprovalInfo returns the stored details for one approval without scanning tasks.
func GetApprovalInfo(ctx context.Context, g *guard.Guard, approvalID, runtimeName string) (daemonruntime.ApprovalInfo, bool, error) {
	if g == nil {
		return daemonruntime.ApprovalInfo{}, false, daemonruntime.BadRequest("approvals are unavailable")
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return daemonruntime.ApprovalInfo{}, false, daemonruntime.BadRequest("approval_request_id is required")
	}
	rec, ok, err := g.GetApproval(ctx, approvalID)
	if err != nil || !ok {
		return daemonruntime.ApprovalInfo{}, ok, err
	}
	return daemonruntime.ApprovalInfo{
		ApprovalRequestID:     approvalID,
		TaskID:                strings.TrimSpace(rec.RunID),
		RunID:                 strings.TrimSpace(rec.RunID),
		Status:                string(rec.Status),
		ToolName:              strings.TrimSpace(rec.ToolName),
		ToolParams:            ApprovalToolParams(rec),
		ActionSummaryRedacted: strings.TrimSpace(rec.ActionSummaryRedacted),
		Reasons:               append([]string(nil), rec.Reasons...),
		Runtime:               strings.TrimSpace(runtimeName),
		CreatedAt:             rec.CreatedAt,
		ExpiresAt:             rec.ExpiresAt,
	}, true, nil
}

func ListPendingApprovals(ctx context.Context, store daemonruntime.TaskReader, g *guard.Guard, req daemonruntime.ApprovalListRequest, runtimeName string) (daemonruntime.ApprovalListResponse, error) {
	if store == nil {
		return daemonruntime.ApprovalListResponse{}, daemonruntime.BadRequest("task store is unavailable")
	}
	if g == nil {
		return daemonruntime.ApprovalListResponse{}, daemonruntime.BadRequest("approvals are unavailable")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = string(daemonruntime.TaskPending)
	}
	if !strings.EqualFold(status, string(daemonruntime.TaskPending)) {
		return daemonruntime.ApprovalListResponse{}, daemonruntime.BadRequest("invalid status")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	tasks := store.List(daemonruntime.TaskListOptions{
		Status: daemonruntime.TaskPending,
		Limit:  limit,
	})
	now := time.Now().UTC()
	items := make([]daemonruntime.ApprovalInfo, 0, len(tasks))
	for _, task := range tasks {
		approvalID := strings.TrimSpace(task.ApprovalRequestID)
		if approvalID == "" {
			continue
		}
		rec, ok, err := g.GetApproval(ctx, approvalID)
		if err != nil {
			return daemonruntime.ApprovalListResponse{}, err
		}
		if !ok || rec.Status != guard.ApprovalPending || (!rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt)) {
			continue
		}
		items = append(items, daemonruntime.ApprovalInfo{
			ApprovalRequestID:     approvalID,
			TaskID:                task.ID,
			RunID:                 rec.RunID,
			Status:                string(rec.Status),
			ToolName:              rec.ToolName,
			ToolParams:            ApprovalToolParams(rec),
			ActionSummaryRedacted: rec.ActionSummaryRedacted,
			Reasons:               append([]string(nil), rec.Reasons...),
			Runtime:               strings.TrimSpace(runtimeName),
			TopicID:               task.TopicID,
			CreatedAt:             rec.CreatedAt,
			ExpiresAt:             rec.ExpiresAt,
			PendingAt:             task.PendingAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i].CreatedAt
		right := items[j].CreatedAt
		if items[i].PendingAt != nil {
			left = *items[i].PendingAt
		}
		if items[j].PendingAt != nil {
			right = *items[j].PendingAt
		}
		return left.After(right)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return daemonruntime.ApprovalListResponse{
		Items: items,
		Limit: limit,
	}, nil
}
