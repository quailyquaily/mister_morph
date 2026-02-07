package contacts

import "context"

type Store interface {
	Ensure(ctx context.Context) error

	GetContact(ctx context.Context, contactID string) (Contact, bool, error)
	PutContact(ctx context.Context, contact Contact) error
	ListContacts(ctx context.Context, status Status) ([]Contact, error)

	GetCandidate(ctx context.Context, itemID string) (ShareCandidate, bool, error)
	PutCandidate(ctx context.Context, candidate ShareCandidate) error
	ListCandidates(ctx context.Context) ([]ShareCandidate, error)

	GetSessionState(ctx context.Context, sessionID string) (SessionState, bool, error)
	PutSessionState(ctx context.Context, state SessionState) error
	ListSessionStates(ctx context.Context) ([]SessionState, error)

	AppendAuditEvent(ctx context.Context, event AuditEvent) error
	ListAuditEvents(ctx context.Context, tickID string, contactID string, action string, limit int) ([]AuditEvent, error)
}
