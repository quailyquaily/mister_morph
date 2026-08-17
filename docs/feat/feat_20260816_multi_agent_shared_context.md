---
date: 2026-08-16
updated: 2026-08-17
title: 基于 Channel 的多 Agent 协作
status: in_progress
---

# 基于 Channel 的多 Agent 协作

## 1. 目标

让运行在不同 endpoint 上的 Morph Agent 可以按人的安排依次完成任务，同时复用现有通信能力。

第一版不建立团队、共享状态或远端任务协议。最小模型是：

```text
Contact(kind: agent)
+ Contact reference
+ agent_send
+ Channel message
```

人通过任务中的 Agent 引用指定当前负责人和后续接收者。当前 Agent 完成自己的部分后，使用 `agent_send` 向下一位 Agent 发送普通消息。目标 Agent 把它作为普通 Channel task 处理。

这是一种异步消息交接。发送方只获得现有工具的发送结果，不等待目标 Agent 返回结构化结果。

## 2. 术语

本文统一使用 **Agent** 表示参与协作的 Morph 实例。

Telegram 和 Slack 的 API 仍会使用 `Bot`、`bot_id`、`is_bot`、`bot_message` 和 Bot-to-Bot Communication 等平台术语。Morph 收到这类平台身份后，将其表示为 `Contact(kind: agent)`。平台术语不进入 Morph 的协作模型。

把平台账号识别为 Agent 只是在 Contacts 中记录身份和地址，不会让它自动参与任务。任务仍然必须由人或当前 Agent 明确引用目标 Agent。

## 3. 复用现有能力

协作只需要解决四件事：

| 问题 | 现有能力 |
| --- | --- |
| 对方是谁 | Contact 的 `contact_id` |
| 如何联系 | Contact 中已有的 Channel 地址 |
| 何时执行 | 消息中的第一个 Contact 引用 |
| 需要知道什么 | 当前消息和可见的 Channel history |

不新增 Agent Card、Agent store、coordinator、共享 Scope 或 Agent RPC。

### 3.1 Contact

继续使用现有 Contact 数据。协作只关心：

```text
contact_id
kind: agent
channel
现有平台地址
```

不增加：

- `capabilities`；
- `accepted_input`；
- `produced_output`；
- 组织关系；
- 自动选人规则。

平台明确标记发送者为 Agent 账号时，Contact observation 使用 `kind: agent`。已经记录为 `kind: agent` 的 Contact 不能被后续普通观察降级为 `human`。

`persona_brief` 等现有字段可以继续存在，但不参与协作寻址和任务分配。

### 3.2 Contact 引用

`@name` 是给人看的输入或显示形式，不是稳定身份。真正的身份继续使用 Morph 已有的引用格式：

```text
[name](protocol:id)
```

例如：

```text
[John](tg:@john_bot)
[Smith](slack:T123:U456)
```

同一平台的原生 mention 在进入 Morph 后归一化为 Contact 引用。Console 中的 Contact 选择器也应写入同一种引用。这样可以引用位于其他 endpoint 或其他 Channel 的 Agent，不需要增加 Agent alias 表。

原生 mention 只能引用当前平台的账号。跨 Channel 任务必须携带带 `contact_id` 的 Contact 引用，不能只依赖可能重名的 `@nickname`。

寻址分为两种：

- 已由平台 mention 或 Contact 选择器解析的 `@name` 只指定 Agent，内部可以表示为 `[name](contact:id)`。`agent_send` 按现有规则选择可用 Connection，允许使用其他可用 Channel；
- `[name](protocol:id)` 明确指定 Channel 和地址。该地址不可用时直接返回错误，不能静默改用其他 Channel。

任务明确要求使用某个 Channel 时，必须使用第二种形式。

## 4. 执行规则

### 4.1 当前负责人

一条群聊协作消息中的第一个 Contact 引用是当前负责人。私聊的接收者已经由 Channel 确定，不再要求正文重复引用自己。

```text
这个方案由 [John](tg:@john_bot) 设计，完成后交给 [Smith](slack:T123:U456) 实施。
```

群聊 runtime 只检查第一项是否指向自己：

- 指向自己：按普通 Channel task 执行；
- 不指向自己：不触发 task；
- 后续引用：只作为任务内容，不立即触发对应 Agent。

runtime 不需要加载全部 Agent Contacts，也不需要判断每个引用是不是 Agent。任务作者负责把当前负责人放在第一项。需要并行执行时，分别发送多条消息。

没有正文 mention 的普通消息继续使用现有 Channel trigger 规则。群聊正文包含 mention 时，无论发送者是人还是 Agent，都按第一项判断负责人；当前 Morph 排在后面时不会触发。私聊消息直接由接收方处理。

### 4.2 Handoff

当前 Agent 只有在任务明确引用了下一位 Agent 时，才进行 handoff。

Handoff 使用专用工具：

```text
agent_send(contact_id, message_text)
```

`message_text` 是普通文本，不定义 JSON schema 或固定模板。它只需让目标 Agent 成为第一项引用，并包含足够继续执行的上下文。

`agent_send` 与 `contacts_send` 使用相同的参数 schema、路由、批量发送、消息封装和返回结构。两者只在工具名称、说明和接收者限制上不同。

运行时仅在 `contacts/ACTIVE.md` 至少存在一个 `kind: agent` 的 Contact 时注册 `agent_send`。每次执行仍须重新校验全部目标都来自 `ACTIVE.md` 且为 `kind: agent`。任一目标不符合时，整次调用失败，不发送部分消息。协议地址也必须映射到 active Agent，不能用任意 `tg:`、`slack:` 等地址绕过检查。

`agent_send` 不增加配置项。`contacts_send` 继续使用原有注册规则；Telegram 和 Slack 群聊继续移除 `contacts_send`，但可以保留满足上述条件的 `agent_send`。

示例：

```text
[Smith](slack:T123:U456)，设计已经完成。方案位于……，请按方案实施，重点检查……
```

发送结果直接沿用 `contacts_send` 的现有返回值，不增加 collaboration outcome。

### 4.3 接收与继续

目标 Agent 收到消息后，走现有 Channel task 流程，包括 history、memory、guard、approval、journal 和回复。

完成后可以：

- 在当前会话回复结果；
- 把结果交给任务中明确引用的下一位 Agent；
- 不再发送 handoff，结束任务。

发送方不挂起原 task 等待目标 Agent，也不恢复远端 tool call。第一版不记录 call graph，不汇总远端 progress、approval 或 task status。

### 4.4 上下文

同一会话中的 Agent 可以读取现有 Channel history。不同 Channel 或不同私聊之间没有共同 history，目标 Agent 只能获得 handoff 正文中的上下文。

因此第一版传递上下文，不建立共享上下文：

- 不同步完整 history；
- 不发送 reasoning、memory 或 journal；
- 不创建共享可写文档；
- 不处理 revision 或 merge；
- 不同步 endpoint 配置和 Channel token。

## 5. Channel 支持

第一版只声明支持已经确认可以在 Agent 账号之间收发消息的 Telegram 和 Slack。其他 Channel 在确认平台能力后使用同一规则，不提前增加新的抽象。

### 5.1 Telegram

需要：

- 在 BotFather 中开启平台所称的 Bot-to-Bot Communication Mode；
- 允许 `tg:@username` 作为发送目标；
- 保留入站消息中的 Agent user ID、username、display name 和 `is_bot`；
- 群聊只让第一项 Contact 引用指向自己的 Agent 消息触发 task；
- 私聊 Agent 消息不要求正文 mention；
- 忽略自己发送的消息。

### 5.2 Slack

需要：

- 接收来自其他 Agent 账号的 `bot_message`；
- 通过 `bots.info` 把 sender bot ID 解析为可 mention 的 user ID，并缓存结果；
- 允许 runtime 调用 `conversations.open` 和 `chat.postMessage` 建立并发送 Agent 私聊；
- 继续忽略自己的 bot ID 和 bot user ID；
- 保留 sender bot ID；
- 按正文顺序提取 `<@USER_ID>`；
- 群聊只让第一项 Contact 引用指向自己的 Agent 消息触发 task；
- 私聊 Agent 消息不要求正文 mention。

双方 Slack App 仍需位于有权读取和发送消息的同一 conversation。

## 6. 循环保护

任务分配和调用数量由人及 Agent 决定，不增加费用、并发、深度或时间预算。

但 Agent 不能作为系统安全边界。运行时仍需保留最低限度的故障保护：

- 忽略自己发送的消息；
- 按平台 event 或 message ID 去重；
- 群聊 Agent 消息只有在第一项引用指向自己时才触发；
- 同一 conversation 的任意十分钟内，最多由 Agent 消息触发 8 次 task；
- 继续使用现有 task timeout。

`conversation` 直接使用现有 Channel history 的最小范围，不增加新的协作标识：

- Telegram 使用 `chat_id + message_thread_id`，没有 topic 时只使用 `chat_id`；
- Slack 使用 `team_id + channel_id + thread_ts`，没有 thread 时使用整个 channel；
- 私聊使用对应的私聊 chat 或 channel；
- endpoint 不进入 key，计数由各 runtime 本地维护。

运行时只记录实际进入 task 队列的 Agent 消息。人工消息不计数，也不清除已有记录。第 9 条消息会被丢弃并写运行日志；最早记录超过十分钟后，新的 Agent 消息可以继续进入队列。切换 Channel、thread 或 Telegram topic 后进入另一个 conversation，使用独立的计数窗口。

这个上限只用于中止同一 conversation 内的异常循环，不追踪跨 Channel 的完整任务链，也不建立 Scope、session ID 或持久化 collaboration state。

## 7. Agent 配对

静态 Channel allowlist 适合限制普通会话，但不适合要求用户手工复制两个 Agent 的平台 ID。Morph 使用双向配对为 Agent 私聊建立权限。

可以发起配对的人由顶层配置声明：

```yaml
admins:
  - tg:@owner
  - slack:T123:U234
```

管理员可以使用平台稳定 ID。Telegram 也允许使用已有 Contact 的 `tg:@username` 引用；引用不存在于 Contacts 时不授权。`admins` 为空时，任何人都不能执行配对命令。管理员身份只允许在私聊中执行配对控制命令，不绕过普通消息的 Channel allowlist。

配对不使用验证码。两个管理员须在五分钟内分别私聊自己的 Agent：

```text
Agent A 的管理员 -> Agent A: /pair @AgentB
Agent B 的管理员 -> Agent B: /pair @AgentA
```

每个 runtime 为本地命令创建一个五分钟有效的临时意图，并向目标 Agent 发送带随机请求 ID 的内部 offer。两个方向的 Agent 身份和意图匹配后，双方确认配对。命令顺序不影响结果；重复的 offer 必须幂等。

配对内部消息在普通 allowlist 判断前处理，但必须满足以下条件：

- 消息来自平台确认的 Agent 账号；
- 消息格式严格匹配 Morph 配对协议；
- 本地存在对应的未过期配对意图，或者消息只用于保存等待本地管理员确认的 offer；
- 内部消息不进入 LLM、普通 Channel history 或普通 task。

配对完成后，每一侧创建或更新对方的 Contact：

```yaml
kind: agent
paired: true
```

`paired` 是每个 Agent Contact 上的布尔值，不是数组。一个 Agent 与多个 Agent 配对时，会有多个分别标记 `paired: true` 的 Contact。平台返回的稳定身份和私聊地址写入 Contact；nickname 只在为空、`Unnamed User` 或等于平台 username 时自动补全，不能覆盖人工昵称。

`paired` 是权限状态，只能由配对流程设置。普通 Contact observation 和面向 LLM 的写入路径不能设置或清除它。

私聊授权规则为：

```text
普通消息：沿用 Channel allowlist
管理员的 /pair：sender 位于 admins 时允许处理
Agent 私聊：Channel allowlist 允许，或者 sender 对应 active、paired Agent Contact
群聊：只使用原有 Channel allowlist 和 trigger 规则
```

配对协议本身不依赖 Channel。各 Channel 只负责解析管理员和目标 Agent 的平台身份、发送内部配对消息，以及把已认证的 sender 映射到 Contact。平台不支持 Agent-to-Agent 投递时，不能只靠该 Channel 完成配对。

### 7.1 日志与 Journal

运行日志记录配对状态转换，用于定位发送、匹配和超时问题：

- `agent_pair_started`；
- `agent_pair_offer_sent`；
- `agent_pair_offer_received`；
- `agent_pair_matched`；
- `agent_pair_completed`；
- `agent_pair_expired`；
- `agent_pair_failed`。

统一 Journal 使用 `contacts` domain，记录有业务意义的本地状态：

- `agent_pair_requested`：本地管理员执行 `/pair`；
- `agent_pair_completed`：Contact 已更新并标记为 paired；
- `agent_pair_expired`：本地配对意图过期；
- `agent_pair_failed`：已确认的本地配对流程终止失败。

Journal payload 只保存 `pair_id`、管理员 ID 和双方 Agent ID；失败事件额外保存原因。Channel 和时间分别使用 Event Trace 与 Event Time，不在 payload 重复保存。没有本地管理员意图的远端 offer 只写运行日志，避免任意 Agent 污染 Journal。

Contact 成功写入后即视为配对完成。此后的 Journal 写入失败只记录 `agent_pair_journal_error`，不能把已经生效的配对向调用方报告为失败。

## 8. 权限

- Channel token 继续只保存在对应 runtime；
- Agent 只能使用本 runtime 已配置的 Channel sender；
- Contacts 负责身份和寻址，不提供平台凭证；
- 目标 Agent 的 guard 和 approval 不因发送者是 Agent 而放宽；
- handoff 会进入对应 Channel history，按该 Channel 的权限和隐私范围处理；
- 不自动把敏感内容发送到权限更宽的会话。
- `admins` 和 `paired` 都属于权限状态，不能由 LLM 自行授予。

## 9. 非目标

第一版不做：

- Agent 自主发现和选择未知协作者；
- 固定团队、部门、职位或管理层级；
- 按能力自动匹配 Agent；
- 同步 Agent RPC；
- 共享可写 Scope；
- 自动合并多个 Agent 的结果；
- 跨 Agent cancel、progress 或 approval 汇总；
- 合并各 endpoint 的 memory、journal 或完整 history；
- 为不支持 Agent-to-Agent 消息的 Channel 绕过平台限制建立中继服务。

只有实际出现自主组织需求时，才考虑 capabilities、输入输出描述、可用性、费用和结构化 task protocol。

## 10. 验收条件

1. 人可以用 Contact 引用指定当前 Agent 和后续 Agent；
2. 同一群聊消息只有第一项引用对应的 Agent 开始执行；私聊由接收方执行；
3. Agent 只向当前任务明确引用的下一位 Agent handoff；
4. handoff 通过仅允许 active Agent 的 `agent_send` 发送普通 Channel 消息；
5. Telegram Agent 可以通过 username 收发消息；
6. Slack Agent 可以接收其他 Agent 明确寻址的消息；
7. 平台 Agent 身份在 Contacts 中表示为 `kind: agent`；
8. 不建立共享 Scope、远端 task 或协作状态机；
9. 自身消息、重复事件和异常循环不会反复触发 task；
10. 没有正文 mention 的人类消息沿用现有 trigger；含 mention 的群聊消息只允许第一项指向的 Agent 触发。
11. 只有 `admins` 中的平台身份，或能解析到现有 Contact 的 Telegram username 引用，可以在私聊中创建本地配对意图；
12. 双方管理员在五分钟内分别执行 `/pair` 后，双方 Contact 都记录对方身份并标记 `paired: true`；
13. 未配对、单向请求、过期请求和重放请求不能取得私聊权限；
14. active、paired Agent 的私聊可以绕过 Channel allowlist，群聊不能；
15. 配对控制消息不进入 LLM 或普通 task，配对状态转换有运行日志和 `contacts` Journal 事件。

## 11. 参考

- [Telegram Bot Features: Bot-to-Bot Communication](https://core.telegram.org/bots/features)
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [Slack bot_message event](https://docs.slack.dev/reference/events/message/bot_message/)
- [Slack app_mention event](https://docs.slack.dev/reference/events/app_mention/)
- [Slack bots.info](https://docs.slack.dev/reference/methods/bots.info/)

## 12. 实现路径与跟踪

### Phase 1：通用身份与路由

- [x] 在现有 Bus message extensions 中保留 `from_is_agent`，不增加新的消息类型。
- [x] Telegram 和 Slack adapter 双向保留 Agent sender 标记。
- [x] 平台明确标记的 Agent sender 写入 `Contact(kind: agent)`。
- [x] 后续普通观察不能把已有 Agent Contact 降级为 human。
- [x] `[name](protocol:id)` 使用指定 Channel；目标不可用时返回错误，不静默 fallback。
- [x] `[name](contact:id)` 继续使用 Contact 的默认 Connection 选择规则。

### Phase 2：Telegram

- [x] Contact 发送流程支持 `tg:@username` 发送目标，供 `contacts_send` 和 `agent_send` 复用。
- [x] 保留 Agent sender 的 user ID、username、display name 和 `is_bot`。
- [x] 忽略当前 runtime 自己发送的消息。
- [x] 群聊按正文或 caption 中的第一项 mention 判断当前负责人。
- [x] 群聊 Agent 消息没有明确把当前 Agent 放在第一项时不触发 task；私聊不要求 mention。

### Phase 3：Slack

- [x] 接受其他 Agent 的 `bot_message`。
- [x] 通过 `bots.info` 解析 sender user ID，并缓存 6 小时。
- [x] 同时按 bot ID 和 bot user ID 忽略自身消息。
- [x] 保留正文中 `<@USER_ID>` 的出现顺序。
- [x] 群聊 Agent 消息没有明确把当前 Agent 放在第一项时不触发 task；私聊不要求 mention。

### Phase 4：Handoff 与故障保护

- [x] 新增与 `contacts_send` 参数一致的 `agent_send`，复用现有发送流程。
- [x] 仅在存在 active Agent 时注册，并在发送前校验全部目标都是 active Agent。
- [x] Telegram 和 Slack 群聊继续移除 `contacts_send`，允许注册 `agent_send`。
- [x] system prompt 限制 Agent 只能通过 `agent_send` handoff 给当前任务明确引用的 Agent。
- [x] handoff 保留引用中的准确 `contact_id`，并要求正文携带继续执行所需的上下文。
- [x] Telegram topic 和 Slack thread 分别使用现有 history scope 计数。
- [x] 同一 conversation 的滚动十分钟窗口最多允许 8 条 Agent 消息触发 task，第 9 条写日志并丢弃。
- [x] 人工消息不参与 Agent 消息计数，也不重置窗口。

### Phase 5：验证

- [x] 覆盖 Bus adapter、Contacts observation、显式 Channel 路由和 Telegram username 发送测试。
- [x] 覆盖 Telegram 与 Slack 第一项 mention、Agent history sender 和连续 handoff 限制测试。
- [x] 覆盖 Slack `bot_message` 解析与 `bots.info` 测试。
- [x] 覆盖 `agent_send` 参数复用、条件注册、active Agent 校验和批量拒绝测试。
- [x] 运行 `go test ./...`，全仓回归通过。
- [ ] 在真实 Telegram Bot-to-Bot Communication 环境验证一次 Agent A → Agent B handoff。
- [ ] 在两个真实 Slack App 之间验证一次 Agent A → Agent B handoff。
- [ ] 在真实 Channel 验证十分钟内第 9 条 Agent 消息被停止，窗口过期后可以恢复。

### Phase 6：Agent 配对

- [x] 解析顶层 `admins` 平台身份列表，并支持指向现有 Contact 的 Telegram username 引用。
- [x] Contact 支持只由配对流程写入的 `paired` 状态。
- [x] 实现五分钟有效、顺序无关且幂等的双向配对状态。
- [x] Telegram 和 Slack 在 allowlist 前处理管理员 `/pair` 和内部配对消息。
- [x] active、paired Agent 私聊绕过 Channel allowlist，群聊不绕过。
- [x] 配对状态转换写入运行日志和 `contacts` Journal。
- [x] 覆盖成功、单向、过期、重放、非管理员、群聊和 allowlist 回归测试。
- [ ] 在真实 Telegram Bot-to-Bot Communication 环境验证一次双向配对。
- [ ] 在两个真实 Slack App 之间验证一次双向配对。
