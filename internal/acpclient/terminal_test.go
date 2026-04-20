package acpclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

func TestManagedTerminalWaitContext_WaitsForCapturedOutput(t *testing.T) {
	t.Parallel()

	term := &managedTerminal{
		done:        make(chan struct{}),
		captureDone: make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- term.waitContext(context.Background())
	}()

	close(term.done)

	select {
	case err := <-errCh:
		t.Fatalf("waitContext() returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(term.captureDone)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("waitContext() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waitContext() did not return after capture completed")
	}
}

func TestManagedTerminalWaitContext_RespectsContextWhileWaitingForCapture(t *testing.T) {
	t.Parallel()

	term := &managedTerminal{
		done:        make(chan struct{}),
		captureDone: make(chan struct{}),
	}
	close(term.done)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := term.waitContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitContext() error = %v, want %v", err, context.DeadlineExceeded)
	}
}
