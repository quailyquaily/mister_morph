package awareness

import (
	"context"
	"log/slog"
	"strings"

	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
)

// Notifier is an optional adapter for delivering awareness messages.
// The payload is intentionally minimal to keep awareness runtime decoupled
// from transport-specific concepts.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

// NotifyFunc adapts a function into Notifier.
type NotifyFunc func(ctx context.Context, text string) error

func (f NotifyFunc) Notify(ctx context.Context, text string) error {
	if f == nil {
		return nil
	}
	return f(ctx, text)
}

type CronNotification struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type CronNotifyFunc func(ctx context.Context, notification CronNotification) error

func notifyCronResult(ctx context.Context, notify CronNotifyFunc, logger *slog.Logger, task cronstore.Task, runID, summary string, runErr error) {
	if notify == nil || !cronstore.IsConsoleNotificationChatID(task.ChatID) {
		return
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = cronstore.DefaultTaskTitle
	}
	notification := CronNotification{
		ID:    strings.TrimSpace(runID),
		Title: title,
		Body:  strings.TrimSpace(summary),
	}
	if runErr != nil {
		notification.Body = strings.TrimSpace(runErr.Error())
	}
	if err := notify(ctx, notification); err != nil && logger != nil {
		logger.Warn("cron_notify_error", "task_id", strings.TrimSpace(task.ID), "error", err.Error())
	}
}
