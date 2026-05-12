package cron

import (
	"path/filepath"
	"strings"
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

func TestStoreDeleteByID(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cron.yaml"))
	if _, err := store.AddRecurringWithChatID("Review invoices.", "0 10 * * 1", "UTC", "invoice-review", ""); err != nil {
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
