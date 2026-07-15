package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/mistermorph/llm"
)

var ErrContextCheckpointRevisionConflict = errors.New("context checkpoint revision conflict")

type ContextCheckpoint struct {
	Version         int         `json:"version"`
	Revision        int64       `json:"revision"`
	Message         llm.Message `json:"message"`
	CoveredThrough  string      `json:"covered_through,omitempty"`
	SourceModel     string      `json:"source_model,omitempty"`
	SourceRunID     string      `json:"source_run_id,omitempty"`
	CompactionCount int         `json:"compaction_count"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type ContextCheckpointStore interface {
	Load(context.Context) (ContextCheckpoint, bool, error)
	Save(context.Context, int64, ContextCheckpoint) error
	Delete(context.Context, int64) error
}

type runLocalCheckpointStore struct {
	mu         sync.Mutex
	checkpoint *ContextCheckpoint
}

func newRunLocalCheckpointStore() *runLocalCheckpointStore {
	return &runLocalCheckpointStore{}
}

func (s *runLocalCheckpointStore) Load(ctx context.Context) (ContextCheckpoint, bool, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpoint == nil {
		return ContextCheckpoint{}, false, nil
	}
	return cloneContextCheckpoint(*s.checkpoint), true, nil
}

func (s *runLocalCheckpointStore) Save(ctx context.Context, expectedRevision int64, checkpoint ContextCheckpoint) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := ValidateContextCheckpoint(checkpoint, expectedRevision); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentRevision := int64(0)
	if s.checkpoint != nil {
		currentRevision = s.checkpoint.Revision
	}
	if currentRevision != expectedRevision {
		return fmt.Errorf("%w: expected %d, current %d", ErrContextCheckpointRevisionConflict, expectedRevision, currentRevision)
	}
	cloned := cloneContextCheckpoint(checkpoint)
	s.checkpoint = &cloned
	return nil
}

func (s *runLocalCheckpointStore) Delete(ctx context.Context, expectedRevision int64) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentRevision := int64(0)
	if s.checkpoint != nil {
		currentRevision = s.checkpoint.Revision
	}
	if currentRevision != expectedRevision {
		return fmt.Errorf("%w: expected %d, current %d", ErrContextCheckpointRevisionConflict, expectedRevision, currentRevision)
	}
	s.checkpoint = nil
	return nil
}

// ValidateContextCheckpoint checks checkpoint content and its next revision before storage.
func ValidateContextCheckpoint(checkpoint ContextCheckpoint, expectedRevision int64) error {
	if checkpoint.Version <= 0 {
		return fmt.Errorf("context checkpoint version must be positive")
	}
	if expectedRevision < 0 || checkpoint.Revision != expectedRevision+1 {
		return fmt.Errorf("context checkpoint revision = %d, want %d", checkpoint.Revision, expectedRevision+1)
	}
	if normalizedMessageRole(checkpoint.Message.Role) != "user" || strings.TrimSpace(checkpoint.Message.Content) == "" {
		return fmt.Errorf("context checkpoint message must be a non-empty user message")
	}
	if checkpoint.CompactionCount < 0 {
		return fmt.Errorf("context checkpoint compaction count cannot be negative")
	}
	return nil
}

func cloneContextCheckpoint(checkpoint ContextCheckpoint) ContextCheckpoint {
	checkpoint.Message = cloneMessagesForCompaction([]llm.Message{checkpoint.Message})[0]
	return checkpoint
}

func insertLoadedCheckpoint(messages []llm.Message, fixedMessageCount int, checkpoint ContextCheckpoint) ([]llm.Message, error) {
	if fixedMessageCount < 0 || fixedMessageCount > len(messages) {
		return nil, fmt.Errorf("fixed message count %d is out of range", fixedMessageCount)
	}
	if normalizedMessageRole(checkpoint.Message.Role) != "user" || strings.TrimSpace(checkpoint.Message.Content) == "" {
		return nil, fmt.Errorf("loaded checkpoint message must be a non-empty user message")
	}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, messages[:fixedMessageCount]...)
	out = append(out, checkpoint.Message)
	out = append(out, messages[fixedMessageCount:]...)
	return out, nil
}

func coveredThroughForSelection(selection transcriptSelection, messageBoundaries map[int]string) string {
	if selection.End <= selection.Start || len(messageBoundaries) == 0 {
		return ""
	}
	coveredThrough := ""
	for index := selection.Start; index < selection.End; index++ {
		if boundary := strings.TrimSpace(messageBoundaries[index]); boundary != "" {
			coveredThrough = boundary
		}
	}
	return coveredThrough
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
