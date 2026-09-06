---
date: 2026-09-04
title: Complete Web Settings
status: implemented
---

# Complete Web Settings

## 1. 背景

`config.yaml` 已经包含模型、工具、Channel、调度、安全、存储和 Console 等配置，但 Web UI 只覆盖其中一部分。用户仍需在表单和手工编辑 YAML 之间切换，也很难判断一个配置是否已经生效。

当前 Web UI 已支持：

- 默认 LLM、部分 Provider 字段和 named profile；
- main loop 的 fallback profiles；
- 10 个工具的启用开关；
- Skills 和 MCP；
- Channel 凭据、allowlist 和 group trigger mode；
- managed runtimes；
- Guard 的基础开关和 URL Fetch 网络规则；
- Persona、OAuth 登录、手动检查更新等独立功能。

主要缺口不是前端少了几个输入框。现有 Settings API 也只读写上述字段。完整实现需要同时扩展配置读取、字段来源识别、校验、写回、secret 处理和运行时生效状态。

## 2. 目标

1. `assets/config/config.example.yaml` 中所有公开且仍受支持的配置项，都能在 Web UI 中查看和修改。CLI 专用选项和 legacy alias 不进入 Web UI。
2. Web UI 保存一个字段时，保留其他字段、注释、顺序和未知扩展字段。
3. Secret 明文永远不从 Settings API 返回给浏览器。
4. 正确区分配置文件值、默认值、环境变量覆盖、`${ENV_VAR}`、`${aws-sm:...}` 和 `${secret:...}`。
5. 正确区分“未配置”“显式为空”“显式为零”“显式 false”和“使用默认值”。
6. 本地 endpoint 与远程 endpoint 使用相同的配置语义。修改哪个 endpoint，就只修改哪个 endpoint 的配置。
7. 保存后明确显示配置是立即生效、对下一批任务生效、需要 runtime restart，还是需要整个进程 restart。需要 restart 的字段在输入控件旁直接显示 `Restart required`。
8. 配置校验失败时不写入文件，也不替换当前有效 runtime snapshot。

## 3. 非目标

- 不提供一个直接编辑完整 YAML 的浏览器文本框。
- 不把启动 flag、运行时统计或派生值伪装成持久化配置。
- 不把所有表单自动生成为通用 JSON Schema UI。
- 不新增 secret backend。继续使用系统密钥管理器、环境变量、AWS Secrets Manager 和已有的明文 fallback。
- 不允许浏览器读取操作系统密钥管理器中的原始 secret。
- 不在本需求中重构整个 runtime generation 架构。
- 不为了页面分组而新增一个 API endpoint。

## 4. 基本原则

### 4.1 `config.yaml` 是持久化真相

Settings API 只负责：

1. 读取当前配置文档；
2. 返回适合 UI 使用的安全视图；
3. 合并用户提交的部分更新；
4. 校验候选配置；
5. 原子写回 `config.yaml`；
6. 返回新的持久化版本和生效状态。

运行时继续从新的配置快照读取值。Settings handler 不应直接维护第二份长期状态。

### 4.2 只提交发生变化的字段

GET 可以返回完整 section。PUT 只能提交当前 section 中实际变化的字段。禁止把整个表单快照无条件写回，因为这会：

- 把缺省值全部物化到配置文件；
- 覆盖用户刚刚在外部编辑的内容；
- 把隐藏字段改回旧值；
- 混淆 absent、empty、zero 和 false。

每个 Settings 响应返回 `config_revision`。Web UI 保存时带回该值。服务端发现文件已经变化时返回 `409 Conflict`，前端重新加载并提示用户。`config_revision` 可以直接使用原始文件内容的 hash，不需要单独维护数据库 revision。

### 4.3 复用现有 API 边界

继续扩展现有的：

- `/settings/agent`：LLM、agent limits、context compaction、chat、tools、skills、MCP、ACP；
- `/settings/console`：managed runtimes、Channels、Guard、admins、heartbeat、cron 和 Console 自身配置；
- `/settings/auto-update`：自动更新策略；

缺少明确归属的系统设置可以增加一个 `/settings/system`，承载 logging、paths、file cache、tasks、contacts、bus、server 等配置。不要为每张 UI 卡片增加独立 endpoint。

所有 endpoint 仍通过现有 endpoint-aware 请求路径访问。目标 Morph 负责读取、保存和应用自己的配置；控制端不解析或保存远端 secret。

### 4.4 使用明确的表单，不使用通用 YAML 编辑器

不同字段需要不同交互：duration、bytes、enum、path、secret、字符串列表、键值表和动态对象不能都用同一个文本框。可以复用字段组件和校验逻辑，但页面仍由明确的产品语义组成。

## 5. 当前覆盖情况

| 配置域 | 当前 Web UI | 主要缺失 |
| --- | --- | --- |
| LLM | 部分支持 | cache、timeout、temperature、reasoning budget、headers、image、完整 routes、部分 Bedrock 字段 |
| Agent limits | 不支持 | step、retry、token、tool repeat、timeout、context compaction |
| Tools | 只有 10 个 enabled | 其余 enabled、timeout、大小限制、PATH、env、deny paths、rewrite |
| Skills | 部分支持 | `dir_name` |
| MCP | 支持 server 列表 | 无本需求外的缺口 |
| ACP | 不支持 | agent 列表及其权限、cwd、roots、session options |
| Channels | 部分支持 | untriggered journal、threshold、timeout、concurrency、listen 和 Channel 专属字段 |
| Guard | 部分支持 | redaction patterns、audit 路径和轮转、目录 |
| Secrets/Auth | 不支持 | allow profiles、AWS source、auth profiles |
| Automation | 不支持 | heartbeat、cron |
| Console | 部分支持 | endpoints、bind、base path、auth、session TTL |
| Logging | 不支持 | 全部 logging 配置 |
| Storage | 不支持 | workspace/state/cache、domain paths、retention 和 quotas |
| Runtime/system | 不支持 | queue、bus、user agent、admins、默认更新策略 |

保存现有表单时，当前 YAML node 写回方式通常会保留未展示字段。切换 LLM provider 时删除不再适用的 provider-specific 配置，以及删除已经废弃的 Mixin trigger 字段，属于有意行为。

## 6. Settings 信息结构

保持现有 Settings 导航风格，不把每个 YAML mapping 都变成侧栏入口。

Settings 使用渐进展示。页面首屏只显示日常需要确认或修改的字段；低频参数通过其所属对象的 `Advanced settings` 入口打开。高级设置不能集中堆在页面末尾，因为这会割裂参数与其实际控制对象之间的关系。

划分依据不是配置结构的深浅，而是使用频率：

- 基础设置：身份、凭据、模型、allowlist、启用状态等首次配置和日常操作需要的字段；
- 高级设置：timeout、limit、cache、headers、行为阈值、PATH、环境变量、部署监听地址等低频调优字段。

高级设置对话框直接呈现字段，不再嵌套卡片。保存仍调用所属 section 的现有 Settings API，不增加新的 endpoint。

### 6.1 Persona

继续管理 identity、soul 和 avatar。它们不是 `config.yaml` 字段，不纳入本需求的配置覆盖统计。

### 6.2 Agent

页面直接显示：

- Default model；
- Named profiles；
- Fallback profiles。

Default model 和每个 named profile 的操作菜单增加 `Advanced settings`，并放在 `Benchmark` 上方。这个入口只编辑当前模型对象的参数：

- Supports image parts；
- Cache TTL 和 Cache key prefix；
- Request timeout；
- Temperature；
- Reasoning budget tokens；
- HTTP headers；
- Reasoning effort；
- Tools emulation；
- Context window。

Image model、Model routing 和 Execution limits 控制的是 LLM 系统或 Agent loop，而不是 Default model。它们作为独立区域放在 LLM Settings 页面，不能混入 Default model 的高级设置。Context compaction 通过 LLM 菜单中的同名对话框编辑，避免长期占用页面空间。

### 6.3 Tools

列表只显示所有工具的启用状态。`read_file` 是始终启用的基础工具，也显示一个 disabled、值为 on 的开关，以保持列表结构一致。每个有额外参数的工具在开关左侧显示一个图标按钮，点击后使用对话框编辑该工具的详细参数。没有额外参数的工具只显示开关，也不显示空的高级设置入口。

工具的高级设置必须按工具拆分。例如 Bash 对话框只包含 Bash timeout、输出限制、deny paths、PATH、环境变量和 command rewrite，不能同时出现 PowerShell 或其他工具的字段。

移动端仍保持工具名称在左、操作和开关在右的单行结构，不把每个 item 改成上下排列。

MCP 页面默认只显示 Server 列表和 Add 按钮。Add 和 Edit 都使用对话框，保存动作属于单个 MCP Server，因此页面不再提供全局 Save。ACP 和 Skills storage 不提供 Web UI 入口；它们仍可由配置文件管理。

### 6.4 Channels

每个 Channel 使用一张独立卡片，卡片内部按以下顺序显示：

1. 凭据；
2. allowlist；
3. 群聊触发模式。

每张 Channel 卡片使用与 LLM profile 相同的 More 菜单提供 `Advanced settings`。`record_untriggered`、addressing thresholds、timeout、concurrency、API base、webhook 和 runtime listen 等低频行为或部署字段放入对应 Channel 的对话框，不能跨 Channel 混在同一个面板中。

### 6.5 Automation

集中管理 Heartbeat 和 Cron 总开关。具体 TODO item 仍在 TODO 页面管理。Save 放在 Frame 右上角，不使用悬浮在内容下方的操作栏。

### 6.6 Security

导航和路由名称统一为 Security，包含：

- Guard；
- Admin identities；
- Auth profiles；
- Secret sources。

### 6.7 System

包含运行时和 Agent process 的通用设置：

- Logging；
- Workspace and storage；
- File cache；
- Tasks and Contacts storage；
- Server and queue；
- 原 Console 页面的非部署配置；
- Update policy。

System 的低频参数放在默认收起的 `Advanced settings` 区域，并清楚显示 restart 要求。Save 放在对应 Frame 右上角。

### 6.8 Console

Console 只管理 Console 自身：

- Console deployment；
- Console endpoints；
- Console password。

这三类配置不能分散到 System 或其他页面。

需要 restart 的配置允许保存，但 Web UI 不自动重启进程或 runtime。可能断开当前 Console 连接的修改，保存前需要确认。

## 7. 完整配置清单

以下清单以公开配置模板和运行时仍然读取的配置为准。动态名称使用 `<name>` 表示。

### 7.1 LLM

基础字段：

- `llm.inference_provider`
- `llm.provider`，只在 Advanced 中显示；通常由 `inference_provider` 推导
- `llm.endpoint`
- `llm.model`
- `llm.context_window_tokens`
- `llm.api_key`
- `llm.headers`
- `llm.cache_ttl`
- `llm.cache_key_prefix`
- `llm.request_timeout`
- `llm.temperature`
- `llm.reasoning_effort`
- `llm.reasoning_budget_tokens`
- `llm.pricing_file`
- `llm.tools_emulation_mode`

Provider-specific 字段：

- `llm.azure.deployment`
- `llm.bedrock.aws_key`
- `llm.bedrock.aws_secret`
- `llm.bedrock.aws_session_token`
- `llm.bedrock.aws_profile`
- `llm.bedrock.region`
- `llm.bedrock.model_arn`
- `llm.cloudflare.account_id`
- `llm.cloudflare.api_token`

Image 字段：

- `llm.image.provider`
- `llm.image.endpoint`
- `llm.image.api_key`
- `llm.image.model`
- `llm.image.request_timeout`
- `llm.image.options.openai`
- `llm.image.options.gemini`
- `llm.image.options.cloudflare`

Named profile 支持与默认 LLM 相同的适用字段，并额外支持：

- `llm.profiles.<name>.supports_image_parts`

Routes：

- `llm.routes.main_loop`
- `llm.routes.addressing`
- `llm.routes.awareness`
- `llm.routes.think`
- `llm.routes.plan_create`

每个 route 支持当前运行时接受的简写 profile 字符串，或完整的 `profile`、`candidates`、`weight` 和 `fallback_profiles` 结构。UI 始终保存为一种规范结构，不在同一个 route 内混用两种表示。

### 7.2 Agent execution

- `max_steps`
- `parse_retries`
- `max_token_budget`
- `tool_repeat_limit`
- `timeout`
- `context_compaction.enabled`
- `context_compaction.trigger_ratio`

### 7.3 Tools

- `tools.read_file.max_bytes`
- `tools.read_file.deny_paths`
- `tools.write_file.enabled`
- `tools.write_file.max_bytes`
- `tools.spawn.enabled`
- `tools.coder.enabled`
- `tools.coder.path_extra`
- `tools.acp_spawn.enabled`
- `tools.contacts_send.enabled`
- `tools.todo_update.enabled`
- `tools.plan_create.enabled`
- `tools.plan_create.max_steps`
- `tools.image_generate.enabled`
- `tools.image_edit.enabled`
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
- `tools.bash.path_extra`
- `tools.bash.injected_env_vars`
- `tools.bash.rewrite.enabled`
- `tools.bash.rewrite.binary`
- `tools.powershell.enabled`
- `tools.powershell.timeout`
- `tools.powershell.max_output_bytes`
- `tools.powershell.deny_paths`
- `tools.powershell.injected_env_vars`

`bash.enabled` 和 `powershell.enabled` 必须保留 absent 状态。未显式配置时使用平台默认值，不能把未设置自动保存为 false。

### 7.4 Skills, MCP and ACP

- `skills.enabled`
- `skills.load`
- `skills.dir_name`
- `mcp.servers`
- `acp.agents`

MCP server 继续支持 stdio/http、command、args、env、URL、headers 和 allowed tools。ACP agent 支持 command、args、env、cwd、read roots、write roots、session options 和 adapter-specific options。

### 7.5 Channels

Telegram：

- `telegram.bot_token`
- `telegram.allowed_chat_ids`
- `telegram.group_trigger_mode`
- `telegram.record_untriggered`
- `telegram.addressing_confidence_threshold`
- `telegram.addressing_interject_threshold`
- `telegram.poll_timeout`
- `telegram.task_timeout`
- `telegram.max_concurrency`
- `telegram.serve_listen`

Slack：

- `slack.base_url`
- `slack.bot_token`
- `slack.app_token`
- `slack.allowed_team_ids`
- `slack.allowed_channel_ids`
- `slack.group_trigger_mode`
- `slack.record_untriggered`
- `slack.addressing_confidence_threshold`
- `slack.addressing_interject_threshold`
- `slack.task_timeout`
- `slack.max_concurrency`
- `slack.serve_listen`

LINE：

- `line.base_url`
- `line.channel_access_token`
- `line.channel_secret`
- `line.webhook_listen`
- `line.webhook_path`
- `line.allowed_group_ids`
- `line.group_trigger_mode`
- `line.record_untriggered`
- `line.addressing_confidence_threshold`
- `line.addressing_interject_threshold`
- `line.task_timeout`
- `line.max_concurrency`
- `line.serve_listen`

Lark：

- `lark.base_url`
- `lark.app_id`
- `lark.app_secret`
- `lark.allowed_chat_ids`
- `lark.group_trigger_mode`
- `lark.record_untriggered`
- `lark.addressing_confidence_threshold`
- `lark.addressing_interject_threshold`
- `lark.task_timeout`
- `lark.max_concurrency`
- `lark.serve_listen`

Mixin：

- `mixin.keystore_file`
- `mixin.allowed_conversation_ids`
- `mixin.task_timeout`
- `mixin.max_concurrency`
- `mixin.serve_listen`

Mixin 不显示 Telegram 风格的 trigger、reply 或 untriggered 配置，因为 Mixin Bot 只能收到平台投递给它的消息。

### 7.6 Automation

- `heartbeat.enabled`
- `heartbeat.interval`
- `cron.enabled`

### 7.7 Guard and credentials

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
- `admins`
- `secrets.allow_profiles`
- `secrets.aws_secrets_manager.region`
- `secrets.aws_secrets_manager.profile`
- `auth_profiles.<name>`
- `server.auth_token`

### 7.8 Console and system

- `console.listen`
- `console.base_path`
- `console.static_dir`
- `console.password`
- `console.password_hash`
- `console.session_ttl`
- `console.managed_runtimes`
- `console.endpoints`
- `server.max_queue`
- `bus.max_inflight`
- `user_agent`
- `auto_update.enabled`
- `workspace_dir`
- `file_state_dir`
- `file_cache_dir`
- `file_cache.max_age`
- `file_cache.max_files`
- `file_cache.max_total_bytes`
- `contacts.dir_name`
- `contacts.proactive.failure_cooldown`
- `tasks.dir_name`
- `tasks.persistence_targets`
- `tasks.rotate_max_bytes`
- `logging.level`
- `logging.format`
- `logging.add_source`
- `logging.file.dir`
- `logging.file.max_age`
- `logging.include_thoughts`
- `logging.include_tool_params`
- `logging.include_skill_contents`
- `logging.max_thought_chars`
- `logging.max_json_bytes`
- `logging.max_string_value_chars`
- `logging.max_skill_content_chars`
- `logging.redact_keys`

## 8. 字段状态模型

每个可编辑字段除了安全的显示值，还需要返回状态。状态使用配置路径作为 key，避免为每个 DTO 再复制一组 metadata struct。

```json
{
  "config_revision": "sha256:...",
  "values": {
    "llm": {
      "model": "gpt-5.4",
      "api_key": ""
    }
  },
  "field_states": {
    "llm.model": {
      "source": "file",
      "explicit": true,
      "editable": true,
      "apply_mode": "next_generation"
    },
    "llm.api_key": {
      "source": "os_secret",
      "explicit": true,
      "configured": true,
      "sensitive": true,
      "editable": true,
      "apply_mode": "next_generation"
    }
  }
}
```

`source` 至少支持：

- `default`
- `file`
- `environment_override`
- `config_env_ref`
- `config_aws_ref`
- `config_os_ref`
- `runtime_flag`

`apply_mode` 至少支持：

- `immediate`
- `next_generation`
- `runtime_restart`
- `process_restart`

环境变量覆盖和 runtime flag 优先级高于文件时，字段显示有效值来源，并禁止编辑文件值冒充有效修改。UI 文案应指出具体环境变量或启动参数。非敏感字段可以显示有效值；敏感字段只显示是否已配置和来源。

## 9. 特殊字段处理

### 9.1 Secret

以下字段至少按 secret 处理：

- LLM、named profile 和 image model 的 API keys；
- Bedrock static credentials 和 session token；
- Cloudflare API token；
- Telegram、Slack、LINE、Lark 凭据；
- `server.auth_token`；
- `console.password` 和 `console.password_hash`；
- `console.endpoints[].auth_token`；
- `auth_profiles.<name>.credential.secret`。

规则：

1. GET 永远返回空值或固定 placeholder，不返回明文。
2. 未修改 secret 时，PUT 不包含该字段。
3. 用户输入新值表示 replace。
4. 明确点击 Clear 才表示删除。
5. 新值优先保存到目标 Morph 所在机器的 OS secret store。
6. OS secret store 写入失败时记录平台相关 warning，并按现有规则回退到 0600 `config.yaml`。
7. 配置文件写入成功后才能删除旧 OS secret；配置写入失败时删除刚创建的新 secret。
8. `${ENV_VAR}`、`${aws-sm:...}` 和 `${secret:...}` 只作为引用处理，不把解析结果返回前端。

`console.password_hash` 不提供读取或直接编辑入口。Web UI 使用“Set new password”对话框接收新密码，服务端生成 bcrypt hash，写入 `console.password_hash`，并删除旧的 `console.password` 明文字段。

MCP 的 `env` 和 `headers` 继续作为显式键值配置处理，不在本需求中自动猜测哪些 key 是 secret。用户应使用已有 secret reference 语法，UI 不根据字段名启发式改写值。

### 9.2 Default、empty、zero 和 auto

字段状态必须包含 `explicit`。UI 提供“Use default”动作，其语义是删除 YAML key，而不是写入当前默认值。

特别注意：

- duration `0s` 可能表示继承上层 timeout；
- numeric `0` 可能表示禁用限制；
- empty list 可能表示 allow all，也可能表示 none；
- shell `enabled` absent 表示按平台自动决定；
- empty string 可能表示使用内置 endpoint，也可能表示禁用默认目录。

这些语义必须由具体字段定义，不能用一个全局的“空值即删除”规则。

### 9.3 Duration、bytes 和 ratio

- Duration UI 使用可读字符串，保存前按 Go duration 语法校验。
- Bytes UI 可以显示换算值，但持久化时保持整数 bytes。
- Ratio 使用明确范围和步进，并保留用户输入精度。
- 所有数值在前端和后端都校验；以后端为准。

### 9.4 Paths

- 保留用户输入的相对路径，不把它改写成本机绝对路径。
- 相对路径继续按现有配置目录或 runtime 规则解析。
- 远程 endpoint 的路径选择器只能浏览远端允许的目录。
- UI 不把控制端本地路径提交给远端 endpoint。

### 9.5 Lists, maps and dynamic objects

- 简单字符串列表使用可增删的 row/chip editor。
- headers、env 和 options 使用 key/value editor。
- profiles、MCP servers、ACP agents、auth profiles 和 Console endpoints 使用稳定名称识别现有 YAML node。
- 常用字段使用明确的表单；`llm.image.options.*`、ACP `session_options` 和 `auth_profiles` 中无法固定建模的对象使用 JSON object editor。
- rename 时提交 `original_name`，避免删除并重建整个 node。
- 重排只改变目标列表顺序，不重写列表项中的未知字段和注释。
- `tools.bash.injected_env_vars` 和 PowerShell 同名字段支持字符串与 `{name, value}` 两种成员，不能把对象压成字符串。

### 9.6 Legacy fields

Legacy alias 不进入 Web UI。保存当前字段时也不能借机扫描或清理不相关的 legacy 字段。已有的 Mixin 配置保存逻辑可以继续删除同一 section 中已经废弃的 trigger 字段；`llm.provider` 只有 Advanced override 需要时才显式写入。

## 10. 保存和生效流程

一次保存按以下顺序执行：

1. 获取进程内配置写锁；
2. 重新读取 `config.yaml`；
3. 校验 `config_revision`；
4. 解析本次实际变化的字段；
5. 为新增 secret 准备 OS secret references；
6. 在 YAML AST 上合并 patch；
7. 构建并校验候选配置；
8. 以 0600 权限原子替换配置文件；
9. 删除不再被引用的旧 OS secrets；
10. 等待或观察 config watcher 发布新的 generation；
11. 返回新的 revision 和 apply 状态。

任何一步失败都不能留下引用不存在的新配置，也不能删除仍在使用的旧 secret。

保存响应至少说明：

```json
{
  "ok": true,
  "config_revision": "sha256:...",
  "apply_mode": "runtime_restart",
  "apply_status": "pending",
  "restart_targets": ["telegram"]
}
```

如果字段需要整个进程 restart，Web UI 在保存前和保存后都明确显示。不要假装它已经对当前进程生效。

## 11. 远程 endpoint

1. URL 中的 endpoint ref 决定 Settings 请求目标。
2. `/e/default/...` 修改当前 Console 的本地配置。
3. `/e/<remote>/...` 只修改该远端 Morph 的配置。
4. Secret replace 请求发送到目标 Morph，由目标 Morph 写入自己的 secret store。
5. OS secret opaque id 不跨机器复制。
6. 远端不支持某个配置域时返回 capability 信息，前端隐藏或禁用对应 section；不能回退修改本地配置。
7. local 与 remote 对共同字段使用相同 DTO、校验和错误语义。

## 12. 错误处理

- `400 Bad Request`：字段类型、枚举、范围或组合无效。
- `409 Conflict`：配置 revision 已变化，或目标 runtime 正在进行互斥配置操作。
- `422 Unprocessable Entity`：YAML 可以解析，但候选 runtime 配置无效。
- `503 Service Unavailable`：目标 endpoint 或所需外部 secret source 当前不可用。

错误响应包含稳定 error code、字段路径和简洁文案。不得包含 secret、底层 credential backend 原始输出或完整配置。

## 13. 性能要求

1. Settings 继续按 section 延迟加载，不在进入页面时请求所有动态数据。
2. 输入字段时只更新当前 section 的 dirty state。
3. 不在每次输入时序列化完整配置或全部 profiles。
4. Secret source、MCP/ACP discovery 和远端 health check 不阻塞基础表单渲染。
5. 保存只校验一次完整候选配置；请求路径不反复读取 secret store。

## 14. 测试要求

正式实现按 phase 先写测试。

后端至少覆盖：

1. 每个公开配置域的 GET/PUT round trip。
2. 修改一个字段时，所有未修改字段、注释、顺序和未知字段保持不变。
3. absent、empty、zero、false 和 reset-to-default 的区别。
4. 所有 secret source 的安全响应和 replace/clear 行为。
5. OS secret 写入与配置文件写入之间的失败回滚。
6. env-managed 和 flag-managed 字段不可被 Web UI 覆盖。
7. revision conflict 返回 409，不覆盖外部修改。
8. 动态对象 rename、reorder、delete 时保留无关字段。
9. 本地与远端 endpoint 修改严格隔离。
10. hot reload、runtime restart 和 process restart 状态正确。
11. 设置新 Console 密码时只写 bcrypt hash，并删除旧的 `console.password` 明文字段。

前端至少覆盖数据转换、dirty tracking、reset、secret 操作和 endpoint 路由。纯视觉布局不新增测试。

## 15. 实施顺序

### Phase 1：安全的配置编辑基础

- [x] 增加 `config_revision` 冲突检查。
- [x] 定义统一 `field_states`。
- [x] 定义部分更新和 reset 语义。
- [x] 把 secret replace/clear 事务用于所有 secret 字段。
- [x] 增加 untouched YAML preservation 回归测试。

### Phase 2：补齐高频行为配置

- [x] Agent execution limits。
- [x] Context compaction 和 chat display。
- [x] Channel trigger、journal、timeout、concurrency 和 thresholds。
- [x] 缺失的 tool enabled 开关和 `plan_create.max_steps`。
- [x] Heartbeat 和 Cron 总开关。

### Phase 3：补齐 LLM

- [x] cache、request timeout、temperature、reasoning budget 和 headers。
- [x] Bedrock session token 和 profile。
- [x] Image model。
- [x] 完整 named profile 字段。
- [x] 完整 routes。

### Phase 4：补齐工具和集成

- [x] Bash、PowerShell、URL Fetch、Web Search、Read/Write File 详细参数。
- [x] ACP agents。
- [x] Auth profiles 和 secret sources。
- [x] Admin identities。

### Phase 5：补齐系统设置

- [x] Logging。
- [x] Paths、file cache、tasks 和 contacts storage。
- [x] Console endpoints 和 update policy。
- [x] Console/server deployment fields，并显示 restart 要求。
- [x] Bus、queue 和 user agent。

### Phase 6：完整性检查

- [x] 逐项核对公开配置模板。
- [x] 删除 UI 中已经失效的 legacy 字段。
- [x] 验证每个字段的 source、reset 和 apply mode。
- [x] 验证 local/remote 行为一致。
- [x] 更新配置文档和 VitePress。

### Phase 7：简化 Settings 首屏

- [x] Default model 和 named profile 的菜单提供 `Advanced settings`，并置于 `Benchmark` 上方。
- [x] Model behavior 和其他低频 Agent 配置不再平铺在 Agent 页面。
- [x] Tool 列表只保留启用状态；有详细参数的 Tool 在开关左侧提供对话框入口。
- [x] 每个 Channel 独立提供高级设置入口，不再显示合并的 Channel behavior 面板。
- [x] 高级设置对话框复用现有字段状态、partial update、secret 和 restart 语义。
- [x] 桌面端和移动端均可完整访问高级设置，且对话框内不嵌套卡片。
- [x] Console 前端构建通过。

### Phase 8：统一 Settings 对象层级

- [x] Default model 的 Advanced settings 只包含模型参数；Image、Routes 和 Agent loop 参数回到 LLM 页面独立区域。
- [x] Tools 列表为 `read_file` 显示 disabled on 开关，并修正移动端左右布局和 Frame margin。
- [x] MCP 改为 Server 列表，使用 Add/Edit 对话框逐项保存，移除页面 Save。
- [x] 移除 ACP 的 Web UI 入口和 Skills storage Frame。
- [x] Channel 的高级设置改用 More 菜单。
- [x] Guard 导航与路由改名为 Security。
- [x] System 和 Automation 的 Save 放在 Frame 右上角，移除浮动操作栏。
- [x] 修正 Automation Frame margin。
- [x] Console 页面只保留 deployment、endpoints 和 password；其余系统配置移入 System。
- [x] System 高级设置默认收起。
- [x] Console 前端构建通过。

### Phase 9：统一 Settings 编辑对话框

- [x] 增加 `SettingDialog`，统一标题、单一滚动区和底部操作栏。
- [x] Cancel 固定在左侧，Save 固定在右侧；保存期间禁止关闭和重复提交。
- [x] 高级设置、MCP 编辑、Console password 和头像裁切使用统一组件。
- [x] 移除调用方重复的 padding、滚动容器和 Save/Cancel 按钮。
- [x] Cancel 丢弃未保存的对话框草稿。
- [x] 认证、测试和选择器等非设置编辑对话框不使用该组件。
- [x] Console 前端测试与构建通过。

### Phase 10：精简字段状态和 LLM 页面

- [x] 移除 LLM Pricing 设置界面。
- [x] 环境变量托管的通用配置字段使用只读托管状态展示，不再显示输入框。
- [x] 移除 `Configured in config.yaml`、`Using default` 和非密钥字段的 `Use default` 操作。
- [x] Context compaction 保留完整配置能力，但改为从 LLM 菜单打开，不再平铺；删除已经失效的 `chat.compact_mode`。
- [x] 输出预留量改由运行时计算，删除 `context_compaction.output_reserve_tokens` 配置。
- [x] `image_generate` 和 `image_edit` 并入标准 Tool 列表，删除 `Additional tools` 专用区域。

## 16. 验收标准

1. 公开配置模板中的每个受支持字段，都能在 Web UI 找到明确入口。
2. 用户不需要为普通配置操作手工编辑 YAML。
3. Web UI 永远看不到 secret 明文。
4. 保存任一 section 不会改变未编辑配置。
5. 外部修改配置后，旧页面不能静默覆盖新内容。
6. 所有字段都能说明当前值来源和生效方式。
7. 远程 endpoint 与本地 endpoint 功能一致，并且配置严格隔离。
8. 需要 restart 的修改不会被显示为已经生效。
9. 配置失败不会破坏当前可运行的配置或遗留无效 secret reference。
10. 配置模板、Settings API 和 Web UI 的字段覆盖保持一致。
