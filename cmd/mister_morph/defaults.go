package main

import (
	"time"

	"github.com/spf13/viper"
)

func initViperDefaults() {
	// Shared agent defaults (used by serve/telegram when flags aren't available).
	viper.SetDefault("provider", "openai")
	viper.SetDefault("endpoint", "https://api.openai.com")
	viper.SetDefault("model", "gpt-4o-mini")
	viper.SetDefault("api_key", "")
	viper.SetDefault("llm.request_timeout", 90*time.Second)

	viper.SetDefault("max_steps", 15)
	viper.SetDefault("parse_retries", 2)
	viper.SetDefault("max_token_budget", 0)
	viper.SetDefault("timeout", 10*time.Minute)
	viper.SetDefault("plan.mode", "auto")

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
	viper.SetDefault("telegram.base_url", "https://api.telegram.org")
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
}
