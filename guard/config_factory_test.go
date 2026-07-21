package guard

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestSnapshotFromReaderDecodesGuardConfiguration(t *testing.T) {
	reader := viper.New()
	stateDir := t.TempDir()
	reader.Set("file_state_dir", stateDir)
	reader.Set("guard.dir_name", "policy")
	reader.Set("guard.enabled", true)
	reader.Set("guard.network.url_fetch.allowed_url_prefixes", []string{"https://api.example.test/v1"})
	reader.Set("guard.network.url_fetch.deny_private_ips", true)
	reader.Set("guard.network.url_fetch.follow_redirects", true)
	reader.Set("guard.network.url_fetch.allow_proxy", true)
	reader.Set("guard.redaction.enabled", true)
	reader.Set("guard.redaction.patterns", []map[string]any{{"name": "account", "re": `acct_[0-9]+`}})
	reader.Set("guard.audit.rotate_max_bytes", int64(2048))
	reader.Set("guard.approvals.enabled", true)

	snapshot, err := SnapshotFromReader(reader)
	if err != nil {
		t.Fatalf("SnapshotFromReader() error = %v", err)
	}
	if !snapshot.Enabled || snapshot.Dir != filepath.Join(stateDir, "policy") {
		t.Fatalf("snapshot boundary = %#v", snapshot)
	}
	wantPrefixes := []string{"https://api.example.test/v1"}
	if !reflect.DeepEqual(snapshot.Config.Network.URLFetch.AllowedURLPrefixes, wantPrefixes) {
		t.Fatalf("allowed prefixes = %#v, want %#v", snapshot.Config.Network.URLFetch.AllowedURLPrefixes, wantPrefixes)
	}
	if !snapshot.Config.Network.URLFetch.DenyPrivateIPs || !snapshot.Config.Network.URLFetch.FollowRedirects || !snapshot.Config.Network.URLFetch.AllowProxy {
		t.Fatalf("network policy = %#v", snapshot.Config.Network.URLFetch)
	}
	if len(snapshot.Config.Redaction.Patterns) != 1 || snapshot.Config.Redaction.Patterns[0].Name != "account" {
		t.Fatalf("redaction patterns = %#v", snapshot.Config.Redaction.Patterns)
	}
	if snapshot.Config.Audit.RotateMaxBytes != 2048 || !snapshot.Config.Approvals.Enabled {
		t.Fatalf("persistence config = %#v / %#v", snapshot.Config.Audit, snapshot.Config.Approvals)
	}
}

func TestNewFromSnapshotBuildsConfiguredStores(t *testing.T) {
	guardDir := t.TempDir()
	g := NewFromSnapshot(Snapshot{
		Enabled: true,
		Dir:     guardDir,
		Config: Config{
			Enabled:   true,
			Audit:     AuditConfig{RotateMaxBytes: 2048},
			Approvals: ApprovalsConfig{Enabled: true},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if g == nil || g.audit == nil || g.approvals == nil {
		t.Fatalf("NewFromSnapshot() = %#v, want configured audit and approval stores", g)
	}
}

func TestNewCheckedFailsWhenEnabledGuardDirectoryCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	guardDir := filepath.Join(root, "guard-file")
	if err := os.WriteFile(guardDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	g, err := NewChecked(Snapshot{
		Enabled: true,
		Dir:     guardDir,
		Config:  Config{Enabled: true},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "guard directory") {
		t.Fatalf("NewChecked() guard = %#v, error = %v, want guard directory error", g, err)
	}
	if g != nil {
		t.Fatalf("NewChecked() guard = %#v, want nil on initialization failure", g)
	}
}

func TestNewFromSnapshotPanicsWhenEnabledGuardCannotInitialize(t *testing.T) {
	root := t.TempDir()
	guardDir := filepath.Join(root, "guard-file")
	if err := os.WriteFile(guardDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("NewFromSnapshot() did not panic on enabled Guard initialization failure")
		}
	}()
	_ = NewFromSnapshot(Snapshot{
		Enabled: true,
		Dir:     guardDir,
		Config:  Config{Enabled: true},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNewCheckedFailsWhenEnabledAuditSinkCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "audit-parent")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	g, err := NewChecked(Snapshot{
		Enabled: true,
		Dir:     filepath.Join(root, "guard"),
		Config: Config{
			Enabled: true,
			Audit:   AuditConfig{JSONLPath: filepath.Join(blockedParent, "guard_audit.jsonl")},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "audit sink") {
		t.Fatalf("NewChecked() guard = %#v, error = %v, want audit sink error", g, err)
	}
	if g != nil {
		t.Fatalf("NewChecked() guard = %#v, want nil on initialization failure", g)
	}
}

func TestNewCheckedFailsWhenEnabledApprovalStoreCannotBeCreated(t *testing.T) {
	guardDir := t.TempDir()
	blockedApprovalsDir := filepath.Join(guardDir, "approvals")
	if err := os.WriteFile(blockedApprovalsDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	g, err := NewChecked(Snapshot{
		Enabled: true,
		Dir:     guardDir,
		Config: Config{
			Enabled:   true,
			Approvals: ApprovalsConfig{Enabled: true},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "approval store") {
		t.Fatalf("NewChecked() guard = %#v, error = %v, want approval store error", g, err)
	}
	if g != nil {
		t.Fatalf("NewChecked() guard = %#v, want nil on initialization failure", g)
	}
}

func TestNewCheckedHonorsEnabledSnapshot(t *testing.T) {
	g, err := NewChecked(Snapshot{
		Enabled: true,
		Dir:     t.TempDir(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewChecked() error = %v", err)
	}
	defer func() { _ = g.Close() }()
	if g == nil || !g.Enabled() {
		t.Fatalf("NewChecked() guard = %#v, want enabled guard", g)
	}
}
