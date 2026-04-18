---
title: 設定フィールド
description: config.yaml の完全フィールドリファレンス。
---

# 設定フィールド

基準ファイル: `assets/config/config.example.yaml`。

すべてのキーは env で上書きできます（`MISTER_MORPH_...`）。詳細は[環境変数](/ja/guide/env-vars-reference)。

## グローバル

- `user_agent`

## LLM

- `llm.provider`
- `llm.model`
- `llm.endpoint`
- `llm.api_key`
- `llm.headers.<name>`（任意のカスタム HTTP ヘッダー）
- `llm.request_timeout`
- `llm.temperature`（任意）
- `llm.reasoning_effort`
- `llm.reasoning_budget_tokens`（任意。`openai_resp` では warning を出して無視）
- `llm.tools_emulation_mode`（`off|fallback|force`）
- `llm.azure.deployment`
- `llm.bedrock.aws_key`
- `llm.bedrock.aws_secret`
- `llm.bedrock.region`
- `llm.bedrock.model_arn`
- `llm.cloudflare.account_id`
- `llm.cloudflare.api_token`
- `llm.profiles.<profile>.*`
- `llm.profiles.<profile>.headers.<name>`（profile 単位のカスタムヘッダー）
- `llm.routes.<purpose>`（`main_loop|addressing|heartbeat|plan_create|memory_draft`）
- `llm.routes.<purpose>.profile`
- `llm.routes.<purpose>.candidates[].profile`
- `llm.routes.<purpose>.candidates[].weight`
- `llm.routes.<purpose>.fallback_profiles[]`

## Multimodal

- `multimodal.image.sources`

## Logging

- `logging.level`
- `logging.format`
- `logging.add_source`
- `logging.include_thoughts`
- `logging.include_tool_params`
- `logging.include_skill_contents`
- `logging.max_thought_chars`
- `logging.max_json_bytes`
- `logging.max_string_value_chars`
- `logging.max_skill_content_chars`
- `logging.redact_keys`

## Secrets / Auth Profiles

- `secrets.allow_profiles`
- `auth_profiles.<id>.credential.kind`
- `auth_profiles.<id>.credential.secret`
- `auth_profiles.<id>.allow.url_prefixes`
- `auth_profiles.<id>.allow.methods`
- `auth_profiles.<id>.allow.follow_redirects`
- `auth_profiles.<id>.allow.allow_proxy`
- `auth_profiles.<id>.allow.deny_private_ips`
- `auth_profiles.<id>.bindings.url_fetch.inject.location`
- `auth_profiles.<id>.bindings.url_fetch.inject.name`
- `auth_profiles.<id>.bindings.url_fetch.inject.format`
- `auth_profiles.<id>.bindings.url_fetch.allow_user_headers`
- `auth_profiles.<id>.bindings.url_fetch.user_header_allowlist`

## Guard

- `guard.enabled`
- `guard.dir_name`
- `guard.network.url_fetch.allowed_url_prefixes`
- `guard.network.url_fetch.deny_private_ips`
- `guard.network.url_fetch.follow_redirects`
- `guard.network.url_fetch.allow_proxy`
- `guard.redaction.enabled`
- `guard.redaction.patterns`
- `guard.audit.jsonl_path`
- `guard.audit.rotate_max_bytes`
- `guard.approvals.enabled`

## Tools

Console の Setup / Settings 画面と `/api/settings/agent` も、`tools.<name>.enabled` の同じ入れ子構造を使います。

Shell のデフォルト値はプラットフォームごとに分かれます。

- Linux / macOS: `tools.bash.enabled=true`、`tools.powershell.enabled=false`
- Windows: `tools.bash.enabled=false`、`tools.powershell.enabled=true`
- どちらも明示的に上書きできます。

- `tools.read_file.max_bytes`
- `tools.read_file.deny_paths`
- `tools.write_file.enabled`
- `tools.write_file.max_bytes`
- `tools.spawn.enabled`
- `tools.contacts_send.enabled`
- `tools.todo_update.enabled`
- `tools.plan_create.enabled`
- `tools.plan_create.max_steps`
- `tools.url_fetch.enabled`
- `tools.url_fetch.timeout`
- `tools.url_fetch.max_bytes`
- `tools.url_fetch.max_bytes_download`
- `tools.web_search.enabled`
- `tools.web_search.base_url`
- `tools.web_search.timeout`
- `tools.web_search.max_results`
- `tools.bash.enabled`
- `tools.bash.timeout`
- `tools.bash.max_output_bytes`
- `tools.bash.deny_paths`
- `tools.bash.injected_env_vars`
- `tools.powershell.enabled`
- `tools.powershell.timeout`
- `tools.powershell.max_output_bytes`
- `tools.powershell.deny_paths`
- `tools.powershell.injected_env_vars`

## MCP

- `mcp.servers[].name`
- `mcp.servers[].enable`
- `mcp.servers[].type`（`stdio|http`）
- `mcp.servers[].command`
- `mcp.servers[].args`
- `mcp.servers[].env`
- `mcp.servers[].url`
- `mcp.servers[].headers`
- `mcp.servers[].allowed_tools`

## Memory

- `memory.enabled`
- `memory.dir_name`
- `memory.short_term_days`
- `memory.injection.enabled`
- `memory.injection.max_items`

## Bus / Contacts / Tasks / Skills

- `bus.max_inflight`
- `contacts.dir_name`
- `contacts.proactive.max_turns_per_session`
- `contacts.proactive.session_cooldown`
- `contacts.proactive.failure_cooldown`
- `tasks.dir_name`
- `tasks.persistence_targets`
- `tasks.rotate_max_bytes`
- `tasks.targets.console.heartbeat_topic_id`
- `skills.dir_name`
- `skills.enabled`
- `skills.load`

## Server / Console

- `server.listen`（非推奨）
- `server.auth_token`
- `server.max_queue`
- `console.listen`
- `console.base_path`
- `console.static_dir`
- `console.password`
- `console.password_hash`
- `console.session_ttl`
- `console.managed_runtimes`
- `console.endpoints[].name`
- `console.endpoints[].url`
- `console.endpoints[].auth_token`

## Telegram

- `telegram.bot_token`
- `telegram.allowed_chat_ids`
- `telegram.group_trigger_mode`
- `telegram.addressing_confidence_threshold`
- `telegram.addressing_interject_threshold`
- `telegram.poll_timeout`
- `telegram.task_timeout`
- `telegram.max_concurrency`
- `telegram.serve_listen`

## Slack

- `slack.base_url`
- `slack.bot_token`
- `slack.app_token`
- `slack.allowed_team_ids`
- `slack.allowed_channel_ids`
- `slack.group_trigger_mode`
- `slack.addressing_confidence_threshold`
- `slack.addressing_interject_threshold`
- `slack.task_timeout`
- `slack.max_concurrency`
- `slack.serve_listen`

## LINE

- `line.base_url`
- `line.channel_access_token`
- `line.channel_secret`
- `line.webhook_listen`
- `line.webhook_path`
- `line.allowed_group_ids`
- `line.group_trigger_mode`
- `line.addressing_confidence_threshold`
- `line.addressing_interject_threshold`
- `line.task_timeout`
- `line.max_concurrency`
- `line.serve_listen`

## Lark

- `lark.base_url`
- `lark.app_id`
- `lark.app_secret`
- `lark.webhook_listen`
- `lark.webhook_path`
- `lark.verification_token`
- `lark.encrypt_key`
- `lark.allowed_chat_ids`
- `lark.group_trigger_mode`
- `lark.addressing_confidence_threshold`
- `lark.addressing_interject_threshold`
- `lark.task_timeout`
- `lark.max_concurrency`
- `lark.serve_listen`

## Heartbeat

- `heartbeat.enabled`
- `heartbeat.interval`

## ループ制限とファイル保存

- `max_steps`
- `parse_retries`
- `max_token_budget`
- `tool_repeat_limit`
- `timeout`
- `file_state_dir`
- `file_cache_dir`
- `file_cache.max_age`
- `file_cache.max_files`
- `file_cache.max_total_bytes`
