package main

import (
	"time"

	"github.com/spf13/viper"
)

func initViperDefaults() {
	// Shared agent defaults (used by serve/telegram when flags aren't available).
	viper.SetDefault("llm.provider", "openai")
	viper.SetDefault("llm.endpoint", "https://api.openai.com")
	viper.SetDefault("llm.model", "gpt-4o-mini")
	viper.SetDefault("llm.api_key", "")
	viper.SetDefault("llm.request_timeout", 90*time.Second)

	viper.SetDefault("max_steps", 15)
	viper.SetDefault("parse_retries", 2)
	viper.SetDefault("max_token_budget", 0)
	viper.SetDefault("timeout", 10*time.Minute)
	viper.SetDefault("plan.mode", "auto")

	// Global
	viper.SetDefault("file_cache_dir", "/var/cache/morph")
	viper.SetDefault("file_cache.max_age", 7*24*time.Hour)
	viper.SetDefault("file_cache.max_files", 1000)
	viper.SetDefault("file_cache.max_total_bytes", int64(512*1024*1024))
	viper.SetDefault("user_agent", "mister_morph/1.0 (+https://github.com/quailyquaily)")

	// Skills
	viper.SetDefault("skills.mode", "smart")
	viper.SetDefault("skills.max_load", 3)
	viper.SetDefault("skills.preview_bytes", int64(2048))
	viper.SetDefault("skills.catalog_limit", 200)
	viper.SetDefault("skills.select_timeout", 10*time.Second)

	// Daemon server
	viper.SetDefault("server.bind", "127.0.0.1")
	viper.SetDefault("server.port", 8787)
	viper.SetDefault("server.max_queue", 100)
	viper.SetDefault("server.url", "http://127.0.0.1:8787")

	// Submit client
	viper.SetDefault("submit.wait", false)
	viper.SetDefault("submit.poll_interval", 1*time.Second)

	// Telegram
	viper.SetDefault("telegram.poll_timeout", 30*time.Second)
	viper.SetDefault("telegram.history_max_messages", 20)
	viper.SetDefault("telegram.aliases", []string{})
	viper.SetDefault("telegram.group_trigger_mode", "smart")
	viper.SetDefault("telegram.alias_prefix_max_chars", 24)
	viper.SetDefault("telegram.addressing_llm.enabled", false)
	viper.SetDefault("telegram.addressing_llm.mode", "borderline")
	viper.SetDefault("telegram.addressing_llm.model", "")
	viper.SetDefault("telegram.addressing_llm.timeout", 3*time.Second)
	viper.SetDefault("telegram.addressing_llm.min_confidence", 0.55)
	viper.SetDefault("telegram.max_concurrency", 3)

	// DB (Phase 1: sqlite only)
	viper.SetDefault("db.driver", "sqlite")
	viper.SetDefault("db.dsn", "")
	viper.SetDefault("db.pool.max_open_conns", 1)
	viper.SetDefault("db.pool.max_idle_conns", 1)
	viper.SetDefault("db.pool.conn_max_lifetime", 0*time.Second)
	viper.SetDefault("db.sqlite.busy_timeout_ms", 5000)
	viper.SetDefault("db.sqlite.wal", true)
	viper.SetDefault("db.sqlite.foreign_keys", true)
	viper.SetDefault("db.automigrate", true)

	// Long-term memory (Phase 1)
	viper.SetDefault("memory.enabled", false)
	viper.SetDefault("memory.injection.enabled", true)
	viper.SetDefault("memory.injection.max_items", 50)
	viper.SetDefault("memory.injection.max_chars", 6000)

	// Secrets / auth profiles (Phase 0: disabled by default, fail-closed).
	viper.SetDefault("secrets.enabled", false)
	viper.SetDefault("secrets.allow_profiles", []string{})
	viper.SetDefault("secrets.aliases", map[string]string{})
	viper.SetDefault("secrets.require_skill_profiles", false)
	viper.SetDefault("auth_profiles", map[string]any{})

	// Guard (M1: disabled by default).
	viper.SetDefault("guard.enabled", false)
	viper.SetDefault("guard.network.url_fetch.allowed_url_prefixes", []string{})
	viper.SetDefault("guard.network.url_fetch.deny_private_ips", true)
	viper.SetDefault("guard.network.url_fetch.resolve_dns", true)
	viper.SetDefault("guard.network.url_fetch.follow_redirects", false)
	viper.SetDefault("guard.network.url_fetch.allow_proxy", false)
	viper.SetDefault("guard.redaction.enabled", true)
	viper.SetDefault("guard.redaction.patterns", []map[string]any{})
	viper.SetDefault("guard.bash.require_approval", true)
	viper.SetDefault("guard.audit.jsonl_path", "")
	viper.SetDefault("guard.audit.rotate_max_bytes", int64(100*1024*1024))
	viper.SetDefault("guard.approvals.enabled", true)
}
