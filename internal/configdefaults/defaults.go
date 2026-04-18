package configdefaults

import (
	"time"

	"github.com/quailyquaily/mistermorph/internal/platformutil"
	"github.com/spf13/viper"
)

// Apply sets all shared defaults used by CLI and desktop console mode.
func Apply(v *viper.Viper) {
	if v == nil {
		return
	}

	v.SetDefault("llm.provider", "openai")
	v.SetDefault("llm.endpoint", "")
	v.SetDefault("llm.model", "")
	v.SetDefault("llm.api_key", "")
	v.SetDefault("llm.request_timeout", 90*time.Second)
	v.SetDefault("llm.tools_emulation_mode", "off")
	v.SetDefault("llm.cloudflare.account_id", "")
	v.SetDefault("llm.cloudflare.api_token", "")

	v.SetDefault("max_steps", 15)
	v.SetDefault("parse_retries", 2)
	v.SetDefault("max_token_budget", 0)
	v.SetDefault("tool_repeat_limit", 3)
	v.SetDefault("timeout", 10*time.Minute)
	v.SetDefault("tools.plan_create.enabled", true)
	v.SetDefault("tools.plan_create.max_steps", 6)

	v.SetDefault("file_state_dir", "~/.morph")
	v.SetDefault("file_cache_dir", "~/.cache/morph")
	v.SetDefault("file_cache.max_age", 7*24*time.Hour)
	v.SetDefault("file_cache.max_files", 1000)
	v.SetDefault("file_cache.max_total_bytes", int64(512*1024*1024))
	v.SetDefault("user_agent", "mistermorph/1.0 (+https://github.com/quailyquaily)")

	v.SetDefault("skills.enabled", true)
	v.SetDefault("skills.dir_name", "skills")

	v.SetDefault("tasks.dir_name", "tasks")
	v.SetDefault("tasks.persistence_targets", []string{"console"})
	v.SetDefault("tasks.rotate_max_bytes", int64(64*1024*1024))
	v.SetDefault("tasks.targets.console.heartbeat_topic_id", "_heartbeat")

	v.SetDefault("bus.max_inflight", 1024)

	v.SetDefault("multimodal.image.sources", []string{"telegram", "line"})

	v.SetDefault("contacts.dir_name", "contacts")
	v.SetDefault("contacts.proactive.max_turns_per_session", 6)
	v.SetDefault("contacts.proactive.session_cooldown", 72*time.Hour)
	v.SetDefault("contacts.proactive.failure_cooldown", 72*time.Hour)

	v.SetDefault("server.max_queue", 100)

	v.SetDefault("console.enabled", true)
	v.SetDefault("console.listen", "127.0.0.1:9080")
	v.SetDefault("console.base_path", "/")
	v.SetDefault("console.static_dir", "")
	v.SetDefault("console.password", "")
	v.SetDefault("console.password_hash", "")
	v.SetDefault("console.session_ttl", 12*time.Hour)
	v.SetDefault("console.endpoints", []map[string]any{})

	v.SetDefault("submit.wait", false)
	v.SetDefault("submit.poll_interval", 1*time.Second)

	v.SetDefault("telegram.poll_timeout", 30*time.Second)
	v.SetDefault("telegram.group_trigger_mode", "smart")
	v.SetDefault("telegram.addressing_confidence_threshold", 0.6)
	v.SetDefault("telegram.addressing_interject_threshold", 0.6)
	v.SetDefault("telegram.max_concurrency", 3)
	v.SetDefault("telegram.serve_listen", "")

	v.SetDefault("slack.base_url", "https://slack.com/api")
	v.SetDefault("slack.bot_token", "")
	v.SetDefault("slack.app_token", "")
	v.SetDefault("slack.allowed_team_ids", []string{})
	v.SetDefault("slack.allowed_channel_ids", []string{})
	v.SetDefault("slack.task_timeout", 0*time.Second)
	v.SetDefault("slack.max_concurrency", 3)
	v.SetDefault("slack.group_trigger_mode", "smart")
	v.SetDefault("slack.addressing_confidence_threshold", 0.6)
	v.SetDefault("slack.addressing_interject_threshold", 0.6)
	v.SetDefault("slack.serve_listen", "")

	v.SetDefault("line.base_url", "https://api.line.me")
	v.SetDefault("line.channel_access_token", "")
	v.SetDefault("line.channel_secret", "")
	v.SetDefault("line.webhook_listen", "127.0.0.1:18080")
	v.SetDefault("line.webhook_path", "/line/webhook")
	v.SetDefault("line.allowed_group_ids", []string{})
	v.SetDefault("line.task_timeout", 0*time.Second)
	v.SetDefault("line.max_concurrency", 3)
	v.SetDefault("line.group_trigger_mode", "smart")
	v.SetDefault("line.addressing_confidence_threshold", 0.6)
	v.SetDefault("line.addressing_interject_threshold", 0.6)
	v.SetDefault("line.serve_listen", "")

	v.SetDefault("lark.base_url", "https://open.feishu.cn/open-apis")
	v.SetDefault("lark.app_id", "")
	v.SetDefault("lark.app_secret", "")
	v.SetDefault("lark.webhook_listen", "127.0.0.1:18081")
	v.SetDefault("lark.webhook_path", "/lark/webhook")
	v.SetDefault("lark.verification_token", "")
	v.SetDefault("lark.encrypt_key", "")
	v.SetDefault("lark.allowed_chat_ids", []string{})
	v.SetDefault("lark.task_timeout", 0*time.Second)
	v.SetDefault("lark.max_concurrency", 3)
	v.SetDefault("lark.group_trigger_mode", "smart")
	v.SetDefault("lark.addressing_confidence_threshold", 0.6)
	v.SetDefault("lark.addressing_interject_threshold", 0.6)
	v.SetDefault("lark.serve_listen", "")

	v.SetDefault("heartbeat.enabled", true)
	v.SetDefault("heartbeat.interval", 30*time.Minute)

	v.SetDefault("memory.enabled", true)
	v.SetDefault("memory.dir_name", "memory")
	v.SetDefault("memory.short_term_days", 7)
	v.SetDefault("memory.injection.enabled", true)
	v.SetDefault("memory.injection.max_items", 50)

	v.SetDefault("secrets.allow_profiles", []string{})
	v.SetDefault("auth_profiles", map[string]any{})

	v.SetDefault("mcp.servers", []map[string]any{})

	v.SetDefault("guard.enabled", true)
	v.SetDefault("guard.network.url_fetch.allowed_url_prefixes", []string{"https://"})
	v.SetDefault("guard.network.url_fetch.deny_private_ips", true)
	v.SetDefault("guard.network.url_fetch.follow_redirects", false)
	v.SetDefault("guard.network.url_fetch.allow_proxy", false)
	v.SetDefault("guard.redaction.enabled", true)
	v.SetDefault("guard.redaction.patterns", []map[string]any{})
	v.SetDefault("guard.dir_name", "guard")
	v.SetDefault("guard.audit.jsonl_path", "")
	v.SetDefault("guard.audit.rotate_max_bytes", int64(100*1024*1024))
	v.SetDefault("guard.approvals.enabled", false)

	v.SetDefault("logging.level", "")
	v.SetDefault("logging.format", "text")
	v.SetDefault("logging.add_source", false)
	v.SetDefault("logging.include_thoughts", true)
	v.SetDefault("logging.include_tool_params", true)
	v.SetDefault("logging.include_skill_contents", false)
	v.SetDefault("logging.max_thought_chars", 2000)
	v.SetDefault("logging.max_json_bytes", 32*1024)
	v.SetDefault("logging.max_string_value_chars", 2000)
	v.SetDefault("logging.max_skill_content_chars", 8000)

	v.SetDefault("tools.read_file.max_bytes", 256*1024)
	v.SetDefault("tools.read_file.deny_paths", []string{"config.yaml"})

	v.SetDefault("tools.write_file.enabled", true)
	v.SetDefault("tools.write_file.max_bytes", 512*1024)
	v.SetDefault("tools.spawn.enabled", true)
	v.SetDefault("tools.acp_spawn.enabled", false)

	// Platform-specific shell tool defaults:
	// - Windows: PowerShell enabled, Bash disabled
	// - Unix-like (Linux/macOS): Bash enabled, PowerShell disabled
	if platformutil.IsWindows() {
		v.SetDefault("tools.bash.enabled", false)
		v.SetDefault("tools.powershell.enabled", true)
	} else {
		v.SetDefault("tools.bash.enabled", true)
		v.SetDefault("tools.powershell.enabled", false)
	}
	v.SetDefault("tools.bash.timeout", 30*time.Second)
	v.SetDefault("tools.bash.max_output_bytes", 256*1024)
	v.SetDefault("tools.bash.deny_paths", []string{"config.yaml"})
	v.SetDefault("tools.bash.injected_env_vars", []string{})

	v.SetDefault("tools.powershell.timeout", 30*time.Second)
	v.SetDefault("tools.powershell.max_output_bytes", 256*1024)
	v.SetDefault("tools.powershell.deny_paths", []string{"config.yaml"})
	v.SetDefault("tools.powershell.injected_env_vars", []string{})

	v.SetDefault("tools.url_fetch.enabled", true)
	v.SetDefault("tools.url_fetch.timeout", 30*time.Second)
	v.SetDefault("tools.url_fetch.max_bytes", int64(512*1024))
	v.SetDefault("tools.url_fetch.max_bytes_download", int64(100*1024*1024))

	v.SetDefault("tools.web_search.enabled", true)
	v.SetDefault("tools.web_search.timeout", 20*time.Second)
	v.SetDefault("tools.web_search.max_results", 5)
	v.SetDefault("tools.web_search.base_url", "https://duckduckgo.com/html/")

	v.SetDefault("tools.contacts_send.enabled", true)
	v.SetDefault("tools.todo_update.enabled", true)

	v.SetDefault("acp.agents", []map[string]any{})
}
