package telegram

import (
	"context"
	"fmt"
	"log/slog"
)

type Hooks struct {
	OnInbound  func(context.Context, InboundEvent)
	OnOutbound func(context.Context, OutboundEvent)
	OnError    func(context.Context, ErrorEvent)
}

type ErrorStage string

const (
	ErrorStageRunTask                  ErrorStage = "run_task"
	ErrorStagePublishOutbound          ErrorStage = "publish_outbound"
	ErrorStagePublishErrorReply        ErrorStage = "publish_error_reply"
	ErrorStagePublishFileDownloadError ErrorStage = "publish_file_download_error"
	ErrorStagePublishInbound           ErrorStage = "publish_inbound"
	ErrorStageDeliverOutbound          ErrorStage = "deliver_outbound"
)

type InboundEvent struct {
	ChatID          int64
	MessageThreadID int64
	MessageID       int64
	ChatType        string
	FromUserID      int64
	Text            string
	MentionUsers    []string
}

type OutboundEvent struct {
	ChatID           int64
	MessageThreadID  int64
	ReplyToMessageID int64
	Text             string
	CorrelationID    string
	Kind             string
}

type ErrorEvent struct {
	Stage     ErrorStage
	ChatID    int64
	MessageID int64
	Err       error
}

func callInboundHook(ctx context.Context, logger *slog.Logger, hooks Hooks, event InboundEvent) {
	if hooks.OnInbound == nil {
		return
	}
	callHookSafely(ctx, logger, "on_inbound", func(hookCtx context.Context) {
		hooks.OnInbound(hookCtx, event)
	})
}

func callOutboundHook(ctx context.Context, logger *slog.Logger, hooks Hooks, event OutboundEvent) {
	if hooks.OnOutbound == nil {
		return
	}
	callHookSafely(ctx, logger, "on_outbound", func(hookCtx context.Context) {
		hooks.OnOutbound(hookCtx, event)
	})
}

func callErrorHook(ctx context.Context, logger *slog.Logger, hooks Hooks, event ErrorEvent) {
	if event.Err == nil || hooks.OnError == nil {
		return
	}
	callHookSafely(ctx, logger, "on_error", func(hookCtx context.Context) {
		hooks.OnError(hookCtx, event)
	})
}

func callHookSafely(ctx context.Context, logger *slog.Logger, hookName string, fn func(context.Context)) {
	if fn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if r := recover(); r != nil && logger != nil {
			logger.Warn("telegram_hook_panic", "hook", hookName, "panic", fmt.Sprint(r))
		}
	}()
	fn(ctx)
}
