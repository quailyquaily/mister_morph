package processsignal

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

type terminationParentKey struct{}

// NotifyContext returns the process command context. SIGINT and SIGTERM cancel
// the returned context, while InteractiveParent can recover the parent that is
// canceled only by SIGTERM or by the caller.
func NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	terminationCtx, stopTermination := signal.NotifyContext(parent, syscall.SIGTERM)
	commandCtx, stopInterrupt := signal.NotifyContext(terminationCtx, os.Interrupt)
	commandCtx = withTerminationParent(commandCtx, terminationCtx)
	return commandCtx, func() {
		stopInterrupt()
		stopTermination()
	}
}

// InteractiveParent preserves caller cancellation and SIGTERM while leaving
// SIGINT to an interactive command's own terminal handling.
func InteractiveParent(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if parent, ok := ctx.Value(terminationParentKey{}).(context.Context); ok && parent != nil {
		return parent
	}
	return ctx
}

func withTerminationParent(ctx context.Context, parent context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if parent == nil {
		parent = ctx
	}
	return context.WithValue(ctx, terminationParentKey{}, parent)
}
