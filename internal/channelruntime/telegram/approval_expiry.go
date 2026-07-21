package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/quailyquaily/mistermorph/guard"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
)

func newTelegramPendingApprovalRegistry(g *guard.Guard, store daemonruntime.TaskUpdater, logger *slog.Logger) *runtimecore.PendingApprovalRegistry[telegramJob] {
	if logger == nil {
		logger = slog.Default()
	}
	var registry *runtimecore.PendingApprovalRegistry[telegramJob]
	registry = runtimecore.NewPendingApprovalRegistry(func(claim runtimecore.PendingApprovalClaim[telegramJob]) {
		approvalID := claim.ID
		job := claim.Job
		err := runtimecore.ExpirePendingApproval(context.Background(), g, store, approvalID, job.TaskID, "telegram:expiry")
		if errors.Is(err, runtimecore.ErrApprovalCommitIndeterminate) || errors.Is(err, runtimecore.ErrApprovalTaskFinalizationFailed) {
			restoreErr := registry.RestoreClaim(claim, time.Now().Add(runtimecore.PendingApprovalRetryDelay))
			if restoreErr == nil {
				logger.Warn("telegram_approval_expiry_retry", "approval_request_id", approvalID, "task_id", job.TaskID, "error", err.Error())
				return
			}
			_, finalizeErr := finalizeTelegramPendingApproval(store, approvalID, job, runtimecore.ApprovalExpiryResolutionFailedTaskError)
			err = errors.Join(err, restoreErr, finalizeErr)
		}
		if err != nil && !errors.Is(err, guard.ErrApprovalNotPending) {
			logger.Error("telegram_approval_expiry_error", "approval_request_id", approvalID, "task_id", job.TaskID, "error", err.Error())
		}
	})
	return registry
}

func finalizeTelegramPendingApproval(store daemonruntime.TaskUpdater, approvalID string, job telegramJob, taskError string) (bool, error) {
	applied, err := runtimecore.FailPendingApprovalTask(store, job.TaskID, approvalID, taskError)
	if err != nil {
		return applied, fmt.Errorf("finalize Telegram approval %q task %q: %w", approvalID, job.TaskID, err)
	}
	return applied, nil
}
