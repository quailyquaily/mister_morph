package guard

import "context"

type AuditSink interface {
	Emit(ctx context.Context, e AuditEvent) error
	Close() error
}
