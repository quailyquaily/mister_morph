package mixinapi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

const maxKeystoreBytes = 1 << 20

type Credentials struct {
	ClientID   string
	SessionID  string
	privateKey ed25519.PrivateKey
}

type keystoreFile struct {
	AppID             string `json:"app_id"`
	ClientID          string `json:"client_id"`
	SessionID         string `json:"session_id"`
	SessionPrivateKey string `json:"session_private_key"`
	PrivateKey        string `json:"private_key"`
}

func LoadKeystore(path string) (Credentials, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Credentials{}, fmt.Errorf("mixin keystore path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("open mixin keystore: %w", err)
	}
	defer file.Close()
	if runtime.GOOS != "windows" {
		info, statErr := file.Stat()
		if statErr != nil {
			return Credentials{}, fmt.Errorf("stat mixin keystore: %w", statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return Credentials{}, fmt.Errorf("mixin keystore permissions must not allow group or other access")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxKeystoreBytes+1))
	if err != nil {
		return Credentials{}, fmt.Errorf("read mixin keystore: %w", err)
	}
	if len(raw) > maxKeystoreBytes {
		return Credentials{}, fmt.Errorf("mixin keystore exceeds %d bytes", maxKeystoreBytes)
	}
	credentials, err := ParseKeystore(raw)
	if err != nil {
		return Credentials{}, fmt.Errorf("parse mixin keystore: %w", err)
	}
	return credentials, nil
}

func ParseKeystore(raw []byte) (Credentials, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var file keystoreFile
	if err := decoder.Decode(&file); err != nil {
		return Credentials{}, fmt.Errorf("invalid json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Credentials{}, fmt.Errorf("invalid json: trailing data")
	}
	clientID := strings.TrimSpace(file.AppID)
	if clientID == "" {
		clientID = file.ClientID
	}
	privateKey := strings.TrimSpace(file.SessionPrivateKey)
	if privateKey == "" {
		privateKey = file.PrivateKey
	}
	return ParseCredentials(clientID, file.SessionID, privateKey)
}

func ParseCredentials(clientID, sessionID, privateKey string) (Credentials, error) {
	clientID, err := normalizeUUID("client_id", clientID)
	if err != nil {
		return Credentials{}, err
	}
	sessionID, err = normalizeUUID("session_id", sessionID)
	if err != nil {
		return Credentials{}, err
	}
	decodedPrivateKey, err := decodePrivateKey(privateKey)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{ClientID: clientID, SessionID: sessionID, privateKey: decodedPrivateKey}, nil
}

func (c Credentials) validate() error {
	if _, err := normalizeUUID("client_id", c.ClientID); err != nil {
		return err
	}
	if _, err := normalizeUUID("session_id", c.SessionID); err != nil {
		return err
	}
	if len(c.privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("private_key must contain an Ed25519 seed or private key")
	}
	return nil
}

func normalizeUUID(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return "", fmt.Errorf("%s must be a non-zero UUID", field)
	}
	return id.String(), nil
}

func decodePrivateKey(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("private_key is required")
	}
	var candidates [][]byte
	if decoded, err := hex.DecodeString(value); err == nil {
		candidates = append(candidates, decoded)
	}
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			candidates = append(candidates, decoded)
		}
	}
	for _, decoded := range candidates {
		switch len(decoded) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(decoded), nil
		case ed25519.PrivateKeySize:
			privateKey := append(ed25519.PrivateKey(nil), decoded...)
			seedKey := ed25519.NewKeyFromSeed(privateKey.Seed())
			if bytes.Equal(seedKey, privateKey) {
				return privateKey, nil
			}
		}
	}
	return nil, fmt.Errorf("private_key must be a Base64URL, Base64, or hexadecimal Ed25519 seed/private key")
}
