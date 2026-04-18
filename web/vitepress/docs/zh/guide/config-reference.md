---
title: 配置字段
description: config.yaml 的完整字段参考（逐字段解释）。
---

# 配置字段

权威来源：`assets/config/config.example.yaml`。

所有字段都可用 `MISTER_MORPH_...` 环境变量覆盖，映射规则见[环境变量](/zh/guide/env-vars-reference)。

## 全局

| 字段 | 含义 |
|---|---|
| `user_agent` | 全局 HTTP User-Agent，作用于 `url_fetch`、`web_search` 等外联工具。 |

## LLM

| 字段 | 含义 |
|---|---|
| `llm.provider` | 主模型提供方（如 `openai`、`openai_resp`、`azure`、`bedrock`、`cloudflare` 等）。 |
| `llm.model` | 主循环默认模型名。 |
| `llm.endpoint` | OpenAI 兼容提供方的 API 基础地址。 |
| `llm.api_key` | 提供方 API Key。建议写成 `${ENV_VAR}`。 |
| `llm.headers.<name>` | 可选的自定义 LLM 请求头。 |
| `llm.request_timeout` | 单次 LLM 请求超时时间。 |
| `llm.temperature` | 可选的默认采样温度；未设置时不强制传给提供方。 |
| `llm.reasoning_effort` | 可选推理强度（`none/minimal/low/medium/high/max/xhigh`）。 |
| `llm.reasoning_budget_tokens` | 可选推理预算 token；`openai_resp` 下会忽略并记录 warning。 |
| `llm.tools_emulation_mode` | 工具调用仿真策略（`off/fallback/force`）。 |
| `llm.azure.deployment` | Azure OpenAI 的 deployment 名称。 |
| `llm.bedrock.aws_key` | Bedrock 的 AWS Access Key。 |
| `llm.bedrock.aws_secret` | Bedrock 的 AWS Secret Key。 |
| `llm.bedrock.region` | Bedrock 区域。 |
| `llm.bedrock.model_arn` | Bedrock 模型 ARN。 |
| `llm.cloudflare.account_id` | Cloudflare Workers AI 账号 ID。 |
| `llm.cloudflare.api_token` | Cloudflare Workers AI API Token。 |
| `llm.profiles.<profile>.*` | 命名 LLM 配置档；可覆盖 provider/model/key 等，用于路由不同任务。 |
| `llm.profiles.<profile>.headers.<name>` | profile 级自定义请求头；同名 header 会覆盖顶层 `llm.headers`。 |
| `llm.routes.<purpose>` | route 定义；`purpose` 支持 `main_loop/addressing/heartbeat/plan_create/memory_draft`。 |
| `llm.routes.<purpose>.profile` | 固定把该 route 绑定到一个 profile。 |
| `llm.routes.<purpose>.candidates[].profile` | 该 route 参与分流的 profile。 |
| `llm.routes.<purpose>.candidates[].weight` | 该候选 profile 的权重；当前 run 内只会选中一个主候选。 |
| `llm.routes.<purpose>.fallback_profiles[]` | 该 route 的本地回退链；主候选失败后按顺序尝试。 |

## Multimodal

| 字段 | 含义 |
|---|---|
| `multimodal.image.sources` | 允许图像输入的来源白名单（如 `telegram`、`line`、`remote_download`）。 |

## Logging

| 字段 | 含义 |
|---|---|
| `logging.level` | 日志级别（`debug/info/warn/error`）。 |
| `logging.format` | 日志格式（`text/json`）。 |
| `logging.add_source` | 是否在日志中附带源码位置（文件:行）。 |
| `logging.include_thoughts` | 是否输出模型 thoughts（可能包含敏感信息）。 |
| `logging.include_tool_params` | 是否记录工具调用参数（会脱敏/截断）。 |
| `logging.include_skill_contents` | 是否记录已加载 `SKILL.md` 内容（截断后）。 |
| `logging.max_thought_chars` | thoughts 日志最大字符数。 |
| `logging.max_json_bytes` | 参数 JSON 日志最大字节数。 |
| `logging.max_string_value_chars` | 日志中单个字符串值的最大长度。 |
| `logging.max_skill_content_chars` | `SKILL.md` 记录时最大字符数。 |
| `logging.redact_keys` | 额外脱敏键名列表。 |

## Secrets 与 Auth Profiles

| 字段 | 含义 |
|---|---|
| `secrets.allow_profiles` | 允许运行时使用的认证 profile 白名单。 |
| `auth_profiles.<id>.credential.kind` | 凭证类型（如 api key/bearer）。 |
| `auth_profiles.<id>.credential.secret` | 实际密钥内容，建议 `${ENV_VAR}`。 |
| `auth_profiles.<id>.allow.url_prefixes` | 允许访问的 URL 前缀白名单。 |
| `auth_profiles.<id>.allow.methods` | 允许的 HTTP 方法白名单。 |
| `auth_profiles.<id>.allow.follow_redirects` | 是否允许跟随重定向。 |
| `auth_profiles.<id>.allow.allow_proxy` | 是否允许走系统代理。 |
| `auth_profiles.<id>.allow.deny_private_ips` | 是否禁止私网/本地地址。 |
| `auth_profiles.<id>.bindings.url_fetch.inject.location` | 凭证注入位置（例如 header/query）。 |
| `auth_profiles.<id>.bindings.url_fetch.inject.name` | 注入字段名（例如 `Authorization`）。 |
| `auth_profiles.<id>.bindings.url_fetch.inject.format` | 注入格式（例如 bearer）。 |
| `auth_profiles.<id>.bindings.url_fetch.allow_user_headers` | 是否允许用户再额外传 header。 |
| `auth_profiles.<id>.bindings.url_fetch.user_header_allowlist` | 允许用户自带的 header 白名单。 |

## Guard

| 字段 | 含义 |
|---|---|
| `guard.enabled` | 是否启用 Guard 安全层。 |
| `guard.dir_name` | Guard 状态目录名（位于 `file_state_dir` 下）。 |
| `guard.network.url_fetch.allowed_url_prefixes` | `url_fetch` 可访问目的地前缀白名单。 |
| `guard.network.url_fetch.deny_private_ips` | 是否拒绝私网/本地 IP 目标。 |
| `guard.network.url_fetch.follow_redirects` | `url_fetch` 是否允许重定向。 |
| `guard.network.url_fetch.allow_proxy` | `url_fetch` 是否允许代理。 |
| `guard.redaction.enabled` | 是否启用输出脱敏。 |
| `guard.redaction.patterns` | 自定义脱敏正则模式。 |
| `guard.audit.jsonl_path` | 审计日志 JSONL 路径（空则使用默认目录）。 |
| `guard.audit.rotate_max_bytes` | 审计日志滚动大小阈值。 |
| `guard.approvals.enabled` | 是否启用审批流程。 |

## Tools

Console 的 Setup / Settings 页面，以及 `/api/settings/agent`，复用同一套 `tools.<name>.enabled` 嵌套结构。

Shell 默认值按平台区分：

- Linux / macOS：`tools.bash.enabled=true`，`tools.powershell.enabled=false`
- Windows：`tools.bash.enabled=false`，`tools.powershell.enabled=true`
- 两者都可以继续显式覆盖。

| 字段 | 含义 |
|---|---|
| `tools.read_file.max_bytes` | `read_file` 单次最大读取字节数。 |
| `tools.read_file.deny_paths` | `read_file` 拒绝读取的路径/文件名列表。 |
| `tools.write_file.enabled` | 是否启用 `write_file`。 |
| `tools.write_file.max_bytes` | `write_file` 单次最大写入字节数。 |
| `tools.spawn.enabled` | 是否启用 `spawn` 工具。它只控制显式 `spawn` 入口，不影响 subtask 运行机制本身。 |
| `tools.contacts_send.enabled` | 是否启用 `contacts_send`。 |
| `tools.todo_update.enabled` | 是否启用 `todo_update`。 |
| `tools.plan_create.enabled` | 是否启用 `plan_create`。 |
| `tools.plan_create.max_steps` | `plan_create` 默认最大步骤数。 |
| `tools.url_fetch.enabled` | 是否启用 `url_fetch`。 |
| `tools.url_fetch.timeout` | `url_fetch` 请求超时。 |
| `tools.url_fetch.max_bytes` | `url_fetch` 直接返回时的最大读取字节数。 |
| `tools.url_fetch.max_bytes_download` | `url_fetch` 下载到文件时的最大读取字节数。 |
| `tools.web_search.enabled` | 是否启用 `web_search`。 |
| `tools.web_search.base_url` | 搜索后端地址（当前默认 DuckDuckGo HTML）。 |
| `tools.web_search.timeout` | `web_search` 请求超时。 |
| `tools.web_search.max_results` | `web_search` 默认返回条数上限。 |
| `tools.bash.enabled` | 是否启用 `bash`（高风险能力）。 |
| `tools.bash.timeout` | `bash` 单次执行超时。 |
| `tools.bash.max_output_bytes` | `bash` 每个输出流最大保留字节数。 |
| `tools.bash.deny_paths` | `bash` 命令中禁止引用的路径列表。 |
| `tools.bash.injected_env_vars` | 额外注入给 `bash` 子进程的环境变量白名单。 |
| `tools.powershell.enabled` | 是否启用 `powershell`（高风险能力）。 |
| `tools.powershell.timeout` | `powershell` 单次执行超时。 |
| `tools.powershell.max_output_bytes` | `powershell` 每个输出流最大保留字节数。 |
| `tools.powershell.deny_paths` | `powershell` 命令中禁止引用的路径列表。 |
| `tools.powershell.injected_env_vars` | 额外注入给 `powershell` 子进程的环境变量白名单。 |

## MCP

| 字段 | 含义 |
|---|---|
| `mcp.servers[].name` | MCP 服务器标识名；会参与工具命名空间。 |
| `mcp.servers[].enable` | 是否启用该 MCP 服务器配置。 |
| `mcp.servers[].type` | 传输类型（`stdio` 或 `http`）。 |
| `mcp.servers[].command` | `stdio` 模式下的启动命令。 |
| `mcp.servers[].args` | `stdio` 模式命令参数。 |
| `mcp.servers[].env` | `stdio` 模式传给子进程的环境变量。 |
| `mcp.servers[].url` | `http` 模式 MCP Endpoint。 |
| `mcp.servers[].headers` | `http` 模式自定义请求头（支持 `${ENV_VAR}`）。 |
| `mcp.servers[].allowed_tools` | 该服务器允许暴露的工具白名单；空表示全部。 |

## Memory

| 字段 | 含义 |
|---|---|
| `memory.enabled` | 是否启用 memory 子系统。 |
| `memory.dir_name` | memory 目录名（位于 `file_state_dir` 下）。 |
| `memory.short_term_days` | 注入短期记忆时回看天数窗口。 |
| `memory.injection.enabled` | 是否把记忆摘要注入系统 prompt。 |
| `memory.injection.max_items` | 单次注入的最大记忆条目数。 |

## Bus / Contacts / Tasks / Skills

| 字段 | 含义 |
|---|---|
| `bus.max_inflight` | 进程内消息总线最大并发在途数。 |
| `contacts.dir_name` | 联系人业务状态目录名。 |
| `contacts.proactive.max_turns_per_session` | 主动会话最大轮次。 |
| `contacts.proactive.session_cooldown` | 主动会话轮次耗尽后的冷却时长。 |
| `contacts.proactive.failure_cooldown` | 主动发送失败后的冷却时长。 |
| `tasks.dir_name` | task 持久化目录名。 |
| `tasks.persistence_targets` | 启用任务文件持久化的目标运行时。 |
| `tasks.rotate_max_bytes` | 任务日志/状态文件滚动大小阈值。 |
| `tasks.targets.console.heartbeat_topic_id` | console 心跳保留 topic id。 |
| `skills.dir_name` | skills 根目录名。 |
| `skills.enabled` | 是否启用 skills 加载。 |
| `skills.load` | 预加载 skill 列表；空列表表示加载全部已发现 skill。 |

## Server 与 Console

| 字段 | 含义 |
|---|---|
| `server.listen` | 已废弃；旧版共享监听地址回退项。 |
| `server.auth_token` | 运行时 API 鉴权 token（Bearer）。 |
| `server.max_queue` | 任务队列最大长度。 |
| `console.listen` | Console API + 静态资源监听地址。 |
| `console.base_path` | Console 路由基础路径。 |
| `console.static_dir` | Console 静态站点目录（生产构建产物）。 |
| `console.password` | Console 明文密码（生产不推荐）。 |
| `console.password_hash` | Console bcrypt 密码哈希。 |
| `console.session_ttl` | Console 会话 token 有效期。 |
| `console.managed_runtimes` | 由 console 进程托管的通道 runtime 列表（如 telegram/slack）。 |
| `console.endpoints[].name` | 外部 runtime endpoint 显示名称。 |
| `console.endpoints[].url` | 外部 runtime endpoint 地址。 |
| `console.endpoints[].auth_token` | 访问该 endpoint 的鉴权 token。 |

## Telegram

| 字段 | 含义 |
|---|---|
| `telegram.bot_token` | Telegram Bot Token。 |
| `telegram.allowed_chat_ids` | 允许访问的 chat id 白名单；空表示不限制。 |
| `telegram.group_trigger_mode` | 群聊触发策略（`strict/smart/talkative`）。 |
| `telegram.addressing_confidence_threshold` | addressing 判定通过所需最小置信度。 |
| `telegram.addressing_interject_threshold` | Telegram addressing 的 interject 上限阈值。 |
| `telegram.poll_timeout` | 长轮询超时。 |
| `telegram.task_timeout` | 单条消息任务超时（`0s` 表示沿用上层）。 |
| `telegram.max_concurrency` | 并发处理 chat 的上限。 |
| `telegram.serve_listen` | Telegram runtime API 监听地址。 |

## Slack

| 字段 | 含义 |
|---|---|
| `slack.base_url` | Slack Web API 基础地址（通常不用改）。 |
| `slack.bot_token` | Slack Bot Token（xoxb）。 |
| `slack.app_token` | Slack Socket Mode App Token（xapp）。 |
| `slack.allowed_team_ids` | 允许 workspace 白名单。 |
| `slack.allowed_channel_ids` | 允许 channel 白名单（也可用于心跳通知目标）。 |
| `slack.group_trigger_mode` | 群聊触发策略（`strict/smart/talkative`）。 |
| `slack.addressing_confidence_threshold` | addressing 判定通过所需最小置信度。 |
| `slack.addressing_interject_threshold` | Slack addressing 的 interject 下限阈值。 |
| `slack.task_timeout` | 单条消息任务超时。 |
| `slack.max_concurrency` | 会话并发处理上限。 |
| `slack.serve_listen` | Slack runtime API 监听地址。 |

## LINE

| 字段 | 含义 |
|---|---|
| `line.base_url` | LINE Messaging API 基础地址。 |
| `line.channel_access_token` | LINE Channel Access Token。 |
| `line.channel_secret` | LINE Webhook 签名校验密钥。 |
| `line.webhook_listen` | LINE webhook 监听地址。 |
| `line.webhook_path` | LINE webhook 路由路径。 |
| `line.allowed_group_ids` | 允许 group 白名单；空表示不限制。 |
| `line.group_trigger_mode` | 群聊触发策略（`strict/smart/talkative`）。 |
| `line.addressing_confidence_threshold` | addressing 判定通过所需最小置信度。 |
| `line.addressing_interject_threshold` | LINE addressing 的 interject 下限阈值。 |
| `line.task_timeout` | 单条消息任务超时。 |
| `line.max_concurrency` | 会话并发处理上限。 |
| `line.serve_listen` | LINE runtime API 监听地址。 |

## Lark

| 字段 | 含义 |
|---|---|
| `lark.base_url` | Lark/飞书 Open API 基础地址。 |
| `lark.app_id` | Lark App ID。 |
| `lark.app_secret` | Lark App Secret。 |
| `lark.webhook_listen` | Lark webhook 监听地址。 |
| `lark.webhook_path` | Lark webhook 路由路径。 |
| `lark.verification_token` | 事件订阅校验 token。 |
| `lark.encrypt_key` | 事件订阅加密 key。 |
| `lark.allowed_chat_ids` | 允许 chat 白名单；空表示不限制。 |
| `lark.group_trigger_mode` | 群聊触发策略（`strict/smart/talkative`）。 |
| `lark.addressing_confidence_threshold` | addressing 判定通过所需最小置信度。 |
| `lark.addressing_interject_threshold` | Lark addressing 的 interject 下限阈值。 |
| `lark.task_timeout` | 单条消息任务超时。 |
| `lark.max_concurrency` | 会话并发处理上限。 |
| `lark.serve_listen` | Lark runtime API 监听地址。 |

## Heartbeat

| 字段 | 含义 |
|---|---|
| `heartbeat.enabled` | 是否启用心跳机制。 |
| `heartbeat.interval` | 心跳执行间隔。 |

## 循环限制与文件目录

| 字段 | 含义 |
|---|---|
| `max_steps` | 单次任务最多工具调用步数。 |
| `parse_retries` | 模型输出 JSON 解析失败时的重试次数。 |
| `max_token_budget` | 累计 token 预算上限（`0` 表示不限制）。 |
| `tool_repeat_limit` | 同名工具在单任务中的重复成功调用上限。 |
| `timeout` | 整个任务运行超时。 |
| `file_state_dir` | 运行状态根目录（memory/skills/tasks 等）。 |
| `file_cache_dir` | 文件缓存根目录（下载文件、媒体临时文件等）。 |
| `file_cache.max_age` | 缓存文件最大保留时长。 |
| `file_cache.max_files` | 缓存文件数量上限。 |
| `file_cache.max_total_bytes` | 缓存总大小上限。 |
