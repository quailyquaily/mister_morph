package secref

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"gopkg.in/yaml.v3"
)

const osKeyringService = "io.quaily.mistermorph"

var (
	ErrOSStoreUnavailable = errors.New("os secret store unavailable")
	errKeyringNotFound    = errors.New("keyring item not found")
)

type OSStore interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte) error
	Delete(context.Context, string) error
}

type keyringClient interface {
	Get(service, account string) (string, error)
	Set(service, account, password string) error
	Delete(service, account string) error
}

type keyringOSStore struct {
	client keyringClient
}

func newKeyringOSStore(client keyringClient) OSStore {
	return &keyringOSStore{client: client}
}

func NewOSSecretID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func OSSecretIDsInYAML(node *yaml.Node) map[string]bool {
	ids := map[string]bool{}
	var visit func(*yaml.Node)
	visit = func(current *yaml.Node) {
		if current == nil {
			return
		}
		if current.Kind == yaml.ScalarNode {
			if ref, ok := ParseSingleRef(current.Value); ok && ref.Kind == RefKindOS {
				ids[ref.SecretID] = true
			}
		}
		for _, child := range current.Content {
			visit(child)
		}
	}
	visit(node)
	return ids
}

func OSSecretRef(id string) string {
	return "secret://os/" + id
}

func (s *keyringOSStore) Get(ctx context.Context, id string) ([]byte, error) {
	if err := validateOSSecretID(id); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.client.Get(osKeyringService, id)
	if errors.Is(err, errKeyringNotFound) {
		return nil, ErrOSSecretNotFound
	}
	if err != nil {
		return nil, ErrOSStoreUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(value), nil
}

func (s *keyringOSStore) Put(ctx context.Context, id string, value []byte) error {
	if err := validateOSSecretID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.client.Set(osKeyringService, id, string(value)); err != nil {
		return ErrOSStoreUnavailable
	}
	return ctx.Err()
}

func (s *keyringOSStore) Delete(ctx context.Context, id string) error {
	if err := validateOSSecretID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.client.Delete(osKeyringService, id)
	if errors.Is(err, errKeyringNotFound) {
		return ErrOSSecretNotFound
	}
	if err != nil {
		return ErrOSStoreUnavailable
	}
	return ctx.Err()
}

func validateOSSecretID(id string) error {
	if !osSecretIDRe.MatchString(id) {
		return ErrInvalidSecretRef
	}
	return nil
}
