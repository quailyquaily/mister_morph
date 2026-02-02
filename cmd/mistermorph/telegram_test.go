package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTelegramWorkerIdleCleanup(t *testing.T) {
	// Simulate the worker lifecycle with context-based shutdown.
	// A cancelled worker exits cleanly and can be replaced by a fresh one.
	var processed atomic.Int64

	ctx1, cancel1 := context.WithCancel(context.Background())
	w := &telegramChatWorker{
		Jobs:   make(chan telegramJob, 16),
		ctx:    ctx1,
		cancel: cancel1,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-w.Jobs:
				processed.Add(1)
			case <-w.ctx.Done():
				return
			}
		}
	}()

	// Enqueue and let the worker process.
	w.Jobs <- telegramJob{ChatID: 1, Text: "msg1"}
	time.Sleep(20 * time.Millisecond)

	// Simulate idle cleanup: cancel the worker's context.
	w.cancel()
	wg.Wait()

	if got := processed.Load(); got != 1 {
		t.Fatalf("expected 1 processed job, got %d", got)
	}

	// Replacement worker starts and processes jobs correctly.
	ctx2, cancel2 := context.WithCancel(context.Background())
	w2 := &telegramChatWorker{
		Jobs:   make(chan telegramJob, 16),
		ctx:    ctx2,
		cancel: cancel2,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-w2.Jobs:
				processed.Add(1)
			case <-w2.ctx.Done():
				return
			}
		}
	}()

	w2.Jobs <- telegramJob{ChatID: 1, Text: "msg2"}
	time.Sleep(20 * time.Millisecond)
	cancel2()
	wg.Wait()

	if got := processed.Load(); got != 2 {
		t.Fatalf("expected 2 total processed jobs, got %d", got)
	}
}

func TestTelegramWorkerConcurrentEnqueueCancel(t *testing.T) {
	// Hammer the enqueue+cancel race to verify no panic or data race.
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		w := &telegramChatWorker{
			Jobs:   make(chan telegramJob, 4),
			ctx:    ctx,
			cancel: cancel,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-w.Jobs:
				case <-w.ctx.Done():
					return
				}
			}
		}()

		// Race: cancel and send happen concurrently.
		go cancel()

		select {
		case w.Jobs <- telegramJob{ChatID: int64(i), Text: "test"}:
		case <-w.ctx.Done():
		default:
		}

		wg.Wait()
	}
}
