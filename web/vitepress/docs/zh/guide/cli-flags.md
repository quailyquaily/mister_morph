---
title: 命令行参数
description: mistermorph 的命令行参数总览。
---

# 命令行参数

## 全局参数

这些参数会被大多数命令继承：

- `--config`：配置文件路径。
- `--log-add-source`：日志里附带源码 `file:line`。
- `--log-format`：日志格式，`text|json`。
- `--log-include-skill-contents`：日志中包含加载的 `SKILL.md` 内容。
- `--log-include-thoughts`：日志中包含模型 thoughts。
- `--log-include-tool-params`：日志中包含工具参数。
- `--log-level`：日志级别，`debug|info|warn|error`。
- `--log-max-json-bytes`：日志里 JSON 参数的最大字节数。
- `--log-max-skill-content-chars`：日志里 `SKILL.md` 的最大字符数。
- `--log-max-string-value-chars`：日志里单个字符串值的最大字符数。
- `--log-max-thought-chars`：日志里 thought 的最大字符数。
- `--log-redact-key`：额外需要脱敏的参数 key，可重复。

## `benchmark`

这个命令可以接一个可选的位置参数 `profile-name`。不传时，会跑默认路由和所有命名的 LLM profile。

- `--json`：以 JSON 输出 benchmark 结果。
- `--timeout`：所选 benchmark 的整体超时；`0` 表示不启用超时。

## `run`

- `--api-key`：API key。
- `--endpoint`：provider 的 base URL。
- `--heartbeat`：只执行一次 heartbeat 检查，忽略 `--task` 和 stdin。
- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。
- `--interactive`：支持 Ctrl-C 暂停并注入额外上下文。
- `--llm-request-timeout`：单次 LLM HTTP 请求超时。
- `--max-steps`：最大 tool-call 步数。
- `--max-token-budget`：累计 token budget 上限。
- `--model`：模型名。
- `--parse-retries`：JSON 解析最大重试次数。
- `--provider`：provider 名称。
- `--skill`：要加载的 skill 名称或 id，可重复。
- `--skills-dir`：skills 根目录，可重复。
- `--skills-enabled`：是否启用配置中的 skills 加载。
- `--spawn-enabled`：是否启用 spawn 工具启动子 agent。
- `--task`：要执行的任务；为空时从 stdin 读取。
- `--timeout`：整体超时。
- `--tool-repeat-limit`：同一个成功工具调用重复过多次后强制收尾输出。

## `console serve`

- `--allow-empty-password`：允许在未设置 `console.password` / `console.password_hash` 时启动 console。
- `--console-base-path`：Console base path。
- `--console-listen`：Console 服务监听地址。
- `--console-session-ttl`：Console bearer token 的 session TTL。
- `--console-static-dir`：SPA 静态文件目录。
- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。

## `telegram`

- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。
- `--telegram-addressing-confidence-threshold`：接受 addressing 判定所需的最小 confidence。
- `--telegram-addressing-interject-threshold`：接受 addressing 判定所需的最小 interject 分数。
- `--telegram-allowed-chat-id`：允许的 chat id，可重复。
- `--telegram-bot-token`：Telegram bot token。
- `--telegram-group-trigger-mode`：群组触发模式，`strict|smart|talkative`。
- `--telegram-max-concurrency`：同时处理的 chat 最大数量。
- `--telegram-poll-timeout`：`getUpdates` 长轮询超时。
- `--telegram-task-timeout`：单条消息的 agent 超时。

## `slack`

- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。
- `--slack-addressing-confidence-threshold`：接受 addressing 判定所需的最小 confidence。
- `--slack-addressing-interject-threshold`：接受 addressing 判定所需的最小 interject 分数。
- `--slack-allowed-channel-id`：允许的 Slack channel id，可重复。
- `--slack-allowed-team-id`：允许的 Slack team id，可重复。
- `--slack-app-token`：Socket Mode 用的 Slack app token。
- `--slack-bot-token`：Slack bot token。
- `--slack-group-trigger-mode`：群组触发模式，`strict|smart|talkative`。
- `--slack-max-concurrency`：同时处理的 Slack 会话最大数量。
- `--slack-task-timeout`：单条消息的 agent 超时。

## `line`

- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。
- `--line-addressing-confidence-threshold`：接受 addressing 判定所需的最小 confidence。
- `--line-addressing-interject-threshold`：接受 addressing 判定所需的最小 interject 分数。
- `--line-allowed-group-id`：允许的 LINE group id，可重复。
- `--line-base-url`：LINE API base URL。
- `--line-channel-access-token`：LINE channel access token。
- `--line-channel-secret`：用于 webhook 签名校验的 LINE channel secret。
- `--line-group-trigger-mode`：群组触发模式，`strict|smart|talkative`。
- `--line-max-concurrency`：同时处理的 LINE 会话最大数量。
- `--line-task-timeout`：单条消息的 agent 超时。
- `--line-webhook-listen`：LINE webhook 服务监听地址。
- `--line-webhook-path`：LINE webhook 回调路径。

## `lark`

- `--inspect-prompt`：把 prompt messages dump 到 `./dump`。
- `--inspect-request`：把 LLM request/response payload dump 到 `./dump`。
- `--lark-addressing-confidence-threshold`：接受 addressing 判定所需的最小 confidence。
- `--lark-addressing-interject-threshold`：接受 addressing 判定所需的最小 interject 分数。
- `--lark-allowed-chat-id`：允许的 Lark chat id，可重复。
- `--lark-app-id`：Lark app id。
- `--lark-app-secret`：Lark app secret。
- `--lark-base-url`：Lark Open API base URL。
- `--lark-encrypt-key`：Lark event subscription encrypt key。
- `--lark-group-trigger-mode`：群组触发模式，`strict|smart|talkative`。
- `--lark-max-concurrency`：同时处理的 Lark 会话最大数量。
- `--lark-task-timeout`：单条消息的 agent 超时。
- `--lark-verification-token`：Lark event subscription verification token。
- `--lark-webhook-listen`：Lark webhook 服务监听地址。
- `--lark-webhook-path`：Lark webhook 回调路径。

## `install`

- `-y, --yes`：跳过确认提示。

## `skills list`

- `--skills-dir`：skills 根目录，可重复。

## `skills install`

- `--clean`：复制前删除已有 skill 目录。
- `--dest`：目标目录。
- `--dry-run`：只打印操作，不写文件。
- `--max-bytes`：远程 `SKILL.md` 下载的最大字节数。
- `--skip-existing`：跳过目标目录里已经存在的文件。
- `--timeout`：下载远程 `SKILL.md` 的超时。
- `-y, --yes`：跳过确认提示。

## `tools`

这个命令当前没有独立参数。
