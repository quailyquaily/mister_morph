package models

import "context"

// MemoryItemStore defines annotated SQL methods for memory_items.
// The blank line in comments is required by gorm/gen to separate description and SQL.
type MemoryItemStore interface {
	// GetAny returns a single memory item (no visibility filtering).
	//
	// SELECT * FROM @@table WHERE subject_id=@subjectID AND namespace=@namespace AND `key`=@key LIMIT 1;
	GetAny(ctx context.Context, subjectID string, namespace string, key string) (*MemoryItem, error)

	// GetPublic returns a single public-safe memory item (visibility=public_ok).
	//
	// SELECT * FROM @@table WHERE subject_id=@subjectID AND namespace=@namespace AND `key`=@key AND visibility=0 LIMIT 1;
	GetPublic(ctx context.Context, subjectID string, namespace string, key string) (*MemoryItem, error)

	// ListAny lists memory items in a namespace (no visibility filtering).
	// Pass prefix as "foo%" or empty string.
	//
	// SELECT * FROM @@table {{where}} subject_id=@subjectID AND namespace=@namespace {{if prefix != ""}} AND `key` LIKE @prefix{{end}}{{end}} ORDER BY updated_at DESC LIMIT @limit;
	ListAny(ctx context.Context, subjectID string, namespace string, prefix string, limit int) ([]*MemoryItem, error)

	// ListPublic lists public-safe memory items in a namespace (visibility=public_ok).
	// Pass prefix as "foo%" or empty string.
	//
	// SELECT * FROM @@table {{where}} subject_id=@subjectID AND namespace=@namespace AND visibility=0 {{if prefix != ""}} AND `key` LIKE @prefix{{end}}{{end}} ORDER BY updated_at DESC LIMIT @limit;
	ListPublic(ctx context.Context, subjectID string, namespace string, prefix string, limit int) ([]*MemoryItem, error)

	// Upsert writes the latest value for (subject_id, namespace, key).
	//
	// INSERT INTO @@table (subject_id, namespace, `key`, value, visibility, confidence, source, created_at, updated_at) VALUES (@subjectID, @namespace, @key, @value, @visibility, @confidence, @source, @createdAt, @updatedAt) ON CONFLICT(subject_id, namespace, `key`) DO UPDATE SET value=@value, visibility=@visibility, confidence=@confidence, source=@source, updated_at=@updatedAt;
	Upsert(ctx context.Context, subjectID string, namespace string, key string, value string, visibility int, confidence *float64, source *string, createdAt int64, updatedAt int64) error

	// DeleteKey deletes one memory item by key.
	//
	// DELETE FROM @@table WHERE subject_id=@subjectID AND namespace=@namespace AND `key`=@key;
	DeleteKey(ctx context.Context, subjectID string, namespace string, key string) error

	// DeleteNamespace deletes all memory items in a namespace for a subject.
	//
	// DELETE FROM @@table WHERE subject_id=@subjectID AND namespace=@namespace;
	DeleteNamespace(ctx context.Context, subjectID string, namespace string) error

	// WipeSubject deletes all memory items for a subject.
	//
	// DELETE FROM @@table WHERE subject_id=@subjectID;
	WipeSubject(ctx context.Context, subjectID string) error
}

// IdentityLinkStore defines annotated SQL methods for identity_links.
type IdentityLinkStore interface {
	// GetByExternalKey returns an identity link for an external_key.
	//
	// SELECT * FROM @@table WHERE external_key=@externalKey LIMIT 1;
	GetByExternalKey(ctx context.Context, externalKey string) (*IdentityLink, error)
}
