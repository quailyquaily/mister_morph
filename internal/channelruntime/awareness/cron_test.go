package awareness

import (
	"context"
	"path/filepath"
	"testing"

	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
)

func TestCronLoopRunnerSkipsAlreadyInFlightTask(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddRecurringWithChatID("Run task.", "* * * * *", "UTC", "task-a", ""); err != nil {
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
