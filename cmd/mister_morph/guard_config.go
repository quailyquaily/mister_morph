package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/quailyquaily/mister_morph/db"
	"github.com/quailyquaily/mister_morph/guard"
	"github.com/quailyquaily/mister_morph/internal/pathutil"
	"github.com/spf13/viper"
)

func guardFromViper(log *slog.Logger) *guard.Guard {
	if !viper.GetBool("guard.enabled") {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}

	var patterns []guard.RegexPattern
	_ = viper.UnmarshalKey("guard.redaction.patterns", &patterns)

	cfg := guard.Config{
		Enabled: true,
		Network: guard.NetworkConfig{
			URLFetch: guard.URLFetchNetworkPolicy{
				AllowedURLPrefixes: viper.GetStringSlice("guard.network.url_fetch.allowed_url_prefixes"),
				DenyPrivateIPs:     viper.GetBool("guard.network.url_fetch.deny_private_ips"),
				FollowRedirects:    viper.GetBool("guard.network.url_fetch.follow_redirects"),
				AllowProxy:         viper.GetBool("guard.network.url_fetch.allow_proxy"),
			},
		},
		Redaction: guard.RedactionConfig{
			Enabled:  viper.GetBool("guard.redaction.enabled"),
			Patterns: patterns,
		},
		Bash: guard.BashConfig{
			RequireApproval: viper.GetBool("guard.bash.require_approval"),
		},
		Audit: guard.AuditConfig{
			JSONLPath:      strings.TrimSpace(viper.GetString("guard.audit.jsonl_path")),
			RotateMaxBytes: viper.GetInt64("guard.audit.rotate_max_bytes"),
		},
		Approvals: guard.ApprovalsConfig{
			Enabled: viper.GetBool("guard.approvals.enabled"),
		},
	}

	jsonlPath := strings.TrimSpace(cfg.Audit.JSONLPath)
	if jsonlPath == "" {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			jsonlPath = filepath.Join(home, ".morph", "guard_audit.jsonl")
		}
	}
	jsonlPath = pathutil.ExpandHomePath(jsonlPath)

	var sink guard.AuditSink
	if strings.TrimSpace(jsonlPath) != "" {
		s, err := guard.NewJSONLAuditSink(jsonlPath, cfg.Audit.RotateMaxBytes)
		if err != nil {
			log.Warn("guard_audit_sink_error", "error", err.Error())
		} else {
			sink = s
		}
	}

	var approvals guard.ApprovalStore
	if cfg.Approvals.Enabled {
		dsn, err := db.ResolveSQLiteDSN(viper.GetString("db.dsn"))
		if err != nil {
			log.Warn("guard_approvals_dsn_error", "error", err.Error())
		}
		if strings.TrimSpace(dsn) != "" && err == nil {
			st, err := guard.NewSQLiteApprovalStore(dsn)
			if err != nil {
				log.Warn("guard_approvals_store_error", "error", err.Error())
			} else {
				approvals = st
			}
		}
	}

	log.Info("guard_enabled",
		"url_fetch_prefixes", len(cfg.Network.URLFetch.AllowedURLPrefixes),
		"bash_require_approval", cfg.Bash.RequireApproval,
		"audit_jsonl", jsonlPath,
		"approvals_enabled", approvals != nil,
	)

	return guard.New(cfg, sink, approvals)
}
