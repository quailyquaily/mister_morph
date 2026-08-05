package lark

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"time"

	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/runtimecontrol"
)

const (
	larkRuntimeClosedTaskError = "lark runtime closed"
	larkWorkerPanicTaskError   = "conversation worker panicked"
)

type larkRuntimeBusCloser interface {
	Close() error
}

type larkDaemonShutdowner interface {
	Shutdown(context.Context) error
}

func newLarkOwnedContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithCancel(context.WithoutCancel(parent))
}

func larkConversationRunnerOptions(logger *slog.Logger, store daemonruntime.TaskUpdater, runControl *runtimecontrol.RunControl) runtimecore.ConversationRunnerOptions[string, larkJob] {
	if logger == nil {
		logger = slog.Default()
	}
	return runtimecore.ConversationRunnerOptions[string, larkJob]{
		Logger: logger,
		OnDrop: func(_ string, job larkJob) {
			cancelLarkRuntimeTask(logger, store, job)
			job.releaseGeneration()
		},
		OnPanic: func(conversationKey string, job larkJob) {
			defer job.releaseGeneration()
			taskID := strings.TrimSpace(job.TaskID)
			if runControl != nil && taskID != "" {
				runControl.Finish("lark", conversationKey, taskID)
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
				info.Error = larkWorkerPanicTaskError
				info.FinishedAt = &finishedAt
			}); err != nil {
				logger.Error("lark_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskFailed, "error", err.Error())
			}
		},
	}
}

func cancelLarkTaskOnWorkerShutdown(workerCtx context.Context, logger *slog.Logger, store daemonruntime.TaskUpdater, job larkJob) bool {
	if workerCtx == nil || workerCtx.Err() == nil {
		return false
	}
	cancelLarkRuntimeTask(logger, store, job)
	return true
}

func cancelLarkRuntimeTask(logger *slog.Logger, store daemonruntime.TaskUpdater, job larkJob) {
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
		info.Error = larkRuntimeClosedTaskError
		info.FinishedAt = &finishedAt
	}); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("lark_task_state_write_error", "task_id", taskID, "status", daemonruntime.TaskCanceled, "error", err.Error())
	}
}

func shutdownLarkRuntime(daemonServer larkDaemonShutdowner, stopDaemonServer context.CancelFunc, bus larkRuntimeBusCloser, stopWorkers context.CancelFunc, runner *runtimecore.ConversationRunner[string, larkJob]) {
	if !isNilLarkDaemonShutdowner(daemonServer) {
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

func isNilLarkDaemonShutdowner(daemonServer larkDaemonShutdowner) bool {
	if daemonServer == nil {
		return true
	}
	value := reflect.ValueOf(daemonServer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
