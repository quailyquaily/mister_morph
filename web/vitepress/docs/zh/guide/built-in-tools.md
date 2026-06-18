---
title: 内置工具
description: 静态工具、运行时注入工具与通道专属工具。
---

# 内置工具

Mistermorph 的工具不是一次性全部固定注册，而是按运行环境分层注入的：

1. 静态工具：只依赖配置和目录上下文，启动时即可注册。
2. Engine 工具：在某次 agent engine 装配时注册。
3. 运行时工具：需要活跃的 LLM client/model 或任务上下文。
4. 专属工具：只会出现在 Telegram、Slack 这类具体运行环境中。

## 工具分组速览

| 分组 | 什么时候出现 | 工具 |
|---|---|---|
| 静态工具 | 仅靠配置即可创建 | `read_file`、`write_file`、`bash`、`powershell`、`url_fetch`、`web_search`、`contacts_send` |
| Engine 工具 | 某次 agent engine 装配完成后可用 | `spawn`、`coder`、`acp_spawn` |
| 运行时工具 | 当 LLM 或者依赖的上下文可用时 | `plan_create`、`todo_update`、`image_generate`、`image_edit` |
| 通道专属工具 | 当前正在使用 Telegram / Slack 等具体 Channel | `telegram_send_voice`、`telegram_send_photo`、`telegram_send_file`、`message_react` |

## 静态工具

`workspace_dir`、`file_cache_dir`、`file_state_dir` 的关系，见 [文件系统根目录](/zh/guide/filesystem-roots)。

Shell 的默认启用状态按平台区分：

- Linux / macOS：默认启用 `bash`，默认关闭 `powershell`。
- Windows：默认启用 `powershell`，默认关闭 `bash`。
- 两者都可以继续通过 `tools.<name>.enabled` 显式覆盖。

### `read_file`

读取本地文本文件。Agent 会用它查看配置、日志、缓存结果、`SKILL.md` 或状态文件。

关键限制：会受 `tools.read_file.deny_paths` 限制；支持 `file_cache_dir/...` 和 `file_state_dir/...` 别名。

### `write_file`

写入本地文件，支持覆盖或追加，用于生成中间结果、更新状态文件或把下载结果转存到本地。

关键限制：只能写到 `file_cache_dir` / `file_state_dir` 下；相对路径默认落到 `file_cache_dir`；大小受 `tools.write_file.max_bytes` 限制。

### `bash`

执行本地 `bash` 命令，调用已有 CLI、做一次性格式转换、跑脚本或查询系统信息。

关键限制：会受 `deny_paths` 和内部 deny-token 规则限制；`bash` 启动的子进程只继承白名单环境变量。
当前隔离执行行为：支持显式 `run_in_subtask=true`，把命令放进一层 direct boundary 里执行；如果当前 runtime 暴露了流式 sink，stdout/stderr 会在命令结束前先出现在预览流里。

### `powershell`

执行本地 PowerShell 命令。它是面向 Windows 的 shell 工具，适合调用已有 CLI、跑脚本或查询本地环境。

关键限制：可通过 `tools.powershell.enabled` 关闭；会受 `deny_paths` 和内部 deny-token 规则限制；子进程只继承白名单环境变量。
当前行为：支持和 `bash` 相同的 `file_cache_dir` / `file_state_dir` 别名，也支持 `file_cache_dir\foo.txt` 这类反斜杠路径写法。
当前差异：暂不支持 `run_in_subtask=true`。

### `url_fetch`

发起 HTTP(S) 请求并返回响应，或把响应下载到本地缓存文件。支持 `GET/POST/PUT/PATCH/DELETE`、`download_path`、`auth_profile`。

关键限制：敏感请求头会被拦截；请求仍受 Guard 网络策略约束。

### `web_search`

做网页搜索并返回结构化结果列表，适合先找线索、候选页面和公开资料入口。

关键限制：返回的是搜索结果摘要，不是整页正文；结果数受 `tools.web_search.max_results` 和代码上限控制。

### `contacts_send`

向单个联系人发送一条外发消息，底层会根据联系人资料选择 Telegram / Slack / LINE 等可用通道。

关键限制：某些 group/supergroup 场景会默认隐藏该工具。

## Engine 工具

这些工具会在某次 agent engine 装配时注册。它们依赖当前 engine 的运行状态，因此不属于静态 base registry。

### `spawn`

启动一个拥有独立上下文和显式工具白名单的 Subagent。父 agent 会同步等待内部执行结束，并收到统一的结构化 JSON envelope。

关键限制：可通过 `tools.spawn.enabled` 关闭；内部 agent 只能使用参数 `tools` 里显式列出的工具；默认不会把原始 transcript 回灌给父 loop。

`spawn` 支持可选 `observe_profile` 参数。`default` 会保守处理运行中预览，`long_shell` 适合长命令和日志，`web_extract` 会先压制原始高噪声输出，等后续阶段信号或更高层摘要。

参数细节、返回 envelope 字段、测试 prompt，以及它和 `bash.run_in_subtask=true` 的差别，见 [Subagents](/zh/guide/subagents)。

### `acp_spawn`

通过配置好的 profile 启动一个外部 ACP agent。父 agent 仍然同步等待，但内部执行走的是 ACP，会话和回调也由 ACP client 处理，而不是再起一个本地 Mister Morph loop。

关键限制：可通过 `tools.acp_spawn.enabled` 关闭；必须能在 `acp.agents` 里找到对应 profile；当前只支持 `stdio`。

当前行为：一次 `acp_spawn` 调用会创建一个 ACP session，处理文件和终端回调，并返回和其他隔离任务路径相同的 `SubtaskResult` envelope。

profile 配置、运行时行为和 Codex 适配层示例，见 [ACP](/zh/guide/acp)。

### `coder`

用本机 Codex 或 Claude Code CLI 运行 coding 子任务。CLI stdout 会按 streaming JSON/JSONL 读取，文本增量会先作为 tool-output event 发出，结束后再返回 `SubtaskResult` envelope。

关键限制：默认通过 `tools.coder.enabled=false` 关闭，但 `$coder` 可以只为当前任务暴露它；只支持 `coder=codex` 或 `coder=claude`；本地 CLI 会以 bypass approval / permission 的方式运行。如果 CLI 不在服务进程 PATH 里，设置 `tools.coder.path_extra`。

- Codex 默认路径：`codex exec --dangerously-bypass-approvals-and-sandbox --json -C <cwd> -`
- Claude 默认路径：`claude -p <task> --output-format stream-json --verbose --include-partial-messages --no-session-persistence --dangerously-skip-permissions`

Codex / Claude Code 委托优先用它。`acp_spawn` 保留给真正需要 ACP 协议的 agent。

## 运行时工具

这些工具会在 Agent 运行时动态注入。

### `plan_create`

生成结构化执行计划 JSON，通常用于复杂任务拆解。

关键限制：步骤数受 `tools.plan_create.max_steps` 控制。

### `todo_update`

维护 `file_state_dir/cron.yaml` 里的待办事项。

关键限制：新增一次性待办事项使用 `action=add_once`，需要 `content` 和 `at`；新增重复待办事项使用 `action=add_recurring`，需要 `content` 和五段数字 `cron`。新增时可以传可选的 `title`。删除待办事项使用 `action=delete`，优先按 `id` 删除；没有 `id` 时按 `content` 做语义匹配，找不到或匹配过多都会报错。

待办事项与 `HEARTBEAT.md` 的运行流程见 [待办事项与 Heartbeat](/zh/guide/todo-and-heartbeat)。

### `image_generate`

根据提示词生成一张图片，并保存到 `file_cache_dir` 或 `workspace_dir`。

关键限制：只有在 `tools.image_generate.enabled=true`、图像模型已配置，并且当前任务有明确图像意图时才会注册。

参数：`prompt` 必填。`output_path` 可选，支持 `workspace_dir/...` 和 `file_cache_dir/...` 别名；相对路径会保存到 `file_cache_dir/images/` 下。

输出文件会写成 PNG 或 JPEG；最终扩展名会按返回的 MIME 类型修正。

### `image_edit`

根据提示词编辑一张本地图片，并把结果保存到 `file_cache_dir` 或 `workspace_dir`。

关键限制：只有在 `tools.image_edit.enabled=true`、图像模型已配置，并且当前任务有图像编辑意图或保留的图像状态时才会注册。

参数：`prompt` 必填。可以用 `input_path` 指定输入图片；如果当前会话有 active image，也可以设置 `use_active_image=true`。`output_path` 规则同 `image_generate`。

输入图片必须能从 `workspace_dir` 或 `file_cache_dir` 读取，格式必须是 PNG 或 JPEG，大小最多 20 MB。

## 专属工具

这些工具不会在普通 CLI / 通用 embedding 场景中出现，只有对应通道 runtime 具备上下文时才会注入。

### `telegram_send_voice`

把本地语音文件发回当前 Telegram 会话，适合发送已经生成好的语音结果。

关键限制：只支持本地文件发送；文件通常应位于 `file_cache_dir`；不负责实时文字转语音。

### `telegram_send_photo`

把本地图片以内联照片形式发回 Telegram。

关键限制：这是「照片」发送，不是「文件」发送；如果你希望对方收到文档附件，应改用 `telegram_send_file`。

### `telegram_send_file`

把本地缓存文件作为文档发送到 Telegram，会保留更像附件的交付形态。

关键限制：只支持本地缓存目录下的文件；目录路径无效；受文件大小上限限制。

### `message_react`

对当前消息添加轻量级 emoji reaction，用于“收到”“赞同”“确认”这类不值得单独发一条文字的场景。

- Telegram 版本：对 Telegram 消息加 emoji reaction，可带 `is_big`。
- Slack 版本：对 Slack 消息加 emoji name reaction，要求使用 Slack emoji 名称而不是原始 Unicode emoji。

关键限制：参数形状会随通道不同而变化；如果当前 runtime 没有对应消息上下文，这个工具就不会出现或会要求显式参数。

## Core 嵌入里的白名单

可通过 `integration.Config.BuiltinToolNames` 限制内置工具。

```go
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "todo_update"}
```

为空表示启用全部内置工具。

## 配置入口

```yaml
tools:
  read_file: ...
  write_file: ...
  spawn: ...
  coder: ...
  acp_spawn: ...
  bash: ...
  powershell: ...
  url_fetch: ...
  web_search: ...
  contacts_send: ...
  todo_update: ...
  plan_create: ...
```

Console 的 Setup / Settings 页面，以及 `/api/settings/agent` 的 `tools` payload，也使用同一套嵌套结构，例如 `tools.spawn.enabled`、`tools.coder.enabled` 和 `tools.acp_spawn.enabled`。

完整的配置请参考 [配置字段](/zh/guide/config-reference.md)。
