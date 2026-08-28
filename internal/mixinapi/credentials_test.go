package mixinapi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseKeystore(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	tests := []struct {
		name       string
		privateKey string
	}{
		{name: "base64url full key", privateKey: base64.RawURLEncoding.EncodeToString(privateKey)},
		{name: "base64url seed", privateKey: base64.RawURLEncoding.EncodeToString(seed)},
		{name: "hex seed", privateKey: hex.EncodeToString(seed)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{
				"client_id":"773e5e77-4107-45c2-b648-8fc722ed77f5",
				"session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
				"private_key":%q,
				"pin":"123456",
				"pin_token":"ignored"
			}`, tt.privateKey))
			got, err := ParseKeystore(raw)
			if err != nil {
				t.Fatalf("ParseKeystore() error = %v", err)
			}
			if got.ClientID != "773e5e77-4107-45c2-b648-8fc722ed77f5" {
				t.Fatalf("ClientID = %q", got.ClientID)
			}
			if got.SessionID != "a34c07a9-755d-4b54-94c5-e45e9a2dd43e" {
				t.Fatalf("SessionID = %q", got.SessionID)
			}
			if !bytes.Equal(got.privateKey, privateKey) {
				t.Fatal("private key mismatch")
			}
		})
	}
}

func TestParseDashboardKeystore(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	raw := []byte(fmt.Sprintf(`{
		"app_id":"773e5e77-4107-45c2-b648-8fc722ed77f5",
		"session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		"server_public_key":"ignored",
		"session_private_key":%q
	}`, hex.EncodeToString(seed)))
	got, err := ParseKeystore(raw)
	if err != nil {
		t.Fatalf("ParseKeystore() error = %v", err)
	}
	if got.ClientID != "773e5e77-4107-45c2-b648-8fc722ed77f5" || got.SessionID != "a34c07a9-755d-4b54-94c5-e45e9a2dd43e" {
		t.Fatalf("credentials = %#v", got)
	}
	if !bytes.Equal(got.privateKey, ed25519.NewKeyFromSeed(seed)) {
		t.Fatal("private key mismatch")
	}
}

func TestParseCredentials(t *testing.T) {
	seed := bytes.Repeat([]byte{0x31}, ed25519.SeedSize)
	got, err := ParseCredentials(
		"773e5e77-4107-45c2-b648-8fc722ed77f5",
		"a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		hex.EncodeToString(seed),
	)
	if err != nil {
		t.Fatalf("ParseCredentials() error = %v", err)
	}
	if got.ClientID != "773e5e77-4107-45c2-b648-8fc722ed77f5" || got.SessionID != "a34c07a9-755d-4b54-94c5-e45e9a2dd43e" {
		t.Fatalf("credentials = %#v", got)
	}
}

func TestParseKeystoreRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed json", raw: `{`},
		{name: "missing client id", raw: `{"session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e","private_key":"x"}`},
		{name: "invalid client id", raw: `{"client_id":"bad","session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e","private_key":"x"}`},
		{name: "invalid session id", raw: `{"client_id":"773e5e77-4107-45c2-b648-8fc722ed77f5","session_id":"bad","private_key":"x"}`},
		{name: "invalid private key", raw: `{"client_id":"773e5e77-4107-45c2-b648-8fc722ed77f5","session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e","private_key":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseKeystore([]byte(tt.raw)); err == nil {
				t.Fatal("ParseKeystore() expected error")
			}
		})
	}
}

func TestLoadKeystore(t *testing.T) {
	seed := bytes.Repeat([]byte{0x24}, ed25519.SeedSize)
	path := filepath.Join(t.TempDir(), "mixin-keystore.json")
	raw := fmt.Sprintf(`{"client_id":"773e5e77-4107-45c2-b648-8fc722ed77f5","session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e","private_key":%q}`, hex.EncodeToString(seed))
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeystore(path); err != nil {
		t.Fatalf("LoadKeystore() error = %v", err)
	}
}

func TestLoadKeystoreRejectsGroupReadablePrivateKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not available")
	}
	seed := bytes.Repeat([]byte{0x24}, ed25519.SeedSize)
	path := filepath.Join(t.TempDir(), "mixin-keystore.json")
	raw := fmt.Sprintf(`{"client_id":"773e5e77-4107-45c2-b648-8fc722ed77f5","session_id":"a34c07a9-755d-4b54-94c5-e45e9a2dd43e","private_key":%q}`, hex.EncodeToString(seed))
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeystore(path); err == nil {
		t.Fatal("LoadKeystore() expected insecure permission error")
	}
}
