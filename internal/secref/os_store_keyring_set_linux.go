//go:build linux

package secref

import (
	ss "github.com/zalando/go-keyring/secret_service"
)

func setSystemKeyringSecret(service, account, label, password string) error {
	client, err := ss.NewSecretService()
	if err != nil {
		return err
	}
	session, err := client.OpenSession()
	if err != nil {
		return err
	}
	defer client.Close(session)

	collection := client.GetLoginCollection()
	if err := client.Unlock(collection.Path()); err != nil {
		return err
	}
	return client.CreateItem(collection, label, map[string]string{
		"username":   account,
		"service":    service,
		"xdg:schema": service,
	}, ss.NewSecret(session.Path(), password))
}
