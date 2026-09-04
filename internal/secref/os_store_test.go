package secref

import (
	"context"
	"errors"
	"testing"

	"gopkg.in/yaml.v3"
)

type fakeKeyringClient struct {
	values       map[string]map[string]string
	getErr       error
	setErr       error
	deleteErr    error
	lastService  string
	lastAccount  string
	lastLabel    string
	lastPassword string
}

func (f *fakeKeyringClient) Get(service, account string) (string, error) {
	f.lastService = service
	f.lastAccount = account
	if f.getErr != nil {
		return "", f.getErr
	}
	value, ok := f.values[service][account]
	if !ok {
		return "", errKeyringNotFound
	}
	return value, nil
}

func (f *fakeKeyringClient) Set(service, account, label, password string) error {
	f.lastService = service
	f.lastAccount = account
	f.lastLabel = label
	f.lastPassword = password
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = map[string]map[string]string{}
	}
	if f.values[service] == nil {
		f.values[service] = map[string]string{}
	}
	f.values[service][account] = password
	return nil
}

func (f *fakeKeyringClient) Delete(service, account string) error {
	f.lastService = service
	f.lastAccount = account
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.values[service][account]; !ok {
		return errKeyringNotFound
	}
	delete(f.values[service], account)
	return nil
}

func TestNewOSSecretIDCreatesValidOpaqueIDs(t *testing.T) {
	first, err := NewOSSecretID()
	if err != nil {
		t.Fatalf("NewOSSecretID() error = %v", err)
	}
	second, err := NewOSSecretID()
	if err != nil {
		t.Fatalf("second NewOSSecretID() error = %v", err)
	}
	if first == second {
		t.Fatalf("generated duplicate ids: %q", first)
	}
	for _, id := range []string{first, second} {
		encoded := OSSecretRef(id)
		if encoded != "${secret:"+id+"}" {
			t.Fatalf("OSSecretRef(%q) = %q", id, encoded)
		}
		ref, ok := ParseSingleRef(encoded)
		if !ok || ref.Kind != RefKindOS || ref.SecretID != id {
			t.Fatalf("generated id %q does not produce a valid OS ref", id)
		}
	}
}

func TestKeyringOSStoreLifecycle(t *testing.T) {
	client := &fakeKeyringClient{}
	store := newKeyringOSStore(client)
	const id = "b_LsX7HLzAR3OShG7YjRcw"

	if err := store.Put(context.Background(), id, "llm.profiles.reasoning.api_key", []byte("secret-value")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if client.lastService != osKeyringService || client.lastAccount != id || client.lastLabel != "MisterMorph · llm.profiles.reasoning.api_key" || client.lastPassword != "secret-value" {
		t.Fatalf("keyring Set args = %q/%q/%q/%q", client.lastService, client.lastAccount, client.lastLabel, client.lastPassword)
	}
	got, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(got) != "secret-value" {
		t.Fatalf("Get() = %q, want secret-value", got)
	}
	if err := store.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(context.Background(), id); !errors.Is(err, ErrOSSecretNotFound) {
		t.Fatalf("Get() after Delete error = %v, want ErrOSSecretNotFound", err)
	}
}

func TestKeyringOSStoreRejectsInvalidID(t *testing.T) {
	store := newKeyringOSStore(&fakeKeyringClient{})
	if err := store.Put(context.Background(), "provider/openai", "llm.api_key", []byte("secret")); !errors.Is(err, ErrInvalidSecretRef) {
		t.Fatalf("Put() error = %v, want ErrInvalidSecretRef", err)
	}
}

func TestKeyringOSStoreHidesBackendErrors(t *testing.T) {
	store := newKeyringOSStore(&fakeKeyringClient{getErr: errors.New("backend leaked secret-value")})
	_, err := store.Get(context.Background(), "b_LsX7HLzAR3OShG7YjRcw")
	if !errors.Is(err, ErrOSStoreUnavailable) {
		t.Fatalf("Get() error = %v, want ErrOSStoreUnavailable", err)
	}
	if err != ErrOSStoreUnavailable {
		t.Fatalf("Get() exposed wrapped backend error: %v", err)
	}
}

func TestKeyringOSStorePreservesSafeUnavailableReason(t *testing.T) {
	store := newKeyringOSStore(&fakeKeyringClient{setErr: ErrOSStoreServiceUnavailable})
	err := store.Put(context.Background(), "b_LsX7HLzAR3OShG7YjRcw", "llm.api_key", []byte("secret"))
	if !errors.Is(err, ErrOSStoreUnavailable) || !errors.Is(err, ErrOSStoreServiceUnavailable) {
		t.Fatalf("Put() error = %v, want service-unavailable reason", err)
	}
}

func TestKeyringOSStoreHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newKeyringOSStore(&fakeKeyringClient{})
	if _, err := store.Get(ctx, "b_LsX7HLzAR3OShG7YjRcw"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context.Canceled", err)
	}
}

func TestCheckOSStoreAvailableWhenProbeIsMissing(t *testing.T) {
	store := newKeyringOSStore(&fakeKeyringClient{})
	if err := CheckOSStore(context.Background(), store); err != nil {
		t.Fatalf("CheckOSStore() error = %v", err)
	}
}

func TestCheckOSStoreUnavailable(t *testing.T) {
	store := newKeyringOSStore(&fakeKeyringClient{getErr: errors.New("unavailable")})
	if err := CheckOSStore(context.Background(), store); !errors.Is(err, ErrOSStoreUnavailable) {
		t.Fatalf("CheckOSStore() error = %v, want ErrOSStoreUnavailable", err)
	}
}

func TestOSSecretIDsInYAMLFindsUniqueReferences(t *testing.T) {
	const firstID = "b_LsX7HLzAR3OShG7YjRcw"
	const secondID = "Kzv6u6zFjNkDjBj2n5RcvA"
	var doc yaml.Node
	body := "a: " + OSSecretRef(firstID) + "\nb:\n  - " + OSSecretRef(firstID) + "\n  - " + OSSecretRef(secondID) + "\n  - prefix-" + OSSecretRef(secondID) + "\n"
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}

	ids := OSSecretIDsInYAML(&doc)
	if len(ids) != 2 || !ids[firstID] || !ids[secondID] {
		t.Fatalf("ids = %#v, want both exact references", ids)
	}
}
