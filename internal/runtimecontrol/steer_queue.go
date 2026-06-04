package runtimecontrol

import (
	"fmt"
	"strings"
	"sync"
)

const defaultSteerQueueLimit = 32

type SteerQueue struct {
	mu     sync.Mutex
	limit  int
	items  []string
	closed bool
}

func NewSteerQueue(limit int) *SteerQueue {
	if limit <= 0 {
		limit = defaultSteerQueueLimit
	}
	return &SteerQueue{limit: limit}
}

func (q *SteerQueue) Push(input string) (int, error) {
	if q == nil {
		return 0, fmt.Errorf("steer queue is nil")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return q.Len(), fmt.Errorf("steer input is empty")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return len(q.items), fmt.Errorf("steer queue is closed")
	}
	if q.limit > 0 && len(q.items) >= q.limit {
		return len(q.items), fmt.Errorf("steer queue is full")
	}
	q.items = append(q.items, input)
	return len(q.items), nil
}

func (q *SteerQueue) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.items = nil
}

func (q *SteerQueue) DrainAndClose() []string {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	if len(q.items) == 0 {
		return nil
	}
	out := make([]string, len(q.items))
	copy(out, q.items)
	q.items = nil
	return out
}

func (q *SteerQueue) Drain() []string {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	out := make([]string, len(q.items))
	copy(out, q.items)
	q.items = nil
	return out
}

func (q *SteerQueue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
