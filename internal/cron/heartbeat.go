package cron

import (
	"fmt"
	"strings"
	"time"
)

const HeartbeatTaskID = "__heartbeat__"

func HeartbeatTask(schedule string) Task {
	return Task{
		ID:      HeartbeatTaskID,
		Title:   "Heartbeat",
		Cron:    strings.TrimSpace(schedule),
		Content: "Run heartbeat checklist.",
	}
}

func HeartbeatIntervalSchedule(interval time.Duration) (string, bool) {
	if interval <= 0 {
		return "", false
	}
	if interval < time.Hour {
		if interval%time.Minute != 0 {
			return "", false
		}
		minutes := int(interval / time.Minute)
		if minutes <= 0 || 60%minutes != 0 {
			return "", false
		}
		if minutes == 1 {
			return "* * * * *", true
		}
		return fmt.Sprintf("*/%d * * * *", minutes), true
	}
	if interval%time.Hour != 0 {
		return "", false
	}
	hours := int(interval / time.Hour)
	if hours <= 0 || 24%hours != 0 {
		return "", false
	}
	switch hours {
	case 1:
		return "0 * * * *", true
	case 24:
		return "0 0 * * *", true
	default:
		return fmt.Sprintf("0 */%d * * *", hours), true
	}
}

func HeartbeatIntervalScheduleWithFallback(interval, fallback time.Duration) (schedule string, used time.Duration, fallbackUsed bool, ok bool) {
	if schedule, ok := HeartbeatIntervalSchedule(interval); ok {
		return schedule, interval, false, true
	}
	if schedule, ok := HeartbeatIntervalSchedule(fallback); ok {
		return schedule, fallback, true, true
	}
	return "", 0, false, false
}
