package acpclient

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunPrompt_ClaudeNativeWrapperFakeBackend(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the Claude wrapper integration test")
	}

	repoRoot := repoRootFromTestFile(t)
	wrapperPath := filepath.Join(repoRoot, "wrappers", "acp", "claude", "src", "index.mjs")
	backendPath := filepath.Join(repoRoot, "wrappers", "acp", "claude", "test", "fixtures", "fake-claude-success.mjs")

	prepared, err := PrepareAgentConfig(AgentConfig{
		Name:       "claude",
		Enable:     true,
		Type:       "stdio",
		Command:    node,
		Args:       []string{wrapperPath},
		CWD:        repoRoot,
		ReadRoots:  []string{repoRoot},
		WriteRoots: []string{repoRoot},
		Env: map[string]string{
			"MISTERMORPH_CLAUDE_COMMAND": node,
			"MISTERMORPH_CLAUDE_ARGS":    backendPath,
		},
		SessionOptions: map[string]any{
			"permission_mode": "dontAsk",
			"allowed_tools":   []string{"Read"},
			"max_turns":       2,
		},
	}, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var events []Event
	result, err := RunPrompt(ctx, prepared, RunRequest{
		Prompt: "Say exactly: Hello",
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
	if strings.TrimSpace(result.Output) != "Hello" {
		t.Fatalf("Output = %q, want %q", result.Output, "Hello")
	}
	if len(events) == 0 {
		t.Fatal("expected at least one ACP event from the Claude wrapper")
	}
}

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
