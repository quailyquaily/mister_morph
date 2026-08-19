---
date: 2026-08-07
title: 非 Trigger 群消息写入 Journal
status: implemented
---

# 非 Trigger 群消息写入 Journal

## 1) 需求

Telegram、Slack、LINE、Lark 的群消息只有通过 group trigger 后才进入 main run。未触发消息通常不会持久化；部分消息只会进入进程内 history。

每个支持群聊的 channel 都需要一个默认关闭的选项，把该 channel 中有效但未触发 main run 的群消息写入统一 journal。记录只保留会话事实，不保存 trigger 诊断、LLM 内容或完整附件信息。

## 2) 最小方案

每个支持群聊的 channel 增加同名布尔配置：

```yaml
telegram:
  record_untriggered: false

slack:
  record_untriggered: false

line:
  record_untriggered: false

lark:
  record_untriggered: false
```

四个开关彼此独立，默认都是 `false`。某个 channel 只有在自身开关为 `true` 且 runtime 正在运行时才记录。没有全局 fallback，也不增加 CLI flag。

这里的 `untriggered` 专指未通过 group trigger admission，不包含无效、未授权或 command 消息。

持久化群聊内容涉及不同的隐私范围和消息量，因此开关由 channel 配置拥有。共享 journal 只负责存储，不决定哪些 channel 可以写入。

新事件使用：

```text
domain = conversation
type = untriggered_inbound
schema_version = 1
```

不能复用 `memory/record`，因为未触发消息没有 task、final output，也不应进入 memory projection。不能改写运行日志，因为日志会轮转，也不是业务事实源。增加一个小的 conversation event 是这里最少且语义正确的改动。

## 3) “未触发”的定义

以 main run admission 结果为准，不以最终是否出现文字回复为准。

应记录：

- 已通过现有入口校验并到达 group trigger admission 的人类群消息。
- `strict`、`smart` 或 `talkative` 判断返回 `accepted=false` 的消息。
- Telegram 的 reply-without-mention 前置拒绝。
- addressing 阶段添加了 reaction，但没有创建 main run 的消息。

不记录：

- 已被 main run 接纳的消息，即使最后只有 reaction 或空文本。
- 私聊、command path、outbound、bot 和平台系统消息。
- 无效、未授权，以及已被现有入口重复过滤拦截的消息。
- group trigger 报错或超时的消息；这种情况不是一次正常拒绝，只写错误日志。

开关只增加持久化，不改变 trigger、reaction 或进程内 history 行为。

本功能不新增去重状态。平台重试是否会被识别，沿用各 channel runtime 现有的入口行为；不提供更强的跨重试去重保证。

## 4) 最小事件结构

```json
{
  "id": "evt_01",
  "time": "2026-08-07T12:34:56.789Z",
  "domain": "conversation",
  "type": "untriggered_inbound",
  "schema_version": 1,
  "trace": {
    "runtime": "telegram"
  },
  "payload": {
    "channel": "telegram",
    "conversation_key": "tg:-100000000001_8",
    "message_id": "42",
    "sender_id": "10001",
    "sent_at": "2026-08-07T12:34:55Z",
    "text": "今晚的发布推迟到九点。"
  }
}
```

Payload 只包含：

| 字段 | 必需 | 含义 |
| --- | --- | --- |
| `channel` | 是 | `telegram`、`slack`、`line` 或 `lark` |
| `conversation_key` | 是 | 现有 history scope；包含 Telegram topic 或 Slack thread |
| `message_id` | 是 | 平台消息 ID |
| `sender_id` | 否 | 平台能提供时保存用户 ID |
| `sent_at` | 是 | 平台消息时间，UTC RFC3339Nano |
| `text` | 否 | 文本或 caption |
| `text_truncated` | 否 | 文本被截断时为 `true` |
| `has_attachment` | 否 | 存在附件时为 `true` |

envelope 的 `time` 是 append 时间，payload 的 `sent_at` 是平台消息时间。

为限制单条事件大小，文本固定最多保存 2048 bytes，不提供配置项。截断不能切断 UTF-8 code point。只去掉首尾空白并统一换行，不改正文内部格式。

附件只保存 `has_attachment=true`。不下载文件，不保存附件名、类型、URL、token 或本地路径。文本和附件都为空时不写事件。

不保存 sender name、reply 正文、mention 列表、trigger mode、addressing 分数、reason、prompt、history、memory、LLM request/response 或 token usage。

## 5) 写入流程

```text
platform inbound
  -> 现有入口校验
  -> command path
  -> group trigger
       -> accepted: 现有 main run 路径
       -> rejected: 开关开启时 append conversation event
                    -> return，不创建 task
```

在 `internal/channelruntime/core` 放一个共享 recorder，负责字段校验、文本裁剪和 `domainjournal.Event` 构造。各 channel 只映射已有消息字段，并在现有拒绝分支调用它。这个 recorder 包含实际的数据约束，不是对 `Journal.Append` 的改名包装。

recorder 使用现有 journal 目录、segment、rotation 和同步写入规则。每个 runtime 把自己的开关传给 recorder；一个 channel 的配置不能影响其他 channel。recorder 独立于 `memory.enabled` 和 `tasks.persistence_targets`。

## 6) 必须同时修正的 Cursor 问题

conversation event 会和 memory、task event 共用 journal。现有部分 projection 只在遇到自己的 domain 时推进 checkpoint。若 journal 中连续出现大量 conversation event，memory timer 或 task restart 会重复扫描它们。

实现本功能前只需统一一条规则：projection 跳过有效的其他 domain 事件时，也要推进并保存“最后扫描位置”；自己的事件解码失败时则不能越过。

具体需要覆盖：

- memory 在本轮得到零条 memory event 时，也能保存已扫描 cursor，避免下次 timer 重扫。
- task projection snapshot 保存最后扫描位置，不只保存最后应用的 task event。

不需要新增 domain index 或另一套 journal。

## 7) 失败和安全

- 开关开启但 journal 无法初始化时，channel runtime 启动失败。
- 单次 append 失败时写 error log，继续接收后续消息；不能因此触发 main run 或向群里发送错误。
- 默认关闭，因为该功能会持久化原本不会写盘的群聊内容。
- 只记录 allowlist 范围内的消息，沿用现有 journal 文件权限。
- 文本单条有固定上限，但消息数量没有上限。开启者需要考虑长期磁盘增长。
- 第一版不通过 observations API、memory projection 或 prompt history 暴露这些事件。

## 8) 实现顺序和测试

每个阶段先写测试，再实现。

1. 修正多 domain cursor。
   - memory/task 跳过 conversation event 后 cursor 前进。
   - 自己 domain 的坏 payload 不推进 checkpoint。
2. 增加配置和共享 recorder。
   - 四个 channel 默认分别关闭。
   - 只开启一个 channel 时，其他 channel 不写事件。
   - envelope、payload、UTF-8 截断和空字段符合约定。
3. 接入四个 channel 的拒绝分支。
   - rejected 恰好写一条。
   - accepted、unauthorized、command、trigger error 不写。
   - Telegram 覆盖 reply-without-mention 和 topic；Slack 覆盖 thread。
4. 更新 feature 文档、配置总表和示例配置，运行 `go test ./...`、`go vet ./...`。

## 9) 验收条件

1. 默认配置下行为不变。
2. 某个 channel 开启后，只记录该 channel 的有效未触发群消息。
3. 一个 channel 的开关不会改变其他 channel 的写入行为。
4. 每次正常拒绝最多写入一个小事件，不创建 task，不写 memory，不调用额外 LLM。
5. 文本不超过 2048 bytes，附件不下载。
6. conversation event 不改变 memory/task projection 结果，也不会造成重复扫描。
7. trigger、reaction 和进程内 history 行为不变。

## 10) 非目标

- 完整 turn journal。
- 从 journal 恢复 prompt history。
- 自动提升为长期 memory。
- conversation 搜索 API、UI、index 或 retention。
- per-conversation 策略。
