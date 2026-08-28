package mixinapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSignAuthenticationToken(t *testing.T) {
	seed := bytes.Repeat([]byte{0x17}, ed25519.SeedSize)
	credentials := Credentials{
		ClientID:   "773e5e77-4107-45c2-b648-8fc722ed77f5",
		SessionID:  "a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		privateKey: ed25519.NewKeyFromSeed(seed),
	}
	now := time.Date(2026, 8, 27, 1, 2, 3, 0, time.UTC)
	body := []byte(`{"conversation_id":"8f7059b9-b1b2-4ed8-a99f-4ac2f07a9a34"}`)
	token, err := signAuthenticationToken(credentials, "post", "/messages", body, now, "5f02a273-cd18-4af3-a57b-f3224a3c3591")
	if err != nil {
		t.Fatalf("signAuthenticationToken() error = %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d", len(parts))
	}
	var header map[string]any
	decodeJWTPart(t, parts[0], &header)
	if header["alg"] != "EdDSA" || header["typ"] != "JWT" {
		t.Fatalf("header = %#v", header)
	}
	var claims map[string]any
	decodeJWTPart(t, parts[1], &claims)
	if claims["uid"] != credentials.ClientID || claims["sid"] != credentials.SessionID {
		t.Fatalf("identity claims = %#v", claims)
	}
	if claims["jti"] != "5f02a273-cd18-4af3-a57b-f3224a3c3591" || claims["scp"] != "FULL" {
		t.Fatalf("request claims = %#v", claims)
	}
	digest := sha256.Sum256(append([]byte("POST/messages"), body...))
	if claims["sig"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("sig = %v", claims["sig"])
	}
	if claims["iat"] != float64(now.Unix()) || claims["exp"] != float64(now.Add(authenticationTokenTTL).Unix()) {
		t.Fatalf("time claims = %#v", claims)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(credentials.privateKey.Public().(ed25519.PublicKey), []byte(parts[0]+"."+parts[1]), signature) {
		t.Fatal("signature verification failed")
	}
}

func TestSignAuthenticationTokenRejectsInvalidInput(t *testing.T) {
	seed := bytes.Repeat([]byte{0x17}, ed25519.SeedSize)
	valid := Credentials{
		ClientID:   "773e5e77-4107-45c2-b648-8fc722ed77f5",
		SessionID:  "a34c07a9-755d-4b54-94c5-e45e9a2dd43e",
		privateKey: ed25519.NewKeyFromSeed(seed),
	}
	tests := []struct {
		name        string
		credentials Credentials
		method      string
		uri         string
		requestID   string
	}{
		{name: "missing method", credentials: valid, uri: "/me", requestID: "5f02a273-cd18-4af3-a57b-f3224a3c3591"},
		{name: "invalid uri", credentials: valid, method: "GET", uri: "https://api.mixin.one/me", requestID: "5f02a273-cd18-4af3-a57b-f3224a3c3591"},
		{name: "invalid request id", credentials: valid, method: "GET", uri: "/me", requestID: "bad"},
		{name: "invalid credentials", credentials: Credentials{}, method: "GET", uri: "/me", requestID: "5f02a273-cd18-4af3-a57b-f3224a3c3591"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := signAuthenticationToken(tt.credentials, tt.method, tt.uri, nil, time.Now(), tt.requestID); err == nil {
				t.Fatal("signAuthenticationToken() expected error")
			}
		})
	}
}

func decodeJWTPart(t *testing.T, value string, target any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
