package runtimeclock

import (
	"testing"
	"time"
)

func TestWithRuntimeClockMetaAddsLocalWeekday(t *testing.T) {
	now := time.Date(2026, 4, 15, 17, 56, 42, 0, time.FixedZone("JST", 9*60*60))

	meta := WithRuntimeClockMeta(map[string]any{
		"trigger": "ui",
	}, now)

	if got := meta["now_local_weekday"]; got != "Wednesday" {
		t.Fatalf("now_local_weekday = %#v, want %q", got, "Wednesday")
	}
	if got := meta["now_local"]; got != "2026-04-15T17:56:42+09:00" {
		t.Fatalf("now_local = %#v, want %q", got, "2026-04-15T17:56:42+09:00")
	}
	if got := meta["now_utc"]; got != "2026-04-15T08:56:42Z" {
		t.Fatalf("now_utc = %#v, want %q", got, "2026-04-15T08:56:42Z")
	}
}
