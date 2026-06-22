package guard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

const approvalsFileVersion = 1

type approvalStateFile struct {
	Version int                       `json:"version"`
	Records map[string]ApprovalRecord `json:"records"`
}

type FileApprovalStore struct {
	path     string
	lockPath string
}

func NewFileApprovalStore(path string, lockRoot string) (*FileApprovalStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("missing approvals file path")
	}
	if strings.TrimSpace(lockRoot) == "" {
		return nil, fmt.Errorf("missing lock root")
	}
	lockPath, err := fsstore.BuildLockPath(lockRoot, "state.guard_approvals")
	if err != nil {
		return nil, err
	}
	return &FileApprovalStore{
		path:     path,
		lockPath: lockPath,
	}, nil
}

func (s *FileApprovalStore) Create(ctx context.Context, rec ApprovalRecord) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil approval store")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(5 * time.Minute)
	}
	rec.Status = ApprovalPending

	id := strings.TrimSpace(rec.ID)
	if id == "" {
		id = "apr_" + randHex(12)
	}
	rec.ID = id

	if err := fsstore.WithLock(ctx, s.lockPath, func() error {
		state, err := s.loadState()
		if err != nil {
			return err
		}
		state.Records[id] = rec
		return s.saveState(state)
	}); err != nil {
		return "", err
	}
	return id, nil
}

func (s *FileApprovalStore) Get(ctx context.Context, id string) (ApprovalRecord, bool, error) {
	if s == nil {
		return ApprovalRecord{}, false, fmt.Errorf("nil approval store")
	}
	_ = ctx
	id = strings.TrimSpace(id)
	if id == "" {
		return ApprovalRecord{}, false, nil
	}
	state, err := s.loadState()
	if err != nil {
		return ApprovalRecord{}, false, err
	}
	rec, ok := state.Records[id]
	if !ok {
		return ApprovalRecord{}, false, nil
	}
	return rec, true, nil
}

func (s *FileApprovalStore) Resolve(ctx context.Context, id string, status ApprovalStatus, actor string, comment string) error {
	if s == nil {
		return fmt.Errorf("nil approval store")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("missing approval id")
	}

	switch status {
	case ApprovalApproved, ApprovalDenied:
	default:
		return fmt.Errorf("invalid approval status: %q", status)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return fsstore.WithLock(ctx, s.lockPath, func() error {
		state, err := s.loadState()
		if err != nil {
			return err
		}
		rec, ok := state.Records[id]
		if !ok {
			return ErrApprovalNotFound
		}
		if rec.Status != ApprovalPending {
			return ErrApprovalNotPending
		}
		now := time.Now().UTC()
		rec.Status = status
		rec.Actor = strings.TrimSpace(actor)
		rec.Comment = strings.TrimSpace(comment)
		rec.ResolvedAt = &now
		state.Records[id] = rec
		return s.saveState(state)
	})
}

func (s *FileApprovalStore) loadState() (approvalStateFile, error) {
	var file approvalStateFile
	ok, err := fsstore.ReadJSON(s.path, &file)
	if err != nil {
		return approvalStateFile{}, err
	}
	if !ok {
		return approvalStateFile{
			Version: approvalsFileVersion,
			Records: map[string]ApprovalRecord{},
		}, nil
	}
	if file.Version == 0 {
		file.Version = approvalsFileVersion
	}
	if file.Records == nil {
		file.Records = map[string]ApprovalRecord{}
	}
	return file, nil
}

func (s *FileApprovalStore) saveState(file approvalStateFile) error {
	if file.Version == 0 {
		file.Version = approvalsFileVersion
	}
	if file.Records == nil {
		file.Records = map[string]ApprovalRecord{}
	}
	return fsstore.WriteJSONAtomic(s.path, file, fsstore.FileOptions{})
}
