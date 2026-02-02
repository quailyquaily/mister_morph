package guard

import (
	"context"
	"time"
)

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalExpired  ApprovalStatus = "expired"
)

type ApprovalRecord struct {
	ID        string
	RunID     string
	CreatedAt time.Time
	ExpiresAt time.Time
	ResolvedAt *time.Time

	Status   ApprovalStatus
	Actor    string
	Comment  string

	ActionType ActionType
	ToolName   string
	ActionHash string

	RiskLevel RiskLevel
	Decision  Decision
	Reasons   []string

	ActionSummaryRedacted string

	ResumeState []byte
}

type ApprovalStore interface {
	Create(ctx context.Context, rec ApprovalRecord) (string, error)
	Get(ctx context.Context, id string) (ApprovalRecord, bool, error)
	Resolve(ctx context.Context, id string, status ApprovalStatus, actor string, comment string) error
}

