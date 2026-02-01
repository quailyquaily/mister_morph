package memory

import "context"

type Store interface {
	Get(ctx context.Context, subjectID, namespace, key string, opt ReadOptions) (Item, bool, error)
	List(ctx context.Context, subjectID, namespace string, opt ReadOptions) ([]Item, error)
	Put(ctx context.Context, subjectID, namespace, key, value string, opt PutOptions) (Item, error)
	Delete(ctx context.Context, subjectID, namespace, key string) error

	DeleteNamespace(ctx context.Context, subjectID, namespace string) error
	WipeSubject(ctx context.Context, subjectID string) error
}
