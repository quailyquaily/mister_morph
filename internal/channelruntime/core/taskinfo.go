package core

import (
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/pagination"
	"github.com/quailyquaily/mistermorph/internal/textutil"
)

const defaultOutputSummaryLimit = 4000
const pendingApprovalTaskPageLimit = 200

func TaskIDForPendingApproval(store daemonruntime.TaskReader, approvalID string) string {
	if store == nil {
		return ""
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return ""
	}
	cursor := ""
	for {
		items := store.List(daemonruntime.TaskListOptions{
			Status: daemonruntime.TaskPending,
			Limit:  pendingApprovalTaskPageLimit,
			Cursor: cursor,
		})
		for _, item := range items {
			if strings.TrimSpace(item.ApprovalRequestID) == approvalID {
				return strings.TrimSpace(item.ID)
			}
		}
		if len(items) < pendingApprovalTaskPageLimit {
			return ""
		}
		last := items[len(items)-1]
		nextCursor := pagination.EncodeKeysetCursor(last.CreatedAt, last.ID)
		if nextCursor == "" || nextCursor == cursor {
			return ""
		}
		cursor = nextCursor
	}
}

func MarkTaskRunning(store daemonruntime.TaskUpdater, taskID string) error {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	startedAt := time.Now().UTC()
	return store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskRunning
		info.StartedAt = &startedAt
	})
}

func MarkTaskFailed(store daemonruntime.TaskUpdater, taskID string, displayErr string, canceled bool) error {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	finishedAt := time.Now().UTC()
	status := daemonruntime.TaskFailed
	if canceled {
		status = daemonruntime.TaskCanceled
	}
	return store.Update(taskID, func(info *daemonruntime.TaskInfo) {
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

func MarkTaskDone(store daemonruntime.TaskUpdater, taskID string, output string) error {
	if store == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	finishedAt := time.Now().UTC()
	summary := textutil.TruncateRunes(strings.TrimSpace(output), defaultOutputSummaryLimit)
	return store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		info.Status = daemonruntime.TaskDone
		info.Error = ""
		info.FinishedAt = &finishedAt
		info.Result = map[string]any{
			"output": summary,
		}
	})
}
