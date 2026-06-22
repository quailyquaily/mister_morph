package core

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

const defaultOutputSummaryLimit = 4000

func TaskIDForPendingApproval(store daemonruntime.TaskReader, approvalID string) string {
	if store == nil {
		return ""
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return ""
	}
	items := store.List(daemonruntime.TaskListOptions{
		Status: daemonruntime.TaskPending,
		Limit:  200,
	})
	for _, item := range items {
		if strings.TrimSpace(item.ApprovalRequestID) == approvalID {
			return strings.TrimSpace(item.ID)
		}
	}
	return ""
}

func MarkTaskRunning(store daemonruntime.TaskUpdater, taskID string) {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	startedAt := time.Now().UTC()
	store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskRunning
		info.StartedAt = &startedAt
	})
}

func MarkTaskFailed(store daemonruntime.TaskUpdater, taskID string, displayErr string, canceled bool) {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	finishedAt := time.Now().UTC()
	status := daemonruntime.TaskFailed
	if canceled {
		status = daemonruntime.TaskCanceled
	}
	store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = status
		info.Error = strings.TrimSpace(displayErr)
		info.FinishedAt = &finishedAt
	})
}

func ClearTaskPendingApprovalFields(info *daemonruntime.TaskInfo) {
	if info == nil {
		return
	}
	info.PendingAt = nil
	info.ApprovalRequestID = ""
	info.Result = nil
}

func MarkTaskDone(store daemonruntime.TaskUpdater, taskID string, output string) {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	finishedAt := time.Now().UTC()
	summary := daemonruntime.TruncateUTF8(strings.TrimSpace(output), defaultOutputSummaryLimit)
	store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = map[string]any{
			"output": summary,
		}
	})
}
