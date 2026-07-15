package contextcheckpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/quailyquaily/mistermorph/agent"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

type FileStore struct {
	path     string
	lockPath string
	mu       sync.Mutex
}

func NewFileStore(root string, conversationKey string) (*FileStore, error) {
	root = strings.TrimSpace(root)
	conversationKey = strings.TrimSpace(conversationKey)
	if root == "" {
		return nil, fmt.Errorf("context checkpoint root is required")
	}
	if conversationKey == "" {
		return nil, fmt.Errorf("context checkpoint conversation key is required")
	}
	hash := sha256.Sum256([]byte(conversationKey))
	key := hex.EncodeToString(hash[:])
	lockPath, err := fsstore.BuildLockPath(filepath.Join(root, "locks"), "context_checkpoint_"+key)
	if err != nil {
		return nil, err
	}
	return &FileStore{
		path:     filepath.Join(root, "context_checkpoints", key+".json"),
		lockPath: lockPath,
	}, nil
}

func (s *FileStore) Load(ctx context.Context) (agent.ContextCheckpoint, bool, error) {
	if s == nil {
		return agent.ContextCheckpoint{}, false, fmt.Errorf("context checkpoint store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var checkpoint agent.ContextCheckpoint
	var found bool
	err := fsstore.WithLock(ctx, s.lockPath, func() error {
		var err error
		checkpoint, found, err = s.readLocked()
		return err
	})
	if err != nil {
		return agent.ContextCheckpoint{}, false, err
	}
	return checkpoint, found, nil
}

func (s *FileStore) Save(ctx context.Context, expectedRevision int64, checkpoint agent.ContextCheckpoint) error {
	if s == nil {
		return fmt.Errorf("context checkpoint store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expectedRevision < 0 {
		return fmt.Errorf("context checkpoint expected revision cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fsstore.WithLock(ctx, s.lockPath, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, found, err := s.readLocked()
		if err != nil {
			return err
		}
		currentRevision := int64(0)
		if found {
			currentRevision = current.Revision
		}
		if currentRevision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", agent.ErrContextCheckpointRevisionConflict, expectedRevision, currentRevision)
		}
		if err := agent.ValidateContextCheckpoint(checkpoint, expectedRevision); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return fsstore.WriteJSONAtomic(s.path, checkpoint, fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600})
	})
}

func (s *FileStore) Delete(ctx context.Context, expectedRevision int64) error {
	if s == nil {
		return fmt.Errorf("context checkpoint store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if expectedRevision < 0 {
		return fmt.Errorf("context checkpoint expected revision cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fsstore.WithLock(ctx, s.lockPath, func() error {
		current, found, err := s.readLocked()
		if err != nil {
			return err
		}
		currentRevision := int64(0)
		if found {
			currentRevision = current.Revision
		}
		if currentRevision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", agent.ErrContextCheckpointRevisionConflict, expectedRevision, currentRevision)
		}
		if !found {
			return nil
		}
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete context checkpoint: %w", err)
		}
		return nil
	})
}

func (s *FileStore) readLocked() (agent.ContextCheckpoint, bool, error) {
	var checkpoint agent.ContextCheckpoint
	found, err := fsstore.ReadJSONStrict(s.path, &checkpoint)
	if err != nil {
		return agent.ContextCheckpoint{}, false, err
	}
	if !found {
		return agent.ContextCheckpoint{}, false, nil
	}
	if checkpoint.Revision <= 0 {
		return agent.ContextCheckpoint{}, false, fmt.Errorf("stored context checkpoint has invalid revision")
	}
	if err := agent.ValidateContextCheckpoint(checkpoint, checkpoint.Revision-1); err != nil {
		return agent.ContextCheckpoint{}, false, fmt.Errorf("stored context checkpoint is invalid: %w", err)
	}
	return checkpoint, true, nil
}
