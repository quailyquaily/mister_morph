package processsignal

import (
	"context"
	"testing"
)

func TestInteractiveParentIgnoresInterruptContextButKeepsTerminationParent(t *testing.T) {
	interruptCtx, cancelInterrupt := context.WithCancel(context.Background())
	terminationCtx, cancelTermination := context.WithCancel(context.Background())
	ctx := withTerminationParent(interruptCtx, terminationCtx)
	interactive := InteractiveParent(ctx)

	cancelInterrupt()
	select {
	case <-interactive.Done():
		t.Fatalf("interactive parent canceled with interrupt context: %v", interactive.Err())
	default:
	}

	cancelTermination()
	<-interactive.Done()
	if interactive.Err() != context.Canceled {
		t.Fatalf("interactive parent error = %v, want context canceled", interactive.Err())
	}
}

func TestInteractiveParentKeepsOrdinaryCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	interactive := InteractiveParent(ctx)
	cancel()

	<-interactive.Done()
	if interactive.Err() != context.Canceled {
		t.Fatalf("interactive parent error = %v, want caller cancellation", interactive.Err())
	}
}
