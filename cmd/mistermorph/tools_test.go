package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestToolsCommand_IncludesRuntimeTools(t *testing.T) {
	initViperDefaults()

	registryResolver := newRegistryRuntimeResolver()
	if err := registryResolver.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	t.Cleanup(func() { _ = registryResolver.Close() })
	cmd := newToolsCmd(registryResolver.Registry)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("tools command failed: %v", err)
	}

	got := out.String()
	checks := []string{
		"Core tools (",
		"Extra tools (",
		"Telegram tools (",
		"read_file",
		"spawn",
		"plan_create",
		"telegram_send_file",
		"telegram_send_photo",
		"telegram_send_voice",
		"message_react",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("tools output missing %q\noutput:\n%s", want, got)
		}
	}
}
