package awareness

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/internal/configdefaults"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
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
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.worker(ctx)
	}()
	r.tick(ctx)
	ticker := time.NewTicker(cronTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(r.queue)
			wg.Wait()
			return
		case <-ticker.C:
			r.tick(ctx)
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

func (r *cronLoopRunner) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	now := time.Now
	if r.opts.Now != nil {
		now = r.opts.Now
	}
	due, taskErrs, err := r.store.DueLenient(now().UTC())
	if err != nil {
		r.warn("cron_tick_error", "error", err.Error())
		return
	}
	systemDue, systemTaskErrs := dueSystemTasks(r.opts.SystemTasks, now().UTC())
	due = append(systemDue, due...)
	taskErrs = append(taskErrs, systemTaskErrs...)
	for _, taskErr := range taskErrs {
		if taskErr != nil {
			r.warn("cron_task_invalid", "error", taskErr.Error())
		}
	}
	for _, item := range due {
		id := strings.TrimSpace(item.Task.ID)
		if id == "" {
			continue
		}
		if !r.markInFlight(id) {
			r.debug("cron_skip", "task_id", id, "reason", "already_queued_or_running")
			continue
		}
		select {
		case r.queue <- item:
		default:
			r.clearInFlight(id)
			r.warn("cron_skip", "task_id", id, "reason", "queue_full")
		}
	}
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
