package taskruntime

import (
	"context"
	"log/slog"
	"strings"

	"github.com/quailyquaily/mistermorph/agent"
)

const ContextCompactionDoneText = "上下文已压缩，正在继续处理当前任务。"

type ContextCompactionNotifyFunc func(context.Context, agent.Event, string) error

func WithContextCompactionNotification(ctx context.Context, logger *slog.Logger, notify ContextCompactionNotifyFunc) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if notify == nil {
		return ctx
	}
	baseSink, _ := agent.EventSinkFromContext(ctx)
	if logger == nil {
		logger = slog.Default()
	}
	sink := agent.EventSinkFunc(func(deliveryCtx context.Context, event agent.Event) {
		if baseSink != nil {
			baseSink.HandleEvent(deliveryCtx, event)
		}
		if strings.TrimSpace(event.Kind) != agent.EventKindContextCompactionDone {
			return
		}
		if strings.TrimSpace(event.Reason) == agent.ContextCompactionReasonManual {
			return
		}
		if err := notify(deliveryCtx, event, ContextCompactionDoneText); err != nil {
			logger.Warn("context_compaction_notification_failed", "error", err.Error())
		}
	})
	return agent.WithEventSinkContext(ctx, sink)
}
