package acpclient

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestPrepareAgentConfig_OverrideCWDDoesNotReanchorRelativeRoots(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	override := filepath.Join(base, "worktrees", "child")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("MkdirAll(override) error = %v", err)
	}

	cfg := AgentConfig{
		Name:       "codex",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        base,
		ReadRoots:  []string{"src"},
		WriteRoots: []string{"out"},
	}

	prepared, err := PrepareAgentConfig(cfg, "worktrees/child")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}
	if prepared.ProfileCWD != base {
		t.Fatalf("prepared.ProfileCWD = %q, want %q", prepared.ProfileCWD, base)
	}
	if prepared.CWD != override {
		t.Fatalf("prepared.CWD = %q, want %q", prepared.CWD, override)
	}
	if got := prepared.ReadRoots[0]; got != filepath.Join(base, "src") {
		t.Fatalf("prepared.ReadRoots[0] = %q, want %q", got, filepath.Join(base, "src"))
	}
	if got := prepared.WriteRoots[0]; got != filepath.Join(base, "out") {
		t.Fatalf("prepared.WriteRoots[0] = %q, want %q", got, filepath.Join(base, "out"))
	}
}

func TestPrepareAgentConfig_RejectsOverrideOutsideAllowedRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	base := filepath.Join(root, "profile")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("MkdirAll(base) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	cfg := AgentConfig{
		Name:       "codex",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        base,
		ReadRoots:  []string{"."},
		WriteRoots: []string{"."},
	}

	if _, err := PrepareAgentConfig(cfg, outside); err == nil {
		t.Fatal("PrepareAgentConfig() error = nil, want outside allowed roots")
	}
}

func TestPrepareAgentConfig_FreezesMissingRootBoundary(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior varies on windows")
	}

	root := t.TempDir()
	base := filepath.Join(root, "profile")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("MkdirAll(base) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	cfg := AgentConfig{
		Name:       "codex",
		Enable:     true,
		Type:       "stdio",
		Command:    "helper",
		CWD:        base,
		ReadRoots:  []string{"sandbox"},
		WriteRoots: []string{"sandbox"},
	}
	prepared, err := PrepareAgentConfig(cfg, "")
	if err != nil {
		t.Fatalf("PrepareAgentConfig() error = %v", err)
	}

	lateRoot := filepath.Join(base, "sandbox")
	if err := os.Symlink(outside, lateRoot); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}
	target := filepath.Join(lateRoot, "secret.txt")
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret) error = %v", err)
	}

	if _, err := resolveAllowedPath(target, prepared.ReadRoots); err == nil {
		t.Fatal("resolveAllowedPath() error = nil, want frozen root boundary")
	}
}
