package acpclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cursorACPIntegrationEnv = "MISTERMORPH_ACP_CURSOR_INTEGRATION"

func TestRunPrompt_CursorACPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Cursor ACP integration test in short mode")
	}
	if strings.TrimSpace(os.Getenv(cursorACPIntegrationEnv)) != "1" {
		t.Skipf("set %s=1 to run the live Cursor ACP integration test", cursorACPIntegrationEnv)
	}
	if _, err := exec.LookPath("agent"); err != nil {
		t.Skip("Cursor CLI `agent` is required for the live Cursor ACP integration test")
	}

	dir := t.TempDir()
	probePath := filepath.Join(dir, "acp_probe.txt")
	if err := os.WriteFile(probePath, []byte("ACP_CURSOR_SMOKE_TOKEN_20260413\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(probePath) error = %v", err)
	}

	prepared, err := PrepareAgentConfig(AgentConfig{
		Name:       "cursor",
		Command:    "agent",
		Args:       []string{"acp"},
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
	}, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

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
	if result.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "end_turn")
	}
	output := strings.ReplaceAll(result.Output, "\r\n", "\n")
	if strings.TrimSpace(output) != "ACP_CURSOR_SMOKE_TOKEN_20260413" {
		t.Fatalf("Output = %q, want probe token", result.Output)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one ACP event from the Cursor ACP command")
	}
}
