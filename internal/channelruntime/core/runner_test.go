package core

import (
	"context"
	"testing"
	"time"
)

type runnerTestJob struct {
	Version uint64
	Value   string
}

func TestConversationRunnerEnqueueUsesCurrentVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sem := make(chan struct{}, 1)
	handled := make(chan runnerTestJob, 2)
	r := NewConversationRunner[string, runnerTestJob](
		ctx,
		sem,
		4,
		func(_ context.Context, _ string, job runnerTestJob) {
			handled <- job
		},
	)

	if err := r.Enqueue(context.Background(), "conv:a", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "first"}
	}); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	r.IncrementVersion("conv:a")
	if err := r.Enqueue(context.Background(), "conv:a", func(version uint64) runnerTestJob {
		return runnerTestJob{Version: version, Value: "second"}
	}); err != nil {
		t.Fatalf("enqueue second: %v", err)
	}

	first := readRunnerJob(t, handled)
	second := readRunnerJob(t, handled)
	if first.Value != "first" || first.Version != 0 {
		t.Fatalf("first job = %#v, want value=first version=0", first)
	}
	if second.Value != "second" || second.Version != 1 {
		t.Fatalf("second job = %#v, want value=second version=1", second)
	}
}

func TestConversationRunnerCurrentVersionDefault(t *testing.T) {
	r := NewConversationRunner[string, runnerTestJob](
		context.Background(),
		make(chan struct{}, 1),
		2,
		nil,
	)
	if got := r.CurrentVersion("missing"); got != 0 {
		t.Fatalf("current version = %d, want 0", got)
	}
}

func TestConversationRunnerEnqueueRequiresBuilder(t *testing.T) {
	r := NewConversationRunner[string, runnerTestJob](
		context.Background(),
		make(chan struct{}, 1),
		2,
		nil,
	)
	if err := r.Enqueue(context.Background(), "k", nil); err == nil {
		t.Fatalf("enqueue(nil builder) should fail")
	}
}

func readRunnerJob(t *testing.T, ch <-chan runnerTestJob) runnerTestJob {
	t.Helper()
	select {
	case job := <-ch:
		return job
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for runner job")
		return runnerTestJob{}
	}
}
