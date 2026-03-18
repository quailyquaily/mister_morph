package core

import (
	"context"
	"fmt"
	"sync"

	runtimeworker "github.com/quailyquaily/mistermorph/internal/channelruntime/worker"
)

const defaultQueueSize = 16

type conversationWorker[J any] struct {
	jobs    chan J
	version uint64
}

type ConversationRunner[K comparable, J any] struct {
	workersCtx context.Context
	sem        chan struct{}
	queueSize  int
	handle     func(context.Context, K, J)

	mu      sync.Mutex
	workers map[K]*conversationWorker[J]
}

func NewConversationRunner[K comparable, J any](
	workersCtx context.Context,
	sem chan struct{},
	queueSize int,
	handle func(context.Context, K, J),
) *ConversationRunner[K, J] {
	if workersCtx == nil {
		workersCtx = context.Background()
	}
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	return &ConversationRunner[K, J]{
		workersCtx: workersCtx,
		sem:        sem,
		queueSize:  queueSize,
		handle:     handle,
		workers:    make(map[K]*conversationWorker[J]),
	}
}

func (r *ConversationRunner[K, J]) Enqueue(ctx context.Context, key K, buildJob func(version uint64) J) error {
	if buildJob == nil {
		return fmt.Errorf("build job callback is required")
	}
	w, version := r.ensureWorkerWithVersion(key)
	job := buildJob(version)
	return runtimeworker.Enqueue(ctx, r.workersCtx, w.jobs, job)
}

func (r *ConversationRunner[K, J]) CurrentVersion(key K) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentVersionLocked(key)
}

func (r *ConversationRunner[K, J]) IncrementVersion(key K) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.ensureWorkerLocked(key)
	w.version++
	return w.version
}

func (r *ConversationRunner[K, J]) ensureWorkerWithVersion(key K) (*conversationWorker[J], uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.ensureWorkerLocked(key)
	return w, w.version
}

func (r *ConversationRunner[K, J]) currentVersionLocked(key K) uint64 {
	w, ok := r.workers[key]
	if !ok || w == nil {
		return 0
	}
	return w.version
}

func (r *ConversationRunner[K, J]) ensureWorkerLocked(key K) *conversationWorker[J] {
	if w, ok := r.workers[key]; ok && w != nil {
		return w
	}
	w := &conversationWorker[J]{jobs: make(chan J, r.queueSize)}
	r.workers[key] = w
	runtimeworker.Start(runtimeworker.StartOptions[J]{
		Ctx:  r.workersCtx,
		Sem:  r.sem,
		Jobs: w.jobs,
		Handle: func(ctx context.Context, job J) {
			if r.handle != nil {
				r.handle(ctx, key, job)
			}
		},
	})
	return w
}
