package acpclient

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClampTerminalOutputLimit(t *testing.T) {
	t.Parallel()

	if got := clampTerminalOutputLimit(0); got != defaultTerminalOutputSize {
		t.Fatalf("clampTerminalOutputLimit(0) = %d, want %d", got, defaultTerminalOutputSize)
	}
	if got := clampTerminalOutputLimit(defaultTerminalOutputSize / 2); got != defaultTerminalOutputSize/2 {
		t.Fatalf("clampTerminalOutputLimit(small) = %d, want %d", got, defaultTerminalOutputSize/2)
	}
	if got := clampTerminalOutputLimit(maxTerminalOutputSize * 4); got != maxTerminalOutputSize {
		t.Fatalf("clampTerminalOutputLimit(huge) = %d, want %d", got, maxTerminalOutputSize)
	}
}

func TestResolveTerminalCWD_RejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior varies on windows")
	}

	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("MkdirAll(allowed) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	escape := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("Symlink() unavailable: %v", err)
	}

	cfg := PreparedAgentConfig{
		ProfileCWD: allowed,
		CWD:        allowed,
		ReadRoots:  []string{allowed},
		WriteRoots: []string{allowed},
	}
	if _, err := resolveTerminalCWD(escape, cfg); err == nil {
		t.Fatal("resolveTerminalCWD() error = nil, want outside allowed roots")
	}
}
