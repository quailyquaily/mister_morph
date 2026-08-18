package awareness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/daemonruntime"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const cronTickInterval = time.Minute
const DefaultHeartbeatInterval = configdefaults.DefaultHeartbeatInterval

type CronLoopOptions struct {
	Logger      *slog.Logger
	Source      string
	Path        string
	SystemTasks []cronstore.Task
	Run         func(context.Context, cronstore.DueTask) error
	Now         func() time.Time
	Requests    <-chan CronRequest
}

type CronRequest struct {
	Task   cronstore.Task
	Result chan error
}

func TriggerCron(ctx context.Context, requests chan<- CronRequest, task cronstore.Task) error {
	if requests == nil {
		return fmt.Errorf("cron trigger is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cronstore.ValidateTask(task); err != nil {
		return err
	}
	req := CronRequest{
		Task:   task,
		Result: make(chan error, 1),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case requests <- req:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.Result:
		return err
	}
}

func RunCronLoop(ctx context.Context, opts CronLoopOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Run == nil || strings.TrimSpace(opts.Path) == "" {
		<-ctx.Done()
		return
	}
	r := &cronLoopRunner{
		opts:     opts,
		store:    cronstore.NewStore(opts.Path),
		queue:    make(chan cronstore.DueTask, 1024),
		inFlight: map[string]bool{},
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		r.worker(ctx)
	}()
	schedulerQuiesced := make(chan struct{})
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		r.runScheduler(ctx, workerDone, schedulerQuiesced)
	}()
	requests := opts.Requests
	for {
		select {
		case <-ctx.Done():
			<-schedulerQuiesced
			close(r.queue)
			<-workerDone
			<-schedulerDone
			return
		case req, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			req.Result <- r.enqueue(ctx, cronstore.DueTask{
				Task:           req.Task,
				ScheduledAtUTC: r.now().UTC(),
				Manual:         true,
			})
		}
	}
}

type cronLoopRunner struct {
	opts  CronLoopOptions
	store *cronstore.Store
	queue chan cronstore.DueTask

	mu       sync.Mutex
	inFlight map[string]bool
}

func (r *cronLoopRunner) runScheduler(ctx context.Context, workerDone <-chan struct{}, schedulerQuiesced chan<- struct{}) {
	quiesced := false
	markQuiesced := func() {
		if quiesced {
			return
		}
		close(schedulerQuiesced)
		quiesced = true
	}
	defer markQuiesced()

	lockPath, err := cronstore.SchedulerLockPath(r.opts.Path)
	if err != nil {
		r.warn("cron_scheduler_lock_error", "error", err.Error())
		return
	}
	err = fsstore.WithLock(ctx, lockPath, func() error {
		r.tick(ctx)
		ticker := time.NewTicker(cronTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				markQuiesced()
				<-workerDone
				return nil
			case <-ticker.C:
				r.tick(ctx)
			}
		}
	})
	if err != nil && ctx.Err() == nil {
		r.warn("cron_scheduler_lock_error", "error", err.Error())
	}
}

func (r *cronLoopRunner) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := r.now().UTC()
	due, taskErrs, err := r.store.DueLenient(now)
	if err != nil {
		r.warn("cron_tick_error", "error", err.Error())
		return
	}
	systemDue, systemTaskErrs := dueSystemTasks(r.opts.SystemTasks, now)
	due = append(systemDue, due...)
	taskErrs = append(taskErrs, systemTaskErrs...)
	for _, taskErr := range taskErrs {
		if taskErr != nil {
			r.warn("cron_task_invalid", "error", taskErr.Error())
		}
	}
	for _, item := range due {
		if err := r.enqueue(ctx, item); err != nil {
			id := strings.TrimSpace(item.Task.ID)
			if errors.Is(err, daemonruntime.ErrCronBusy) {
				r.debug("cron_skip", "task_id", id, "reason", "already_queued_or_running")
				continue
			}
			if strings.Contains(err.Error(), "queue is full") {
				r.warn("cron_skip", "task_id", id, "reason", "queue_full")
				continue
			}
			r.warn("cron_skip", "task_id", id, "reason", err.Error())
		}
	}
}

func (r *cronLoopRunner) enqueue(ctx context.Context, item cronstore.DueTask) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := cronstore.ValidateTask(item.Task); err != nil {
		return err
	}
	id := strings.TrimSpace(item.Task.ID)
	if id == "" {
		return fmt.Errorf("cron task id is required")
	}
	if !r.markInFlight(id) {
		return daemonruntime.ErrCronBusy
	}
	select {
	case r.queue <- item:
		return nil
	default:
		r.clearInFlight(id)
		return fmt.Errorf("cron queue is full")
	}
}

func (r *cronLoopRunner) now() time.Time {
	if r.opts.Now != nil {
		return r.opts.Now()
	}
	return time.Now()
}

func (r *cronLoopRunner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-r.queue:
			if !ok {
				return
			}
			id := strings.TrimSpace(item.Task.ID)
			err := r.opts.Run(ctx, item)
			if err != nil {
				r.warn("cron_task_error", "task_id", id, "error", err.Error())
			}
			r.clearInFlight(id)
		}
	}
}

func (r *cronLoopRunner) markInFlight(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight[id] {
		return false
	}
	r.inFlight[id] = true
	return true
}

func (r *cronLoopRunner) clearInFlight(id string) {
	r.mu.Lock()
	delete(r.inFlight, id)
	r.mu.Unlock()
}

func (r *cronLoopRunner) warn(msg string, args ...any) {
	if r.opts.Logger != nil {
		r.opts.Logger.Warn(msg, append([]any{"source", strings.TrimSpace(r.opts.Source)}, args...)...)
	}
}

func (r *cronLoopRunner) debug(msg string, args ...any) {
	if r.opts.Logger != nil {
		r.opts.Logger.Debug(msg, append([]any{"source", strings.TrimSpace(r.opts.Source)}, args...)...)
	}
}

func dueSystemTasks(tasks []cronstore.Task, now time.Time) ([]cronstore.DueTask, []error) {
	now = now.UTC()
	out := make([]cronstore.DueTask, 0, len(tasks))
	var errs []error
	seen := map[string]bool{}
	for _, task := range tasks {
		id := strings.TrimSpace(task.ID)
		if id == "" {
			errs = append(errs, fmt.Errorf("system cron task id is required"))
			continue
		}
		if seen[id] {
			errs = append(errs, fmt.Errorf("duplicate system cron task id: %s", id))
			continue
		}
		seen[id] = true
		if !cronstore.TaskEnabled(task) {
			continue
		}
		if err := cronstore.ValidateTask(task); err != nil {
			errs = append(errs, err)
			continue
		}
		due, scheduledAt, err := cronstore.IsDue(task, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if due {
			out = append(out, cronstore.DueTask{Task: task, ScheduledAtUTC: scheduledAt})
		}
	}
	return out, errs
}
