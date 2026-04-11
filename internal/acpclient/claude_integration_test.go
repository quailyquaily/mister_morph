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
	claudeACPIntegrationEnv    = "MISTERMORPH_ACP_CLAUDE_INTEGRATION"
	claudeACPSessionOptionsEnv = "MISTERMORPH_ACP_CLAUDE_SESSION_OPTIONS"
)

func TestRunPrompt_ClaudeNativeWrapperIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live Claude ACP integration test in short mode")
	}
	if strings.TrimSpace(os.Getenv(claudeACPIntegrationEnv)) != "1" {
		t.Skipf("set %s=1 to run the live Claude ACP integration test", claudeACPIntegrationEnv)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for the live Claude ACP integration test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is required for the live Claude ACP integration test")
	}
	authStatus := exec.Command("claude", "auth", "status")
	if err := authStatus.Run(); err != nil {
		t.Skipf("claude auth status failed: %v", err)
	}

	sessionOptions, err := parseClaudeACPSessionOptions(os.Getenv(claudeACPSessionOptionsEnv))
	if err != nil {
		t.Fatalf("parse %s: %v", claudeACPSessionOptionsEnv, err)
	}
	if sessionOptions == nil {
		sessionOptions = map[string]any{
			"permission_mode": "dontAsk",
			"allowed_tools":   []string{"Read"},
		}
	}

	repoRoot := repoRootFromTestFile(t)
	wrapperPath := filepath.Join(repoRoot, "wrappers", "acp", "claude", "src", "index.mjs")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the live Claude ACP integration test")
	}

	dir := t.TempDir()
	probePath := filepath.Join(dir, "acp_probe.txt")
	if err := os.WriteFile(probePath, []byte("ACP_CLAUDE_SMOKE_TOKEN_20260411\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(probePath) error = %v", err)
	}

	prepared, err := PrepareAgentConfig(AgentConfig{
		Name:       "claude",
		Enable:     true,
		Type:       "stdio",
		Command:    node,
		Args:       []string{wrapperPath},
		CWD:        dir,
		ReadRoots:  []string{dir},
		WriteRoots: []string{dir},
		Env: map[string]string{
			"MISTERMORPH_CLAUDE_COMMAND": "claude",
		},
		SessionOptions: sessionOptions,
	}, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	if strings.TrimSpace(output) != "ACP_CLAUDE_SMOKE_TOKEN_20260411" {
		t.Fatalf("Output = %q, want %q", result.Output, "ACP_CLAUDE_SMOKE_TOKEN_20260411")
	}
	if len(events) == 0 {
		t.Fatal("expected at least one ACP event from the Claude wrapper")
	}
}

func parseClaudeACPSessionOptions(raw string) (map[string]any, error) {
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
