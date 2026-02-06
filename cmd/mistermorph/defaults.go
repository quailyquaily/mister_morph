package main

import (
	"time"

	"github.com/spf13/viper"
)

func initViperDefaults() {
	// Shared agent defaults (used by serve/telegram when flags aren't available).
	viper.SetDefault("llm.provider", "openai")
	viper.SetDefault("llm.endpoint", "https://api.openai.com")
	viper.SetDefault("llm.model", "gpt-5.2")
	viper.SetDefault("llm.api_key", "")
	viper.SetDefault("llm.request_timeout", 90*time.Second)
	viper.SetDefault("llm.tools_emulation_mode", "off")

	viper.SetDefault("max_steps", 15)
	viper.SetDefault("parse_retries", 2)
	viper.SetDefault("max_token_budget", 0)
	viper.SetDefault("timeout", 10*time.Minute)
	viper.SetDefault("plan.max_steps", 6)

	// Global
	viper.SetDefault("file_state_dir", "~/.morph")
	viper.SetDefault("file_cache_dir", "/var/cache/morph")
	viper.SetDefault("file_cache.max_age", 7*24*time.Hour)
	viper.SetDefault("file_cache.max_files", 1000)
	viper.SetDefault("file_cache.max_total_bytes", int64(512*1024*1024))
	viper.SetDefault("user_agent", "mistermorph/1.0 (+https://github.com/quailyquaily)")

	// Skills
	viper.SetDefault("skills.mode", "smart")
	viper.SetDefault("skills.max_load", 3)
	viper.SetDefault("skills.preview_bytes", int64(2048))
	viper.SetDefault("skills.catalog_limit", 200)
	viper.SetDefault("skills.select_timeout", 10*time.Second)
	viper.SetDefault("skills.dir_name", "skills")

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
	viper.SetDefault("telegram.smart_addressing_max_chars", 24)
	viper.SetDefault("telegram.smart_addressing_confidence", 0.55)
	viper.SetDefault("telegram.max_concurrency", 3)
	viper.SetDefault("telegram.reactions.enabled", true)

	// Heartbeat
	viper.SetDefault("heartbeat.enabled", true)
	viper.SetDefault("heartbeat.interval", 30*time.Minute)

	// Intent inference
	viper.SetDefault("intent.enabled", true)
	viper.SetDefault("intent.timeout", 8*time.Second)
	viper.SetDefault("intent.max_history", 8)

	// Long-term memory (Phase 1)
	viper.SetDefault("memory.enabled", true)
	viper.SetDefault("memory.dir_name", "memory")
	viper.SetDefault("memory.short_term_days", 7)
	viper.SetDefault("memory.injection.enabled", true)
	viper.SetDefault("memory.injection.max_items", 50)

	// Secrets / auth profiles (Phase 0: disabled by default, fail-closed).
	viper.SetDefault("secrets.enabled", false)
	viper.SetDefault("secrets.allow_profiles", []string{})
	viper.SetDefault("secrets.aliases", map[string]string{})
	viper.SetDefault("secrets.require_skill_profiles", false)
	viper.SetDefault("auth_profiles", map[string]any{})

	// Guard (M1).
	viper.SetDefault("guard.enabled", true)
	viper.SetDefault("guard.network.url_fetch.allowed_url_prefixes", []string{"https://"})
	viper.SetDefault("guard.network.url_fetch.deny_private_ips", true)
	viper.SetDefault("guard.network.url_fetch.follow_redirects", false)
	viper.SetDefault("guard.network.url_fetch.allow_proxy", false)
	viper.SetDefault("guard.redaction.enabled", true)
	viper.SetDefault("guard.redaction.patterns", []map[string]any{})
	viper.SetDefault("guard.bash.require_approval", true)
	viper.SetDefault("guard.audit.jsonl_path", "")
	viper.SetDefault("guard.audit.rotate_max_bytes", int64(100*1024*1024))
	viper.SetDefault("guard.approvals.enabled", true)
	viper.SetDefault("guard.approvals.sqlite_dsn", "")
}
