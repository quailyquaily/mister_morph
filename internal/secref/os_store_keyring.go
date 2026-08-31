//go:build !darwin

package secref

import (
	"errors"

	keyring "github.com/zalando/go-keyring"
)

type systemKeyringClient struct{}

func (systemKeyringClient) Get(service, account string) (string, error) {
	value, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errKeyringNotFound
	}
	return value, err
}

func (systemKeyringClient) Set(service, account, label, password string) error {
	return setSystemKeyringSecret(service, account, label, password)
}

func (systemKeyringClient) Delete(service, account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return errKeyringNotFound
	}
	return err
}

func NewOSStore() OSStore {
	return newKeyringOSStore(systemKeyringClient{})
}
