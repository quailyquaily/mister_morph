//go:build !darwin && !linux

package secref

import keyring "github.com/zalando/go-keyring"

func setSystemKeyringSecret(service, account, _ string, password string) error {
	return keyring.Set(service, account, password)
}
