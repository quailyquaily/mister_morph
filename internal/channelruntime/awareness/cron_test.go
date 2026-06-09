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
