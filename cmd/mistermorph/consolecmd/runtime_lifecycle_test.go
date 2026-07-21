package consolecmd

import (
	"context"
	"log/slog"
	"testing"
	"time"

	busruntime "github.com/quailyquaily/mistermorph/internal/bus"
	runtimecore "github.com/quailyquaily/mistermorph/internal/channelruntime/core"
)

func TestConsoleLocalRuntimeCloseStopsExecutionBeforeDrainingBus(t *testing.T) {
	state := newConsoleExecutionState(nil, nil)
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	state.runner = runtimecore.NewConversationRunner[string, consoleLocalTaskJob](
		state.workersCtx,
		sem,
		1,
		func(context.Context, string, consoleLocalTaskJob) {},
		runtimecore.ConversationRunnerOptions[string, consoleLocalTaskJob]{},
	)
	if err := state.runner.Enqueue(context.Background(), "topic", func(uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{TaskID: "running"}
	}); err != nil {
		t.Fatalf("enqueue running job: %v", err)
	}
	if err := state.runner.Enqueue(context.Background(), "topic", func(uint64) consoleLocalTaskJob {
		return consoleLocalTaskJob{TaskID: "queued"}
	}); err != nil {
		t.Fatalf("enqueue queued job: %v", err)
	}

	bus, err := busruntime.NewInproc(busruntime.InprocOptions{MaxInFlight: 1, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewInproc() error = %v", err)
	}
	handlerStarted := make(chan struct{})
	if err := bus.Subscribe(busruntime.TopicChatMessage, func(ctx context.Context, _ busruntime.BusMessage) error {
		close(handlerStarted)
		return state.runner.Enqueue(ctx, "topic", func(uint64) consoleLocalTaskJob {
			return consoleLocalTaskJob{TaskID: "blocked_bus_delivery"}
		})
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := bus.Publish(context.Background(), busruntime.BusMessage{
		Topic:           busruntime.TopicChatMessage,
		ConversationKey: "topic",
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		state.close()
		_ = bus.Close()
		t.Fatal("bus handler did not start")
	}

	runtime := &consoleLocalRuntime{bus: bus, consoleExecutionState: state}
	closeDone := make(chan struct{})
	go func() {
		runtime.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		return
	case <-time.After(250 * time.Millisecond):
		// Unblock the old shutdown order so the failed test does not leak goroutines.
		state.close()
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() remained blocked after execution cancellation")
	}
	t.Fatal("Close() drained the bus before stopping task execution")
}

func TestConsoleLocalRuntimeCloseWaitsForAwarenessLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(canceled)
		<-release
		close(done)
	}()
	runtime := &consoleLocalRuntime{
		consoleExecutionState: newConsoleExecutionState(nil, nil),
		awarenessCancel:       cancel,
		awarenessDone:         done,
	}
	closeDone := make(chan struct{})
	go func() {
		runtime.Close()
		close(closeDone)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel awareness")
	}
	returnedEarly := false
	select {
	case <-closeDone:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after awareness exited")
	}
	if returnedEarly {
		t.Error("Close() returned before awareness exited")
	}
}

func TestConsoleReloadAwarenessWaitsForPreviousLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(canceled)
		<-release
		close(done)
	}()
	runtime := &consoleLocalRuntime{
		consoleExecutionState: newConsoleExecutionState(nil, nil),
		awarenessCancel:       cancel,
		awarenessDone:         done,
	}
	t.Cleanup(func() { runtime.consoleExecutionState.close() })
	reloadDone := make(chan struct{})
	go func() {
		runtime.reloadAwarenessLoop()
		close(reloadDone)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("reload did not cancel previous awareness")
	}
	returnedEarly := false
	select {
	case <-reloadDone:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-reloadDone:
	case <-time.After(time.Second):
		t.Fatal("reload did not return after previous awareness exited")
	}
	if returnedEarly {
		t.Error("reload returned before previous awareness exited")
	}
}
