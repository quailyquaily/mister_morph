package runtimeclock

import (
	"time"
)

func WithRuntimeClockMeta(meta map[string]any, now time.Time) map[string]any {
	out := make(map[string]any, len(meta)+3)
	for k, v := range meta {
		out[k] = v
	}

	out["now_utc"] = now.UTC().Format(time.RFC3339)
	out["now_local"] = now.Format(time.RFC3339)
	out["now_local_weekday"] = now.Weekday().String()
	return out
}
