package acpclient

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	codexACPIntegrationEnv    = "MISTERMORPH_ACP_CODEX_INTEGRATION"
	codexACPCommandEnv        = "MISTERMORPH_ACP_CODEX_COMMAND"
	codexACPArgsEnv           = "MISTERMORPH_ACP_CODEX_ARGS"
	codexACPSessionOptionsEnv = "MISTERMORPH_ACP_CODEX_SESSION_OPTIONS"
	codexACPProbeText         = "ACP_CODEX_SMOKE_TOKEN_20260411"
)

func TestRunPrompt_CodexACPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Codex ACP integration test in short mode")
	}
	if strings.TrimSpace(os.Getenv(codexACPIntegrationEnv)) != "1" {
		t.Skipf("set %s=1 to run the live Codex ACP integration test", codexACPIntegrationEnv)
	}

	command := strings.TrimSpace(os.Getenv(codexACPCommandEnv))
	if command == "" {
		if _, err := exec.LookPath("codex-acp"); err == nil {
			command = "codex-acp"
		}
	}
	if command == "" {
		t.Skipf("set %s or install codex-acp to run this live integration test", codexACPCommandEnv)
	}

	sessionOptions, err := parseCodexACPSessionOptions(os.Getenv(codexACPSessionOptionsEnv))
	if err != nil {
		t.Fatalf("parse %s: %v", codexACPSessionOptionsEnv, err)
	}

	dir := t.TempDir()
	probePath := filepath.Join(dir, "acp_probe.txt")
	if err := os.WriteFile(probePath, []byte(codexACPProbeText+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(probePath) error = %v", err)
	}

	prepared, err := PrepareAgentConfig(AgentConfig{
		Name:           "codex",
		Enable:         true,
		Type:           "stdio",
		Command:        command,
		Args:           strings.Fields(strings.TrimSpace(os.Getenv(codexACPArgsEnv))),
		CWD:            dir,
		ReadRoots:      []string{dir},
		WriteRoots:     []string{dir},
		SessionOptions: sessionOptions,
	}, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	startedAt := time.Now()

	var events []Event
	result, err := RunPrompt(ctx, prepared, RunRequest{
		Prompt: "Read ./acp_probe.txt and reply with exactly its full contents. " +
			"Do not add quotes, labels, explanations, or any extra text.",
		Observer: ObserverFunc(func(_ context.Context, event Event) {
			events = append(events, event)
		}),
	})
	if err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 45*time.Second {
		t.Fatalf("RunPrompt() took too long: %v", elapsed)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}

	output := strings.ReplaceAll(result.Output, "\r\n", "\n")
	if strings.TrimSpace(output) != codexACPProbeText {
		t.Fatalf("Output = %q, want %q", result.Output, codexACPProbeText)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one ACP event from Codex adapter")
	}
}

func parseCodexACPSessionOptions(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var options map[string]any
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil, err
	}
	return options, nil
}
