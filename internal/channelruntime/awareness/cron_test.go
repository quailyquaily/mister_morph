package awareness

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
)

func TestCronLoopRunnerSkipsAlreadyInFlightTask(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddRecurringWithChatID("", "Run task.", "* * * * *", "UTC", "task-a", ""); err != nil {
		t.Fatalf("seed cron task: %v", err)
	}
	r := &cronLoopRunner{
		opts:     CronLoopOptions{Path: cronPath},
		store:    store,
		queue:    make(chan cronstore.DueTask, 1),
		inFlight: map[string]bool{"task-a": true},
	}
	r.tick(context.Background())
	if len(r.queue) != 0 {
		t.Fatalf("queue len = %d, want 0 for in-flight task", len(r.queue))
	}
	r.clearInFlight("task-a")
	r.tick(context.Background())
	if len(r.queue) != 1 {
		t.Fatalf("queue len = %d, want 1 after clearing in-flight task", len(r.queue))
	}
}

func TestCronLoopRunnerIncludesDueSystemTasks(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	now := time.Date(2026, 5, 26, 10, 30, 15, 0, time.UTC)
	r := &cronLoopRunner{
		opts: CronLoopOptions{
			Path:        cronPath,
			SystemTasks: []cronstore.Task{cronstore.HeartbeatTask("*/30 * * * *")},
			Now:         func() time.Time { return now },
		},
		store:    cronstore.NewStore(cronPath),
		queue:    make(chan cronstore.DueTask, 1),
		inFlight: map[string]bool{},
	}

	r.tick(context.Background())

	if len(r.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(r.queue))
	}
	got := <-r.queue
	if got.Task.ID != cronstore.HeartbeatTaskID {
		t.Fatalf("task id = %q, want %q", got.Task.ID, cronstore.HeartbeatTaskID)
	}
	wantScheduledAt := now.Truncate(time.Minute)
	if !got.ScheduledAtUTC.Equal(wantScheduledAt) {
		t.Fatalf("scheduled_at = %s, want %s", got.ScheduledAtUTC, wantScheduledAt)
	}
}

func TestCronLoopRunnerEnqueuesManualCronRequest(t *testing.T) {
	r := &cronLoopRunner{
		queue:    make(chan cronstore.DueTask, 1),
		inFlight: map[string]bool{},
	}
	task := cronstore.Task{
		ID:      "manual-task",
		Cron:    "0 10 * * *",
		Content: "Run manually.",
	}

	if err := r.enqueue(context.Background(), cronstore.DueTask{
		Task:           task,
		ScheduledAtUTC: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
		Manual:         true,
	}); err != nil {
		t.Fatalf("enqueue() error = %v", err)
	}

	got := <-r.queue
	if got.Task.ID != task.ID || !got.Manual {
		t.Fatalf("queued item = %#v, want manual task", got)
	}
}

func TestRunCronLoopUsesSingleSchedulerOwnerPerPath(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddRecurringWithChatID("", "Run task.", "* * * * *", "UTC", "shared-task", ""); err != nil {
		t.Fatalf("seed cron task: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := make(chan string, 2)
	done := make(chan struct{}, 2)
	startLoop := func(source string) {
		go func() {
			defer func() { done <- struct{}{} }()
			RunCronLoop(ctx, CronLoopOptions{
				Path:   cronPath,
				Source: source,
				Now: func() time.Time {
					return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
				},
				Run: func(_ context.Context, task cronstore.DueTask) error {
					if !task.Manual {
						runs <- source
					}
					return nil
				},
			})
		}()
	}
	startLoop("one")
	startLoop("two")

	select {
	case <-runs:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scheduled task")
	}
	select {
	case source := <-runs:
		t.Fatalf("scheduled task ran twice; second source = %q", source)
	case <-time.After(250 * time.Millisecond):
	}

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for cron loop shutdown")
		}
	}
}

func TestRunCronLoopKeepsSchedulerLockUntilScheduledWorkerExits(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddRecurringWithChatID("", "Run task.", "* * * * *", "UTC", "shared-task", ""); err != nil {
		t.Fatalf("seed cron task: %v", err)
	}
	now := func() time.Time {
		return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	}

	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		RunCronLoop(ownerCtx, CronLoopOptions{
			Path: cronPath,
			Now:  now,
			Run: func(_ context.Context, task cronstore.DueTask) error {
				if !task.Manual {
					close(ownerStarted)
					<-releaseOwner
				}
				return nil
			},
		})
	}()
	select {
	case <-ownerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for owner task to start")
	}
	cancelOwner()

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	secondRun := make(chan struct{}, 1)
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		RunCronLoop(secondCtx, CronLoopOptions{
			Path: cronPath,
			Now:  now,
			Run: func(_ context.Context, task cronstore.DueTask) error {
				if !task.Manual {
					secondRun <- struct{}{}
				}
				return nil
			},
		})
	}()

	select {
	case <-secondRun:
		t.Fatal("second scheduler ran while the previous scheduler worker was still active")
	case <-time.After(250 * time.Millisecond):
	}

	close(releaseOwner)
	select {
	case <-ownerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for owner loop shutdown")
	}
	select {
	case <-secondRun:
	case <-time.After(2 * time.Second):
		t.Fatal("second scheduler did not run after the previous worker exited")
	}

	cancelSecond()
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second loop shutdown")
	}
}

func TestRunCronLoopNonOwnerHandlesManualRequestsAndCancellation(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddRecurringWithChatID("", "Run scheduled task.", "* * * * *", "UTC", "scheduled-task", ""); err != nil {
		t.Fatalf("seed cron task: %v", err)
	}

	ownerCtx, cancelOwner := context.WithCancel(context.Background())
	defer cancelOwner()
	ownerRequests := make(chan CronRequest)
	ownerScheduled := make(chan struct{}, 1)
	ownerManual := make(chan struct{}, 1)
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		RunCronLoop(ownerCtx, CronLoopOptions{
			Path:     cronPath,
			Requests: ownerRequests,
			Now: func() time.Time {
				return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
			},
			Run: func(_ context.Context, task cronstore.DueTask) error {
				if task.Manual {
					ownerManual <- struct{}{}
				} else {
					ownerScheduled <- struct{}{}
				}
				return nil
			},
		})
	}()
	select {
	case <-ownerScheduled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for owner to acquire scheduler lock")
	}

	nonOwnerCtx, cancelNonOwner := context.WithCancel(context.Background())
	nonOwnerRequests := make(chan CronRequest)
	nonOwnerManual := make(chan struct{}, 1)
	nonOwnerDone := make(chan struct{})
	go func() {
		defer close(nonOwnerDone)
		RunCronLoop(nonOwnerCtx, CronLoopOptions{
			Path:     cronPath,
			Requests: nonOwnerRequests,
			Run: func(_ context.Context, task cronstore.DueTask) error {
				if task.Manual {
					nonOwnerManual <- struct{}{}
				}
				return nil
			},
		})
	}()

	manualTask := cronstore.Task{ID: "manual-task", Cron: "0 10 * * *", Content: "Run manually."}
	manualCtx, cancelManual := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelManual()
	if err := TriggerCron(manualCtx, nonOwnerRequests, manualTask); err != nil {
		t.Fatalf("TriggerCron(non-owner) error = %v", err)
	}
	select {
	case <-nonOwnerManual:
	case <-time.After(2 * time.Second):
		t.Fatal("non-owner did not run manual task")
	}

	cancelNonOwner()
	select {
	case <-nonOwnerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("non-owner did not stop after cancellation")
	}

	if err := TriggerCron(manualCtx, ownerRequests, manualTask); err != nil {
		t.Fatalf("TriggerCron(owner after non-owner cancellation) error = %v", err)
	}
	select {
	case <-ownerManual:
	case <-time.After(2 * time.Second):
		t.Fatal("owner stopped handling manual tasks after non-owner cancellation")
	}

	cancelOwner()
	select {
	case <-ownerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not stop after cancellation")
	}
}

func TestHeartbeatIntervalCron(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     string
		wantOK   bool
	}{
		{name: "one minute", interval: time.Minute, want: "* * * * *", wantOK: true},
		{name: "five minutes", interval: 5 * time.Minute, want: "*/5 * * * *", wantOK: true},
		{name: "thirty minutes", interval: 30 * time.Minute, want: "*/30 * * * *", wantOK: true},
		{name: "one hour", interval: time.Hour, want: "0 * * * *", wantOK: true},
		{name: "six hours", interval: 6 * time.Hour, want: "0 */6 * * *", wantOK: true},
		{name: "one day", interval: 24 * time.Hour, want: "0 0 * * *", wantOK: true},
		{name: "forty five minutes", interval: 45 * time.Minute, wantOK: false},
		{name: "ninety minutes", interval: 90 * time.Minute, wantOK: false},
		{name: "twenty five hours", interval: 25 * time.Hour, wantOK: false},
		{name: "thirty seconds", interval: 30 * time.Second, wantOK: false},
		{name: "zero", interval: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cronstore.HeartbeatIntervalSchedule(tt.interval)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("cron = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeartbeatIntervalCronWithFallback(t *testing.T) {
	got, used, fallbackUsed, ok := cronstore.HeartbeatIntervalScheduleWithFallback(45*time.Minute, DefaultHeartbeatInterval)
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if got != "*/30 * * * *" {
		t.Fatalf("cron = %q, want */30 * * * *", got)
	}
	if used != DefaultHeartbeatInterval {
		t.Fatalf("used interval = %v, want %v", used, DefaultHeartbeatInterval)
	}
	if !fallbackUsed {
		t.Fatalf("fallbackUsed = false, want true")
	}

	_, _, _, ok = cronstore.HeartbeatIntervalScheduleWithFallback(45*time.Minute, 90*time.Minute)
	if ok {
		t.Fatalf("ok = true, want false for invalid fallback")
	}
}
