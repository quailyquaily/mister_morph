package guard

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/spf13/viper"
)

type Snapshot struct {
	Enabled bool
	Config  Config
	Dir     string
}

type ConfigReader interface {
	GetBool(string) bool
	GetInt64(string) int64
	GetString(string) string
	GetStringSlice(string) []string
	UnmarshalKey(string, any, ...viper.DecoderConfigOption) error
}

func SnapshotFromReader(reader ConfigReader) (Snapshot, error) {
	if reader == nil {
		return Snapshot{}, fmt.Errorf("guard config reader is nil")
	}
	var patterns []RegexPattern
	if err := reader.UnmarshalKey("guard.redaction.patterns", &patterns); err != nil {
		return Snapshot{}, fmt.Errorf("decode guard.redaction.patterns: %w", err)
	}
	return Snapshot{
		Enabled: reader.GetBool("guard.enabled"),
		Config: Config{
			Enabled: true,
			Network: NetworkConfig{
				URLFetch: URLFetchNetworkPolicy{
					AllowedURLPrefixes: append([]string(nil), reader.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes")...),
					DenyPrivateIPs:     reader.GetBool("guard.network.url_fetch.deny_private_ips"),
					FollowRedirects:    reader.GetBool("guard.network.url_fetch.follow_redirects"),
					AllowProxy:         reader.GetBool("guard.network.url_fetch.allow_proxy"),
				},
			},
			Redaction: RedactionConfig{
				Enabled:  reader.GetBool("guard.redaction.enabled"),
				Patterns: append([]RegexPattern(nil), patterns...),
			},
			Audit: AuditConfig{
				JSONLPath:      strings.TrimSpace(reader.GetString("guard.audit.jsonl_path")),
				RotateMaxBytes: reader.GetInt64("guard.audit.rotate_max_bytes"),
			},
			Approvals: ApprovalsConfig{
				Enabled: reader.GetBool("guard.approvals.enabled"),
			},
		},
		Dir: pathutil.ResolveStateChildDir(
			reader.GetString("file_state_dir"),
			reader.GetString("guard.dir_name"),
			"guard",
		),
	}, nil
}

func NewChecked(snapshot Snapshot, logger *slog.Logger) (*Guard, error) {
	if !snapshot.Enabled {
		return nil, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	guardDir := strings.TrimSpace(snapshot.Dir)
	if guardDir == "" {
		guardDir = pathutil.ResolveStateChildDir("", "", "guard")
	}
	if err := os.MkdirAll(guardDir, 0o700); err != nil {
		return nil, fmt.Errorf("initialize guard directory: %w", err)
	}
	lockRoot := filepath.Join(guardDir, ".fslocks")
	if err := os.MkdirAll(lockRoot, 0o700); err != nil {
		return nil, fmt.Errorf("initialize guard lock directory: %w", err)
	}

	jsonlPath := strings.TrimSpace(snapshot.Config.Audit.JSONLPath)
	if jsonlPath == "" {
		jsonlPath = filepath.Join(guardDir, "audit", "guard_audit.jsonl")
	}
	jsonlPath = pathutil.ExpandHomePath(jsonlPath)

	configuredSink, err := NewJSONLAuditSink(jsonlPath, snapshot.Config.Audit.RotateMaxBytes, lockRoot)
	if err != nil {
		return nil, fmt.Errorf("initialize guard audit sink: %w", err)
	}

	var approvals ApprovalStore
	if snapshot.Config.Approvals.Enabled {
		approvalsPath := filepath.Join(guardDir, "approvals", "guard_approvals.json")
		if err := os.MkdirAll(filepath.Dir(approvalsPath), 0o700); err != nil {
			return nil, errors.Join(
				fmt.Errorf("initialize guard approval store: %w", err),
				configuredSink.Close(),
			)
		}
		store, err := NewFileApprovalStore(approvalsPath, lockRoot)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("initialize guard approval store: %w", err),
				configuredSink.Close(),
			)
		}
		if _, err := store.loadState(); err != nil {
			return nil, errors.Join(
				fmt.Errorf("initialize guard approval store: %w", err),
				configuredSink.Close(),
			)
		}
		approvals = store
	}
	cfg := snapshot.Config
	cfg.Enabled = true

	logger.Info("guard_enabled",
		"guard_dir", guardDir,
		"url_fetch_prefixes", len(snapshot.Config.Network.URLFetch.AllowedURLPrefixes),
		"audit_jsonl", jsonlPath,
		"approvals_enabled", approvals != nil,
	)
	return New(cfg, configuredSink, approvals), nil
}

// NewFromSnapshot is retained for source compatibility. It stops the caller
// rather than silently disabling an explicitly enabled Guard when setup fails.
// New code should use NewChecked and propagate its initialization error.
func NewFromSnapshot(snapshot Snapshot, logger *slog.Logger) *Guard {
	g, err := NewChecked(snapshot, logger)
	if err != nil {
		panic(err)
	}
	return g
}
