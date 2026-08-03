package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/internal/fsstore"
)

func TestParseCommandArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		want    Command
		wantErr string
	}{
		{
			name: "status",
			args: "",
			want: Command{Action: CommandStatus},
		},
		{
			name: "attach",
			args: "attach ./repo",
			want: Command{Action: CommandAttach, Dir: "./repo"},
		},
		{
			name: "detach",
			args: "detach",
			want: Command{Action: CommandDetach},
		},
		{
			name:    "detach extra args",
			args:    "detach now",
			wantErr: "usage: /workspace | /workspace attach <dir> | /workspace detach",
		},
		{
			name:    "unknown",
			args:    "list",
			wantErr: "usage: /workspace | /workspace attach <dir> | /workspace detach",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCommandArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestExecuteStoreCommandLifecycle(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workspace_attachments.json"))
	scopeKey := "line:Cgroup123"
	dirA := t.TempDir()
	dirB := t.TempDir()

	result, err := ExecuteStoreCommand(store, scopeKey, "", dirA, nil)
	if err != nil {
		t.Fatalf("status error: %v", err)
	}
	if result.Reply != "workspace: "+dirA+" (default)" {
		t.Fatalf("status reply = %q", result.Reply)
	}

	result, err = ExecuteStoreCommand(store, scopeKey, "attach "+dirB, dirA, nil)
	if err != nil {
		t.Fatalf("attach error: %v", err)
	}
	if result.Reply != "workspace attached: "+dirB {
		t.Fatalf("attach reply = %q", result.Reply)
	}
	if result.WorkspaceDir != dirB {
		t.Fatalf("workspace result = %#v, want attachment %q", result, dirB)
	}

	currentDir, err := LookupWorkspaceDir(store, scopeKey)
	if err != nil {
		t.Fatalf("lookup error: %v", err)
	}
	if currentDir != dirB {
		t.Fatalf("lookup = %q, want %q", currentDir, dirB)
	}

	result, err = ExecuteStoreCommand(store, scopeKey, "detach", dirA, nil)
	if err != nil {
		t.Fatalf("detach error: %v", err)
	}
	if result.Reply != "workspace detached: "+dirB+"\nworkspace: "+dirA+" (default)" {
		t.Fatalf("detach reply = %q", result.Reply)
	}
	if result.WorkspaceDir != dirA {
		t.Fatalf("detach result = %#v, want default %q", result, dirA)
	}

	currentDir, err = LookupWorkspaceDir(store, scopeKey)
	if err != nil {
		t.Fatalf("lookup after detach error: %v", err)
	}
	if currentDir != "" {
		t.Fatalf("lookup after detach = %q, want empty", currentDir)
	}

	result, err = ExecuteStoreCommand(store, scopeKey, "detach", dirA, nil)
	if err != nil {
		t.Fatalf("detach default error: %v", err)
	}
	if result.Reply != "workspace: "+dirA+" (default); no attachment to detach" {
		t.Fatalf("detach default reply = %q", result.Reply)
	}
}

func TestResolveUsesAttachmentThenDefault(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workspace_attachments.json"))
	scopeKey := "console:topic-a"
	defaultDir := t.TempDir()
	attachedDir := t.TempDir()

	resolved, err := Resolve(store, scopeKey, defaultDir)
	if err != nil {
		t.Fatalf("Resolve(default) error = %v", err)
	}
	if resolved.WorkspaceDir != defaultDir || resolved.Source != SourceDefault {
		t.Fatalf("Resolve(default) = %#v", resolved)
	}
	if _, ok, err := store.Get(scopeKey); err != nil || ok {
		t.Fatalf("default created attachment: ok=%v err=%v", ok, err)
	}

	if _, _, err := store.Set(scopeKey, Attachment{WorkspaceDir: attachedDir}); err != nil {
		t.Fatalf("store.Set() error = %v", err)
	}
	resolved, err = Resolve(store, scopeKey, defaultDir)
	if err != nil {
		t.Fatalf("Resolve(attachment) error = %v", err)
	}
	if resolved.WorkspaceDir != attachedDir || resolved.Source != SourceAttachment {
		t.Fatalf("Resolve(attachment) = %#v", resolved)
	}
}

func TestResolveReturnsNoneWithoutAttachmentOrDefault(t *testing.T) {
	resolved, err := Resolve(nil, "console:topic-a", "")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.WorkspaceDir != "" || resolved.Source != SourceNone {
		t.Fatalf("Resolve() = %#v", resolved)
	}
}

func TestResolveInitialWorkspacePriority(t *testing.T) {
	cwd := t.TempDir()
	defaultDir := t.TempDir()
	explicitDir := t.TempDir()

	tests := []struct {
		name       string
		raw        string
		disabled   bool
		defaultDir string
		want       string
	}{
		{name: "disabled", raw: explicitDir, disabled: true, defaultDir: defaultDir, want: ""},
		{name: "explicit", raw: explicitDir, defaultDir: defaultDir, want: explicitDir},
		{name: "configured default", defaultDir: defaultDir, want: defaultDir},
		{name: "cwd fallback", want: cwd},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveInitialWorkspace(cwd, tc.raw, tc.disabled, tc.defaultDir, nil)
			if err != nil {
				t.Fatalf("ResolveInitialWorkspace() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ResolveInitialWorkspace() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateDir_RejectsOutsideAllowedRoots(t *testing.T) {
	allowedRoot := t.TempDir()
	outsideRoot := t.TempDir()

	_, err := ValidateDir(outsideRoot, []string{allowedRoot})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreSetWaitsForCrossProcessLock(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workspace_attachments.json"))
	scopeKey := "line:Cgroup123"
	dir := t.TempDir()
	done := make(chan error, 1)

	err := fsstore.WithLock(context.Background(), store.lockPath, func() error {
		go func() {
			_, _, err := store.Set(scopeKey, Attachment{WorkspaceDir: dir})
			done <- err
		}()
		select {
		case err := <-done:
			return fmt.Errorf("set finished while lock was held: %v", err)
		case <-time.After(120 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		t.Fatalf("holding external lock: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("store.Set() error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("store.Set() did not finish after lock release")
	}

	got, ok, err := store.Get(scopeKey)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if !ok {
		t.Fatalf("store.Get() found = false, want true")
	}
	if got.WorkspaceDir != dir {
		t.Fatalf("workspace dir = %q, want %q", got.WorkspaceDir, dir)
	}
}
