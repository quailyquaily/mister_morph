package line

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
)

const (
	lineRuntimeClosedTaskError = "line runtime closed"
	lineWorkerPanicTaskError   = "conversation worker panicked"
)

type lineRuntimeBusCloser interface {
	Close() error
}

type lineWebhookShutdowner interface {
	Shutdown(context.Context) error
}

type lineWebhookJoiner interface {
	lineWebhookShutdowner
	RegisterOnShutdown(func())
}

func newLineOwnedContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithCancel(context.WithoutCancel(parent))
}

func lineConversationRunnerOptions(logger *slog.Logger, store daemonruntime.TaskUpdater, runControl *runtimecontrol.RunControl) runtimecore.ConversationRunnerOptions[string, lineJob] {
	if logger == nil {
		logger = slog.Default()
	}
	return runtimecore.ConversationRunnerOptions[string, lineJob]{
		Logger: logger,
		OnDrop: func(_ string, job lineJob) {
			cancelLineRuntimeTask(logger, store, job)
			job.releaseGeneration()
		},
		OnPanic: func(conversationKey string, job lineJob) {
			defer job.releaseGeneration()
			taskID := strings.TrimSpace(job.TaskID)
			if runControl != nil && taskID != "" {
				runControl.Finish("line", conversationKey, taskID)
			}
			if store == nil || taskID == "" {
				return
			}
			finishedAt := time.Now().UTC()
			if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
				if info == nil || (info.Status != daemonruntime.TaskQueued && info.Status != daemonruntime.TaskRunning) {
					return
				}
				info.Status = daemonruntime.TaskFailed
				info.Error = lineWorkerPanicTaskError
				info.FinishedAt = &finishedAt
			}); err != nil {
				logger.Error("line_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskFailed, "error", err.Error())
			}
		},
	}
}

func cancelLineTaskOnWorkerShutdown(workerCtx context.Context, logger *slog.Logger, store daemonruntime.TaskUpdater, job lineJob) bool {
	if workerCtx == nil || workerCtx.Err() == nil {
		return false
	}
	cancelLineRuntimeTask(logger, store, job)
	return true
}

func cancelLineRuntimeTask(logger *slog.Logger, store daemonruntime.TaskUpdater, job lineJob) {
	taskID := strings.TrimSpace(job.TaskID)
	if store == nil || taskID == "" {
		return
	}
	finishedAt := time.Now().UTC()
	if err := store.Update(taskID, func(info *daemonruntime.TaskInfo) {
		if info == nil || (info.Status != daemonruntime.TaskQueued && info.Status != daemonruntime.TaskRunning) {
			return
		}
		info.Status = daemonruntime.TaskCanceled
		info.Error = lineRuntimeClosedTaskError
		info.FinishedAt = &finishedAt
	}); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("line_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskCanceled, "error", err.Error())
	}
}

func shutdownLineRuntime(daemonServer lineWebhookShutdowner, stopDaemonServer context.CancelFunc, bus lineRuntimeBusCloser, stopWorkers context.CancelFunc, runner *runtimecore.ConversationRunner[string, lineJob]) {
	if daemonServer != nil {
		_ = daemonServer.Shutdown(context.Background())
	}
	if stopDaemonServer != nil {
		stopDaemonServer()
	}
	if stopWorkers != nil {
		stopWorkers()
	}
	if bus != nil {
		_ = bus.Close()
	}
	if runner != nil {
		runner.WaitClosed()
	}
}

func stopAndWaitLineWebhook(server lineWebhookJoiner, serveDone <-chan error, stopWorkers context.CancelFunc) error {
	var workersStopped chan struct{}
	if server != nil && stopWorkers != nil {
		workersStopped = make(chan struct{})
		server.RegisterOnShutdown(func() {
			stopWorkers()
			close(workersStopped)
		})
	}
	var shutdownErr error
	if server != nil {
		shutdownErr = server.Shutdown(context.Background())
	} else if stopWorkers != nil {
		stopWorkers()
	}
	if workersStopped != nil {
		<-workersStopped
	}
	var serveErr error
	if serveDone != nil {
		serveErr = <-serveDone
	}
	return errors.Join(shutdownErr, serveErr)
}
