package cron

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExpressionMatchesStandardDOMDOWOr(t *testing.T) {
	expr, err := ParseExpression("0 9 1 * 1")
	if err != nil {
		t.Fatalf("ParseExpression() error = %v", err)
	}
	// Monday, not the first day of month.
	if !expr.Matches(time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected day-of-week match")
	}
	// First day of month, not Monday.
	if !expr.Matches(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected day-of-month match")
	}
	if expr.Matches(time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected non-match")
	}
}

func TestExpressionTreatsLiteralStarAsUnrestricted(t *testing.T) {
	expr, err := ParseExpression("0 9 1 * *")
	if err != nil {
		t.Fatalf("ParseExpression() error = %v", err)
	}
	if !expr.Matches(time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected first day of month match")
	}
	if expr.Matches(time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected non-match when only day-of-month is restricted")
	}
}

func TestParseExpressionRejectsNames(t *testing.T) {
	_, err := ParseExpression("0 9 * * MON")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "numeric") {
		t.Fatalf("expected numeric-only error, got %v", err)
	}
}

func TestIsDueOnceUsesTaskTimezone(t *testing.T) {
	task := Task{
		ID:      "tokyo-once",
		At:      "2026-05-12 09:00",
		TZ:      "Asia/Tokyo",
		Content: "Run task.",
	}
	now := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	due, scheduledAt, err := IsDue(task, now)
	if err != nil {
		t.Fatalf("IsDue() error = %v", err)
	}
	if !due {
		t.Fatalf("expected due")
	}
	if got := scheduledAt.Format(time.RFC3339); got != "2026-05-12T00:00:00Z" {
		t.Fatalf("scheduledAt = %s", got)
	}
}

func TestIsDueOnceUsesUTCOffsetTimezone(t *testing.T) {
	task := Task{
		ID:      "utc-plus-eight",
		At:      "2026-05-12 09:00",
		TZ:      "UTC+8",
		Content: "Run task.",
	}
	now := time.Date(2026, 5, 12, 1, 0, 0, 0, time.UTC)
	due, scheduledAt, err := IsDue(task, now)
	if err != nil {
		t.Fatalf("IsDue() error = %v", err)
	}
	if !due {
		t.Fatalf("expected due")
	}
	if got := scheduledAt.Format(time.RFC3339); got != "2026-05-12T01:00:00Z" {
		t.Fatalf("scheduledAt = %s", got)
	}
}

func TestDueTasksSkipsDisabledTasks(t *testing.T) {
	disabled := false
	file := File{
		Version: Version,
		Tasks: []Task{
			{
				ID:      "disabled",
				Cron:    "* * * * *",
				Content: "Skip this task.",
				Enabled: &disabled,
			},
			{
				ID:      "enabled",
				Cron:    "* * * * *",
				Content: "Run this task.",
			},
		},
	}

	due, err := DueTasks(file, time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DueTasks() error = %v", err)
	}
	if len(due) != 1 || due[0].Task.ID != "enabled" {
		t.Fatalf("due = %#v, want only enabled task", due)
	}
}

func TestStoreDeleteByID(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cron.yaml"))
	if _, err := store.AddRecurringWithChatID("", "Review invoices.", "0 10 * * 1", "UTC", "invoice-review", ""); err != nil {
		t.Fatalf("AddRecurringWithChatID() error = %v", err)
	}
	if _, err := store.DeleteByID("invoice-review"); err != nil {
		t.Fatalf("DeleteByID() error = %v", err)
	}
	file, _, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(file.Tasks) != 0 {
		t.Fatalf("tasks = %#v, want empty", file.Tasks)
	}
}

func TestStoreRoundTripPreservesLLMProfile(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cron.yaml"))
	want := Task{
		ID:         "weekly-report",
		Cron:       "0 10 * * 1",
		Content:    "Prepare weekly report.",
		LLMProfile: "batch",
	}
	if err := store.Write(File{Version: Version, Tasks: []Task{want}}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	file, _, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("tasks = %#v, want one task", file.Tasks)
	}
	if got := file.Tasks[0].LLMProfile; got != want.LLMProfile {
		t.Fatalf("llm_profile = %q, want %q", got, want.LLMProfile)
	}
}

func TestStoreConcurrentAddsAcrossInstancesDoNotLoseTasks(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	const taskCount = 32
	start := make(chan struct{})
	errs := make(chan error, taskCount)
	var wg sync.WaitGroup
	for i := 0; i < taskCount; i++ {
		id := fmt.Sprintf("task-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewStore(cronPath).AddRecurringWithChatID("", "Run "+id, "* * * * *", "UTC", id, "")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent add: %v", err)
		}
	}

	file, _, err := NewStore(cronPath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(file.Tasks) != taskCount {
		t.Fatalf("task count = %d, want %d", len(file.Tasks), taskCount)
	}
	got := make(map[string]bool, taskCount)
	for _, task := range file.Tasks {
		got[task.ID] = true
	}
	for i := 0; i < taskCount; i++ {
		id := fmt.Sprintf("task-%02d", i)
		if !got[id] {
			t.Errorf("missing task %q", id)
		}
	}
}

func TestStoreConcurrentAddsAndDeletesAcrossInstancesDoNotLoseUpdates(t *testing.T) {
	cronPath := filepath.Join(t.TempDir(), "cron.yaml")
	seed := NewStore(cronPath)
	const pairCount = 16
	for i := 0; i < pairCount; i++ {
		id := fmt.Sprintf("old-%02d", i)
		if _, err := seed.AddOnceWithChatID("", "Run "+id, "2027-01-01 00:00", "UTC", id, ""); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	start := make(chan struct{})
	errs := make(chan error, pairCount*2)
	var wg sync.WaitGroup
	for i := 0; i < pairCount; i++ {
		oldID := fmt.Sprintf("old-%02d", i)
		newID := fmt.Sprintf("new-%02d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewStore(cronPath).DeleteByID(oldID)
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := NewStore(cronPath).AddRecurringWithChatID("", "Run "+newID, "* * * * *", "UTC", newID, "")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent mutation: %v", err)
		}
	}

	file, _, err := NewStore(cronPath).Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(file.Tasks) != pairCount {
		t.Fatalf("task count = %d, want %d", len(file.Tasks), pairCount)
	}
	got := make(map[string]bool, pairCount)
	for _, task := range file.Tasks {
		got[task.ID] = true
	}
	for i := 0; i < pairCount; i++ {
		oldID := fmt.Sprintf("old-%02d", i)
		newID := fmt.Sprintf("new-%02d", i)
		if got[oldID] {
			t.Errorf("deleted task %q is still present", oldID)
		}
		if !got[newID] {
			t.Errorf("missing added task %q", newID)
		}
	}
}
