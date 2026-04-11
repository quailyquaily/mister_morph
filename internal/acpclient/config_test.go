package acpclient

import (
	"path/filepath"
	"testing"
)

func TestPrepareAgentConfig_ResolvesRelativePathsFromCWD(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	cfg := AgentConfig{
		Name:       "codex",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        base,
		ReadRoots:  []string{"src"},
		WriteRoots: []string{"out"},
	}

	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}
	if prepared.CWD != base {
		t.Fatalf("prepared.CWD = %q, want %q", prepared.CWD, base)
	}
	if got := prepared.ReadRoots[0]; got != filepath.Join(base, "src") {
		t.Fatalf("prepared.ReadRoots[0] = %q, want %q", got, filepath.Join(base, "src"))
	}
	if got := prepared.WriteRoots[0]; got != filepath.Join(base, "out") {
		t.Fatalf("prepared.WriteRoots[0] = %q, want %q", got, filepath.Join(base, "out"))
	}
}
