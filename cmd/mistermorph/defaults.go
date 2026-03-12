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
	viper.SetDefault("llm.cloudflare.account_id", "")
	viper.SetDefault("llm.cloudflare.api_token", "")

	viper.SetDefault("max_steps", 15)
	viper.SetDefault("parse_retries", 2)
	viper.SetDefault("max_token_budget", 0)
	viper.SetDefault("tool_repeat_limit", 3)
	viper.SetDefault("timeout", 10*time.Minute)
	viper.SetDefault("tools.plan_create.enabled", true)
	viper.SetDefault("tools.plan_create.max_steps", 6)

	// Global
	viper.SetDefault("file_state_dir", "~/.morph")
	viper.SetDefault("file_cache_dir", "~/.cache/morph")
	viper.SetDefault("file_cache.max_age", 7*24*time.Hour)
	viper.SetDefault("file_cache.max_files", 1000)
	viper.SetDefault("file_cache.max_total_bytes", int64(512*1024*1024))
	viper.SetDefault("user_agent", "mistermorph/1.0 (+https://github.com/quailyquaily)")

	// Skills
	viper.SetDefault("skills.enabled", true)
	viper.SetDefault("skills.dir_name", "skills")

	// Bus
	viper.SetDefault("bus.max_inflight", 1024)

	// Multimodal
	viper.SetDefault("multimodal.image.sources", []string{"telegram", "line"})

	viper.SetDefault("contacts.dir_name", "contacts")
	viper.SetDefault("contacts.proactive.max_turns_per_session", 6)
	viper.SetDefault("contacts.proactive.session_cooldown", 72*time.Hour)
	viper.SetDefault("contacts.proactive.failure_cooldown", 72*time.Hour)

	// Daemon server
	viper.SetDefault("server.listen", "127.0.0.1:8787")
	viper.SetDefault("server.max_queue", 100)

	// Console server
	viper.SetDefault("console.enabled", true)
	viper.SetDefault("console.listen", "127.0.0.1:9080")
	viper.SetDefault("console.base_path", "/console")
	viper.SetDefault("console.static_dir", "")
	viper.SetDefault("console.password", "")
	viper.SetDefault("console.password_hash", "")
	viper.SetDefault("console.session_ttl", 12*time.Hour)
	viper.SetDefault("console.endpoints", []map[string]any{})

	// Submit client
	viper.SetDefault("submit.wait", false)
	viper.SetDefault("submit.poll_interval", 1*time.Second)

	// Telegram
	viper.SetDefault("telegram.poll_timeout", 30*time.Second)
	viper.SetDefault("telegram.group_trigger_mode", "smart")
	viper.SetDefault("telegram.addressing_confidence_threshold", 0.6)
	viper.SetDefault("telegram.addressing_interject_threshold", 0.6)
	viper.SetDefault("telegram.max_concurrency", 3)

	// Slack
	viper.SetDefault("slack.base_url", "https://slack.com/api")
	viper.SetDefault("slack.bot_token", "")
	viper.SetDefault("slack.app_token", "")
	viper.SetDefault("slack.allowed_team_ids", []string{})
	viper.SetDefault("slack.allowed_channel_ids", []string{})
	viper.SetDefault("slack.task_timeout", 0*time.Second)
	viper.SetDefault("slack.max_concurrency", 3)
	viper.SetDefault("slack.group_trigger_mode", "smart")
	viper.SetDefault("slack.addressing_confidence_threshold", 0.6)
	viper.SetDefault("slack.addressing_interject_threshold", 0.6)

	// LINE.
	viper.SetDefault("line.base_url", "https://api.line.me")
	viper.SetDefault("line.channel_access_token", "")
	viper.SetDefault("line.channel_secret", "")
	viper.SetDefault("line.webhook_listen", "127.0.0.1:18080")
	viper.SetDefault("line.webhook_path", "/line/webhook")
	viper.SetDefault("line.allowed_group_ids", []string{})
	viper.SetDefault("line.task_timeout", 0*time.Second)
	viper.SetDefault("line.max_concurrency", 3)
	viper.SetDefault("line.group_trigger_mode", "smart")
	viper.SetDefault("line.addressing_confidence_threshold", 0.6)
	viper.SetDefault("line.addressing_interject_threshold", 0.6)

	// Lark.
	viper.SetDefault("lark.base_url", "https://open.feishu.cn/open-apis")
	viper.SetDefault("lark.app_id", "")
	viper.SetDefault("lark.app_secret", "")
	viper.SetDefault("lark.webhook_listen", "127.0.0.1:18081")
	viper.SetDefault("lark.webhook_path", "/lark/webhook")
	viper.SetDefault("lark.verification_token", "")
	viper.SetDefault("lark.encrypt_key", "")
	viper.SetDefault("lark.allowed_chat_ids", []string{})
	viper.SetDefault("lark.task_timeout", 0*time.Second)
	viper.SetDefault("lark.max_concurrency", 3)
	viper.SetDefault("lark.group_trigger_mode", "smart")
	viper.SetDefault("lark.addressing_confidence_threshold", 0.6)
	viper.SetDefault("lark.addressing_interject_threshold", 0.6)

	// Heartbeat
	viper.SetDefault("heartbeat.enabled", true)
	viper.SetDefault("heartbeat.interval", 30*time.Minute)

	// Long-term memory (Phase 1)
	viper.SetDefault("memory.enabled", true)
	viper.SetDefault("memory.dir_name", "memory")
	viper.SetDefault("memory.short_term_days", 7)
	viper.SetDefault("memory.injection.enabled", true)
	viper.SetDefault("memory.injection.max_items", 50)

	// Secrets / auth profiles.
	viper.SetDefault("secrets.allow_profiles", []string{})
	viper.SetDefault("auth_profiles", map[string]any{})

	// MCP (Model Context Protocol) servers.
	viper.SetDefault("mcp.servers", []map[string]any{})

	// Guard (M1).
	viper.SetDefault("guard.enabled", true)
	viper.SetDefault("guard.network.url_fetch.allowed_url_prefixes", []string{"https://"})
	viper.SetDefault("guard.network.url_fetch.deny_private_ips", true)
	viper.SetDefault("guard.network.url_fetch.follow_redirects", false)
	viper.SetDefault("guard.network.url_fetch.allow_proxy", false)
	viper.SetDefault("guard.redaction.enabled", true)
	viper.SetDefault("guard.redaction.patterns", []map[string]any{})
	viper.SetDefault("guard.dir_name", "guard")
	viper.SetDefault("guard.audit.jsonl_path", "")
	viper.SetDefault("guard.audit.rotate_max_bytes", int64(100*1024*1024))
	viper.SetDefault("guard.approvals.enabled", false)
}
