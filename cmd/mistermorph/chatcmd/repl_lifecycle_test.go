package chatcmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestRunREPLReturnsWhenRootContextIsCanceled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rootCtx, cancel := context.WithCancel(context.Background())
	inputReader, inputWriter := io.Pipe()
	cmd := New(Dependencies{})
	cmd.SetIn(inputReader)
	cmd.SetOut(io.Discard)
	sess := &chatSession{
		cmd:         cmd,
		rootContext: rootCtx,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan error, 1)
	go func() {
		done <- runREPL(sess)
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runREPL() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		_ = inputWriter.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("runREPL() did not return after root context cancellation")
	}
	_ = inputWriter.Close()
}

func TestCancelAndWaitActiveChatTurnWaitsForResultCleanup(t *testing.T) {
	turnCtx, cancelTurn := context.WithCancelCause(context.Background())
	active := &activeChatTurn{cancel: cancelTurn}
	resultCh := make(chan chatTurnResult)
	cleanupDone := make(chan struct{})
	go func() {
		<-turnCtx.Done()
		close(cleanupDone)
		resultCh <- chatTurnResult{turn: active, cause: context.Cause(turnCtx)}
	}()

	returned := make(chan struct{})
	go func() {
		cancelAndWaitActiveChatTurn(active, resultCh)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("cancelAndWaitActiveChatTurn() did not return")
	}
	select {
	case <-cleanupDone:
	default:
		t.Fatal("cancelAndWaitActiveChatTurn() returned before result cleanup")
	}
}
