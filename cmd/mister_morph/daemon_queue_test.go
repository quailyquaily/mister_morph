package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTaskStore_NextReturnsOnClose(t *testing.T) {
	store := NewTaskStore(10)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		qt, ok := store.Next()
		if ok {
			t.Errorf("expected ok=false after Close, got ok=true (qt=%v)", qt)
		}
	}()

	// Give the goroutine time to block on Next().
	time.Sleep(50 * time.Millisecond)
	store.Close()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Worker exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after Close()")
	}
}

func TestTaskStore_CloseIsIdempotent(t *testing.T) {
	store := NewTaskStore(10)
	store.Close()
	store.Close() // must not panic
}

func TestTaskStore_EnqueueAfterCloseReturnsError(t *testing.T) {
	store := NewTaskStore(10)
	store.Close()
	_, err := store.Enqueue(context.Background(), "task", "model", time.Minute)
	if err == nil {
		t.Fatal("expected error on Enqueue after Close, got nil")
	}
}

func TestTaskStore_CloseCancelsInFlightTasks(t *testing.T) {
	store := NewTaskStore(10)
	info, err := store.Enqueue(context.Background(), "task", "model", 5*time.Minute)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Retrieve the queued task to get its context.
	qt, ok := store.Next()
	if !ok {
		t.Fatal("expected task from Next()")
	}
	if qt.info.ID != info.ID {
		t.Fatalf("expected task ID %s, got %s", info.ID, qt.info.ID)
	}

	// Context should not be cancelled yet.
	select {
	case <-qt.ctx.Done():
		t.Fatal("context cancelled before Close()")
	default:
	}

	store.Close()

	// Context should now be cancelled.
	select {
	case <-qt.ctx.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("context not cancelled after Close()")
	}
}

func TestTaskStore_EvictExpired(t *testing.T) {
	store := NewTaskStore(10)
	defer store.Close()

	// Use a very short TTL for testing.
	store.completedTTL = 10 * time.Millisecond

	info, err := store.Enqueue(context.Background(), "task", "model", time.Minute)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Drain the queue.
	store.Next()

	// Mark the task as done.
	finished := time.Now().Add(-1 * time.Second) // finished 1s ago
	store.Update(info.ID, func(i *TaskInfo) {
		i.Status = TaskDone
		i.FinishedAt = &finished
	})

	// Task should still be visible before eviction runs.
	if _, ok := store.Get(info.ID); !ok {
		t.Fatal("expected task to still be visible before eviction")
	}

	// Run eviction.
	store.evictExpired()

	// Task should be gone.
	if _, ok := store.Get(info.ID); ok {
		t.Fatal("expected task to be evicted after TTL")
	}
}

func TestTaskStore_EvictKeepsRunningTasks(t *testing.T) {
	store := NewTaskStore(10)
	defer store.Close()

	store.completedTTL = 10 * time.Millisecond

	info, err := store.Enqueue(context.Background(), "task", "model", time.Minute)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Drain the queue and mark as running.
	store.Next()
	store.Update(info.ID, func(i *TaskInfo) {
		i.Status = TaskRunning
	})

	// Run eviction — running tasks must not be evicted.
	store.evictExpired()

	if _, ok := store.Get(info.ID); !ok {
		t.Fatal("running task was incorrectly evicted")
	}
}
