package memory

import "context"

type NoopStore struct{}

func NewNoopStore() *NoopStore { return &NoopStore{} }

func (s *NoopStore) Get(_ context.Context, _ string, _ string, _ string, _ ReadOptions) (Item, bool, error) {
	return Item{}, false, nil
}

func (s *NoopStore) List(_ context.Context, _ string, _ string, _ ReadOptions) ([]Item, error) {
	return nil, nil
}

func (s *NoopStore) Put(_ context.Context, subjectID, namespace, key, value string, opt PutOptions) (Item, error) {
	vis := PrivateOnly
	if opt.Visibility != nil {
		vis = *opt.Visibility
	}
	return Item{
		SubjectID:  subjectID,
		Namespace:  namespace,
		Key:        key,
		Value:      value,
		Visibility: vis,
		Confidence: opt.Confidence,
		Source:     opt.Source,
		CreatedAt:  0,
		UpdatedAt:  0,
	}, nil
}

func (s *NoopStore) Delete(_ context.Context, _ string, _ string, _ string) error { return nil }

func (s *NoopStore) DeleteNamespace(_ context.Context, _ string, _ string) error { return nil }

func (s *NoopStore) WipeSubject(_ context.Context, _ string) error { return nil }
