package guard

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

type SQLiteApprovalStore struct {
	dsn string

	mu sync.Mutex
	db *sql.DB
}

func NewSQLiteApprovalStore(dsn string) (*SQLiteApprovalStore, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("missing sqlite dsn")
	}
	s := &SQLiteApprovalStore{dsn: dsn}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteApprovalStore) Create(ctx context.Context, rec ApprovalRecord) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil approval store")
	}
	if err := s.ensureOpen(); err != nil {
		return "", err
	}

	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(5 * time.Minute)
	}
	rec.Status = ApprovalPending

	id := rec.ID
	if strings.TrimSpace(id) == "" {
		id = "apr_" + randHex(12)
	}

	reasonsJSON, _ := json.Marshal(rec.Reasons)

	_, err := s.db.ExecContext(ctx, `
INSERT INTO guard_approvals (
  id, run_id, created_at_unix, expires_at_unix, resolved_at_unix,
  status, actor, comment,
  action_type, tool_name, action_hash,
  risk_level, decision, reasons_json,
  action_summary_redacted, resume_state
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, id, strings.TrimSpace(rec.RunID), rec.CreatedAt.Unix(), rec.ExpiresAt.Unix(), nullTimeUnix(rec.ResolvedAt),
		string(rec.Status), strings.TrimSpace(rec.Actor), strings.TrimSpace(rec.Comment),
		string(rec.ActionType), strings.TrimSpace(rec.ToolName), strings.TrimSpace(rec.ActionHash),
		string(rec.RiskLevel), string(rec.Decision), string(reasonsJSON),
		strings.TrimSpace(rec.ActionSummaryRedacted), rec.ResumeState,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *SQLiteApprovalStore) Get(ctx context.Context, id string) (ApprovalRecord, bool, error) {
	if s == nil {
		return ApprovalRecord{}, false, fmt.Errorf("nil approval store")
	}
	if err := s.ensureOpen(); err != nil {
		return ApprovalRecord{}, false, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ApprovalRecord{}, false, nil
	}

	var (
		rec            ApprovalRecord
		createdAtUnix  int64
		expiresAtUnix  int64
		resolvedAtUnix sql.NullInt64
		status         string
		actionType     string
		riskLevel      string
		decision       string
		reasonsJSON    string
	)
	err := s.db.QueryRowContext(ctx, `
SELECT
  id, run_id, created_at_unix, expires_at_unix, resolved_at_unix,
  status, actor, comment,
  action_type, tool_name, action_hash,
  risk_level, decision, reasons_json,
  action_summary_redacted, resume_state
FROM guard_approvals
WHERE id = ?
`, id).Scan(
		&rec.ID, &rec.RunID, &createdAtUnix, &expiresAtUnix, &resolvedAtUnix,
		&status, &rec.Actor, &rec.Comment,
		&actionType, &rec.ToolName, &rec.ActionHash,
		&riskLevel, &decision, &reasonsJSON,
		&rec.ActionSummaryRedacted, &rec.ResumeState,
	)
	if err == sql.ErrNoRows {
		return ApprovalRecord{}, false, nil
	}
	if err != nil {
		return ApprovalRecord{}, false, err
	}

	rec.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	rec.ExpiresAt = time.Unix(expiresAtUnix, 0).UTC()
	if resolvedAtUnix.Valid {
		t := time.Unix(resolvedAtUnix.Int64, 0).UTC()
		rec.ResolvedAt = &t
	}
	rec.Status = ApprovalStatus(status)
	rec.ActionType = ActionType(actionType)
	rec.RiskLevel = RiskLevel(riskLevel)
	rec.Decision = Decision(decision)

	_ = json.Unmarshal([]byte(reasonsJSON), &rec.Reasons)
	return rec, true, nil
}

func (s *SQLiteApprovalStore) Resolve(ctx context.Context, id string, status ApprovalStatus, actor string, comment string) error {
	if s == nil {
		return fmt.Errorf("nil approval store")
	}
	if err := s.ensureOpen(); err != nil {
		return err
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

	now := time.Now().UTC().Unix()
	_, err := s.db.ExecContext(ctx, `
UPDATE guard_approvals
SET status = ?, actor = ?, comment = ?, resolved_at_unix = ?
WHERE id = ? AND status = ?
`, string(status), strings.TrimSpace(actor), strings.TrimSpace(comment), now, id, string(ApprovalPending))
	return err
}

func (s *SQLiteApprovalStore) open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return nil
	}
	db, err := sql.Open("sqlite", s.dsn)
	if err != nil {
		return err
	}
	s.db = db
	return s.migrate()
}

func (s *SQLiteApprovalStore) ensureOpen() error {
	if s.db != nil {
		return nil
	}
	return s.open()
}

func (s *SQLiteApprovalStore) migrate() error {
	if s.db == nil {
		return fmt.Errorf("sqlite db is not open")
	}
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS guard_approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT,
  created_at_unix INTEGER NOT NULL,
  expires_at_unix INTEGER NOT NULL,
  resolved_at_unix INTEGER,
  status TEXT NOT NULL,
  actor TEXT,
  comment TEXT,
  action_type TEXT,
  tool_name TEXT,
  action_hash TEXT,
  risk_level TEXT,
  decision TEXT,
  reasons_json TEXT,
  action_summary_redacted TEXT,
  resume_state BLOB
);
CREATE INDEX IF NOT EXISTS idx_guard_approvals_status ON guard_approvals(status);
`)
	return err
}

func randHex(nbytes int) string {
	if nbytes <= 0 {
		nbytes = 12
	}
	b := make([]byte, nbytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nullTimeUnix(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}
