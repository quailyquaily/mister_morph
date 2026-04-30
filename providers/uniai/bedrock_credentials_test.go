package uniai

import (
	"context"
	"strings"
	"testing"
)

func TestResolveBedrockCredentialsNilConfig(t *testing.T) {
	err := ResolveBedrockCredentials(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	if !strings.Contains(err.Error(), "bedrock config is nil") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveBedrockCredentialsPartialStaticKeyOnly(t *testing.T) {
	cfg := Config{
		Provider:  "bedrock",
		AwsKey:    "AKIA...",
		AwsRegion: "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for partial static credentials (key only), got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete credentials error, got: %v", err)
	}
}

func TestResolveBedrockCredentialsPartialStaticSecretOnly(t *testing.T) {
	cfg := Config{
		Provider:  "bedrock",
		AwsSecret: "secret...",
		AwsRegion: "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for partial static credentials (secret only), got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete credentials error, got: %v", err)
	}
}

func TestResolveBedrockCredentialsPartialStaticSessionTokenOnly(t *testing.T) {
	cfg := Config{
		Provider:        "bedrock",
		AwsSessionToken: "token...",
		AwsRegion:       "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for partial static credentials (session token only), got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete credentials error, got: %v", err)
	}
}

func TestResolveBedrockCredentialsPartialStaticKeyAndToken(t *testing.T) {
	cfg := Config{
		Provider:        "bedrock",
		AwsKey:          "AKIA...",
		AwsSessionToken: "token...",
		AwsRegion:       "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for partial static credentials (key + token), got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete credentials error, got: %v", err)
	}
}

func TestResolveBedrockCredentialsStaticCredentialsOK(t *testing.T) {
	cfg := Config{
		Provider:        "bedrock",
		AwsKey:          "AKIAIOSFODNN7EXAMPLE",
		AwsSecret:       "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		AwsSessionToken: "optional-session-token",
		AwsRegion:       "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("unexpected error for valid static credentials: %v", err)
	}
	if cfg.AwsKey != "AKIAIOSFODNN7EXAMPLE" {
		t.Fatalf("expected key to be preserved, got %q", cfg.AwsKey)
	}
	if cfg.AwsSecret != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Fatalf("expected secret to be preserved, got %q", cfg.AwsSecret)
	}
	if cfg.AwsSessionToken != "optional-session-token" {
		t.Fatalf("expected session token to be preserved, got %q", cfg.AwsSessionToken)
	}
	if cfg.AwsRegion != "us-east-1" {
		t.Fatalf("expected region to be preserved, got %q", cfg.AwsRegion)
	}
}

func TestResolveBedrockCredentialsStaticCredentialsWithoutSessionToken(t *testing.T) {
	cfg := Config{
		Provider:  "bedrock",
		AwsKey:    "AKIAIOSFODNN7EXAMPLE",
		AwsSecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		AwsRegion: "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err != nil {
		t.Fatalf("unexpected error for valid static credentials without session token: %v", err)
	}
	if cfg.AwsKey != "AKIAIOSFODNN7EXAMPLE" {
		t.Fatalf("expected key to be preserved, got %q", cfg.AwsKey)
	}
	if cfg.AwsSecret != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Fatalf("expected secret to be preserved, got %q", cfg.AwsSecret)
	}
}

func TestResolveBedrockCredentialsDefaultChainNoCreds(t *testing.T) {
	// When no credentials are provided at all, LoadDefaultConfig falls back
	// to the ambient credential chain. In a test environment with no AWS
	// config/env, this will fail at credential retrieval.
	cfg := Config{
		Provider:  "bedrock",
		AwsRegion: "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error when ambient AWS credentials are missing, got nil")
	}
	if !strings.Contains(err.Error(), "retrieve bedrock aws credentials") {
		t.Fatalf("expected credential retrieval error, got: %v", err)
	}
}

func TestResolveBedrockCredentialsProfileNotFound(t *testing.T) {
	cfg := Config{
		Provider:   "bedrock",
		AwsProfile: "nonexistent-profile-" + randomSuffix(),
		AwsRegion:  "us-east-1",
	}
	err := ResolveBedrockCredentials(context.Background(), &cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent profile, got nil")
	}
}

func randomSuffix() string {
	// Simple random suffix to avoid colliding with real profiles
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte('a' + (i*7)%26)
	}
	return string(b)
}
