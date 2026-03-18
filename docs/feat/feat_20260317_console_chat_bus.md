---
date: 2026-03-17
title: Console Chat 经由 Bus（让 Console 成为一等 Channel）
status: draft
---

# Console Chat 经由 Bus（让 Console 成为一等 Channel）

## 1) 背景

当前 `console` Chat 在产品形态上看起来像“聊天入口”，但实现上并不是一个 channel ingress：

- 浏览器通过 `POST /tasks` 直接提交任务。
- `consoleLocalRuntime.submitTask()` 直接把任务写入 `ConsoleFileStore`，然后进入本地 `ConversationRunner`。
- 它不经过 `internal/bus`，也没有标准 `BusMessage` 的 `conversation_key / participant_key / session_id / thread / mention / sender identity` 语义。

这带来了几个实际问题：

1. `console` Chat 是系统里的特例，而不是一等 channel。  
2. contacts / memory / history / trigger 语义无法和 Telegram / Slack / LINE / Lark 对齐。  
3. 新 feature 很容易出现 “其他 channel 走 bus，console 单独特判” 的分叉。  
4. `console` UI 看起来像聊天，但底层更接近“本地 task submit 面板”。

前面已经出现过一些症状：

- `console` 没有 bus 驱动的 contact observe。
- `console` memory 只能使用合成的 `"console:user"` 与 `console:<topic_id>` 语义。
- `console` Chat / topic / task store 很容易和 managed runtime 的 topic 视图边界产生混淆。

因此，这里需要重新定义一个问题：

> `console` Chat 到底是不是一个真正的 channel？

本文的结论是：对于“用户在 Console 里输入消息给 agent”这条链路，答案应该是 **是**。

---

## 2) 目标

1. 让 `console` Chat 也走统一 bus ingress 模型。  
2. 让 `console` 成为与 Telegram / Slack / LINE / Lark 对等的一等 channel。  
3. 复用 bus 里的统一语义：
   - `conversation_key`
   - `participant_key`
   - `session_id`
   - idempotency
   - inbound message normalization
4. 保留现有 Console UI 的 task/topic 视图与 `/tasks` `/topics` API，不要求前端一次性重写。  
5. 保留 `ConsoleFileStore` 作为 console 自己的 task/topic projection，而不是拿 bus 替代 store。  
6. 明确 `tasks/console/topic.json` 只属于 console；其他 runtime 即使由 Console 托管，也不得写入 console topic 视图。  
7. 要求 `console` 的 agent 回复也走 bus outbound，再投影到 UI 所读的 task/topic 视图。  
8. 要求 `contacts.ObserveInboundBusMessage(...)` 支持 `ChannelConsole`，把 `console:user` 当作真实联系人参与 contacts 语义。  

---

## 3) 非目标

1. 不把 Console 变成新的外部 daemon endpoint。  
2. 不重做 `ConsoleFileStore` / topic API / task polling 视图。  
3. 不把 heartbeat、settings save、system job 之类所有内部任务都强行改成 bus。  
4. 不改变 `tasks.persistence_targets` 的边界；managed runtime 仍按各自 target 选择 `MemoryStore` 或 `FileTaskStore`。  
5. 不让 `GET /tasks` / `GET /topics` 直接查询 bus；读路径仍然基于 projection store。  

说明：

- bus 是 ingress/egress event 层，不是 projection store，也不是 query backend。

---

## 4) 第一性原理

### 4.1 哪些东西应该走 bus？

只要某条路径满足以下特征，就应该优先走 bus：

- 它在产品上是“消息 ingress”
- 它有明确的 conversation 语义
- 它未来可能需要复用统一的 history / memory / contacts / routing 行为

`console` Chat 满足这三条。

### 4.2 哪些东西不必走 bus？

如果某条路径本质上只是：

- 系统内部 job
- heartbeat tick
- 配置变更导致的 reload
- 一次纯 task submit API，而非“聊天消息”

那它不一定需要 bus。

因此，正确边界不是“所有东西都 bus 化”，而是：

> 所有交互式 chat ingress 尽量统一走 bus；projection、存储、管理 API 继续按现有 runtime/store 分层存在。

### 4.3 bus 解决的不是存储，而是语义统一

让 `console` 走 bus，它真正解决的是：

- 进入 agent loop 前的消息模型统一
- conversation/session 语义统一
- future feature 不需要再为 `console` 额外特判

---

## 5) 当前实现摘要

当前 `console` Chat 的路径是：

```text
Browser
  -> POST /api/proxy?.../tasks
  -> consoleLocalRuntime.submitTask()
  -> consoleLocalRuntime.enqueueTask()
  -> ConsoleFileStore.UpsertWithTrigger()
  -> ConversationRunner[console:<topic_id>]
  -> taskruntime.Run()
```

特点：

- 有 topic 概念
- 有 `console:<topic_id>` 形式的 conversation key 雏形
- 有本地 memory hooks
- 但没有 bus envelope / session / participant / idempotency 这一层

这说明：

- `console` 已经很接近一个 channel
- 只是还没有正式进入 bus 语义

---

## 6) 核心提议

### 6.1 新增 `ChannelConsole`

在 `internal/bus` 与 `internal/channels` 中新增：

- `channels.Console`
- `bus.ChannelConsole`

语义：

- `console` 是一个本地、进程内、只对 `console serve` 可见的 channel
- 它不是外部 transport，但它是正式的 chat ingress channel

### 6.2 console chat 的 canonical key 规则

建议定义：

- `topic`: `chat.message`
- `conversation_key`: `console:<topic_id>`
- `participant_key`: `console:user`
- `channel`: `console`

说明：

- `topic_id` 仍然由 Console 自己管理
- bus 里的 conversation 粒度直接复用 Console topic
- 未来如果 Console 支持多用户认证，`participant_key` 可演进为 `console:<account_id>`

### 6.3 `session_id` 的语义

bus 的 dialogue topic 当前要求 `session_id` 为 UUIDv7。

因此这里要区分两件事：

- `topic_id` 是 Console 自己的 conversation identity
- `session_id` 是 bus envelope 上要求的 UUIDv7 字段

如果你的意思是“让 session 与 topic 绑定”，我同意，这是更简单也更合理的第一阶段语义。  
但它不能直接把 `topic_id` 原样塞进 envelope，因为当前校验会拒绝非 UUIDv7 值。

Phase 1 建议改成：

- topic 创建时，同时分配一个稳定的 topic-scoped UUIDv7 `session_id`
- 这个 `session_id` 与 `topic_id` 一一对应，并持久化在 console topic metadata 中
- 同一 topic 的后续消息始终复用该 `session_id`

这样等价于：

- session 语义上绑定 topic
- 仍满足 bus 对 UUIDv7 的校验要求
- 不需要把“浏览器刷新/标签页”额外引入为 session 边界

### 6.4 兼容现有 `/tasks` API

1. 浏览器继续调用 `POST /tasks`
2. Console backend 在 accept path 上分配：
   - `task_id`
   - `topic_id`
   - `conversation_key`
   - `session_id`
3. backend 先写入初始 `queued` projection（保证现有 UI 立即可见）
4. 然后 publish 一条 `inbound / channel=console / topic=chat.message` 的 bus message
5. bus consumer 执行后，再 publish 对应的 `outbound / channel=console / topic=chat.message`
6. Console outbound projector 消费 outbound message，更新 `ConsoleFileStore`
7. 整个执行链路都经由 bus，而不是再直接 `enqueueTask()` 作为 chat ingress/egress

---

## 7) 目标架构

### 7.1 Phase 1 架构

```text
Browser Console Chat
  -> POST /tasks (compat shim)
  -> Console Local accept path
     - allocate task_id/topic_id
     - allocate or load topic-scoped session_id
     - write queued task projection
     - publish inbound bus message (channel=console)
  -> Console Bus Consumer
     - normalize inbound message
     - enqueue conversation job
     - run taskruntime
     - publish outbound bus message (channel=console)
  -> Console Outbound Projector
     - update ConsoleFileStore
  -> Browser polls /tasks + /topics
```

### 7.2 分层职责

```text
bus
  = ingress normalization + ordering + message semantics

ConsoleFileStore
  = console task/topic projection + persistence

/tasks /topics API
  = management/read surface for Console UI
```

三层不是互斥关系，而是：

- bus 先统一“消息”
- store 再统一“任务与 topic 视图”
- API 最后给 UI 用

### 7.3 `topic.json` / `/topics` 的边界

这里需要明确一个容易混淆的点：

- `tasks/console/topic.json` 不是全局 topic 注册表
- `/topics` 不是“列出所有 runtime 会话”
- 它们只代表 **console 自己的 projection**

因此，即使在 `console serve` 进程内同时托管：

- `console`
- `telegram`
- `slack`
- `line`
- `lark`

也必须保持：

- 只有 console chat 创建或更新 console topic
- heartbeat 只进入 console 的保留 topic
- 其他 runtime 的 task/topic 视图继续落在各自 target 的 store

换句话说：

> Console UI 可以同时“管理”多个 runtime，但 `topic.json` 只表达 console chat 自己的会话列表，而不是整个进程里的所有 channel 会话。

---

## 8) 细化设计

### 8.1 新增 Console Inbound Adapter

建议新增：

- `internal/bus/adapters/console`

至少包含：

- `InboundMessage` 结构
- `InboundMessageFromSubmit(...)`
- `BusMessageFromInboundMessage(...)`

它的职责不是接第三方 webhook，而是把 Console 自己的 submit 语义转成标准 `BusMessage`。

### 8.2 Console Local Runtime 新增 bus runtime

`consoleLocalRuntime` 需要从“直接 submit -> runner”变成：

- 持有一个 inproc bus
- 订阅 `chat.message`
- 只处理 `direction=inbound && channel=console`

即：

```text
console submit
  -> bus.PublishValidated(...)
  -> console subscriber
  -> per-conversation runner
```

### 8.3 任务接收与投影

建议保留现有 `ConsoleFileStore`，但调整写入时机：

- accept path：
  - 只负责创建初始 `queued` task projection
  - 不直接启动 execution
- bus consumer：
  - 负责把 inbound console message 转成真正的 conversation job
  - 负责推进 `running / pending`
- outbound projector：
  - 消费 console outbound bus message
  - 写入最终 assistant output 与 `done / failed`

这样可以避免：

- UI 同步响应丢失
- 现有 polling model 一次性重写

### 8.4 memory 语义

当前 `console` memory 是：

- `SubjectID = console:<topic_id>`
- `SessionID = console:<topic_id>`
- Counterparty 固定 `"console:user"`

建议在 bus 化后改成：

- `SubjectID = conversation_key`
- `SessionID = conversation_key`（Phase 1 继续稳定绑定 topic）
- `Channel = console`

这样：

- subject 仍是稳定 conversation
- memory 语义继续与现有 console topic 视图保持一致
- bus 的 `session_id` 与 memory session 可以先解耦，不强迫 memory 立即迁移到 UUIDv7

### 8.5 contacts 语义

这里需要把语义说清楚：

- `console:user` 代表正在使用 Console 的真实人类操作者
- 因此它不是“假的系统内部占位符”，而是一个真实联系人

建议 Phase 1：

- `ChannelConsole` 进入 bus
- `contacts.ObserveInboundBusMessage(...)` 增加 `ChannelConsole` 分支
- inbound console message 会 upsert `console:user`

约束：

- 当前实现仍然是单一 console principal，因此 contact id 先固定为 `console:user`
- 如果以后 Console 引入真正的用户身份，`participant_key` / contact id 可以演进为 `console:<account_id>`

### 8.6 outbound / projector 是否也要经由 bus？

需要。

理由：

- 如果 `console` 要成为一等 channel，语义上不能只有 inbound 没有 outbound
- assistant reply 本身就是 channel message，适合成为 `outbound / channel=console / topic=chat.message`
- 但这不意味着浏览器要直接读取 bus；UI 仍然应该读取投影视图

推荐做法：

- task accept path 仍然先写 `queued`
- runtime 在完成后 publish console outbound bus message
- Console outbound projector 消费 outbound message，并把结果写入 `ConsoleFileStore`
- `GET /tasks` / `GET /topics` 继续读 projection，不直接读 bus

后续如果需要真正的 message stream，再考虑：

- `channel=console`
- `direction=outbound`
- browser 订阅 console event stream

### 8.7 `tasks.persistence_targets` 与 store 归属

这次改造不应改变持久化归属规则。

当前正确边界应该继续保持：

- `console` chat projection -> `ConsoleFileStore`
- `serve` API projection -> `FileTaskStore(target=serve)` 或 `MemoryStore`
- `telegram/slack/line/lark` projection -> `FileTaskStore(target=<kind>)` 或 `MemoryStore`

也就是说：

- console chat ingress 走 bus
- console chat egress 也走 bus
- 不等于其他 runtime 改写到 console store
- 不等于 `/topics` 变成所有 runtime 的合并视图

这点和前面的 bug 修复是一致的：managed runtime 即使由 Console 托管，也必须继续遵守 `tasks.persistence_targets`，不能把自己的 topic/task 持久化污染到 console 目录。

---

## 9) 与现有 feature 的关系

### 9.1 与 managed runtime 的关系

当前 managed Telegram / Slack 已经有自己的 channel ingress 语义。

若 `console` 也走 bus，则系统内的交互式入口会更统一：

- `console`
- `telegram`
- `slack`
- `line`
- `lark`

它们都成为“chat ingress -> bus -> runtime execution -> projection store”模型。

但这里要避免一个错误推论：

- “都走 bus” 不代表“都共用 console projection”

正确关系是：

- ingress 语义统一
- egress 语义统一
- projection store 继续按 target 隔离
- Console Chat 只显示 console topic 与 heartbeat topic（在 UI 允许显示 heartbeat 时）

### 9.2 与 task persistence 的关系

`tasks.persistence_targets` 不需要改变语义。

它仍然只控制 projection store 是否落盘：

- `console` -> `ConsoleFileStore`
- `telegram/slack/...` -> `FileTaskStore`

bus 不替代 persistence。

### 9.3 与 heartbeat 的关系

heartbeat 建议继续不走 bus：

- 它不是用户 chat ingress
- 它是 system job
- Console heartbeat 仍然写入保留 topic（如 `_heartbeat`）

如果未来要统一 system job event model，可以再单开 feature。

---

## 10) 实施分期建议

### Phase 1：引入 console chat bus

目标：

- `console` chat ingress/egress 都经过 bus
- UI 与 `/tasks` `/topics` 基本不变

步骤：

1. 新增 `channels.Console` / `bus.ChannelConsole`
2. 新增 `internal/bus/adapters/console`
3. `ConsoleFileStore` 的 topic metadata 新增稳定的 topic-scoped `session_id`
4. `consoleLocalRuntime` 内部启动 inproc bus，并订阅 `channel=console`
5. `/tasks` accept path 从“直接 enqueue”改成“queued projection + publish inbound bus message”
6. bus consumer 负责执行与 `running / pending`
7. runtime 完成后 publish console outbound bus message
8. console outbound projector 负责把最终结果投影到 `ConsoleFileStore`
9. `contacts.ObserveInboundBusMessage(...)` 支持 `ChannelConsole`
10. 保持 `/tasks` `/topics` 读取 `ConsoleFileStore`

### 10.1 当前实现任务拆分

为避免过度设计，先按下面 4 个最小任务推进：

1. `UUID topic`
   - 新建 console topic 时直接生成 UUIDv7 作为 `topic_id`
   - 新 topic 不再额外引入独立 `session_id` 存储字段
   - 这样 `topic_id` 本身就可以直接复用为 bus `session_id`

2. `console inbound bus`
   - `POST /tasks` 对 console chat 改为“先写 queued projection，再 publish inbound bus”
   - bus consumer 再进入现有 `ConversationRunner`
   - heartbeat 继续保留直连执行，不纳入这次改造

3. `console contacts observe`
   - 为 bus 增加 `ChannelConsole`
   - `contacts.ObserveInboundBusMessage(...)` 支持 `console:user`

4. `outbound 保持克制`
   - 先不为了“形式对称”改造 `/tasks` 查询层
   - 如果本轮实现 outbound 成本过高，就只先把 inbound 闭环做扎实
   - outbound projector 作为下一步任务推进，而不是在第一轮强行扩 schema

第一轮完成标准：

- Console UI 提交的 chat 不再直接调用 `enqueueTask()`
- 新 topic id 为 UUIDv7
- console inbound bus message 能驱动执行
- `console:user` 能进入 contacts observe
- `/tasks` `/topics` 行为对前端保持兼容

### Phase 2：memory/history 语义统一

目标：

- `console` memory session 与其他 channel 对齐
- `console` history 不再是纯合成结构

步骤：

1. `console` bus message 驱动 chat history projection
2. memory hooks 改为使用 bus session metadata
3. 评估是否需要 topic 级 sticky session 策略

### Phase 3：可选的 console outbound bus/UI stream

目标：

- 让 UI 消息展示也能复用 bus outbound 语义

这不是必做项，只在需要实时 stream / richer UI message model 时再推进。

---

## 11) 风险与控制

### 风险 1：`task_id` 与 bus event 的同步关系变复杂

控制：

- accept path 先分配 `task_id`
- consumer 只消费该 `task_id` 对应 job
- 使用稳定 `idempotency_key`

### 风险 2：`session_id` 既要满足 bus 校验，又要保持 topic 语义

控制：

- 明确定义：
  - `conversation_key = console:<topic_id>`
  - `session_id = topic-scoped UUIDv7`

### 风险 3：固定 `console:user` 无法表达未来多用户 Console

控制：

- Phase 1 先接受单 principal 语义
- 若未来引入真实用户身份，则把 `participant_key/contact_id` 升级为 `console:<account_id>`

### 风险 4：做成“表面 bus 化，实则仍是两套语义”

控制：

- bus 负责真正的 ingress
- 后续 feature 只能以 `ChannelConsole` 为入口接入，不再继续加 console submit 特判

---

## 12) 决策建议

结论：

- 让 `console` Chat 走 bus 是合理方向。
- 但应该把它定义成“console 成为一等 channel”，而不是简单在现有 submit 前面包一层 bus publish。

推荐落地方式：

1. 先做 **console chat ingress/egress bus 化**
2. 保持现有 task/topic projection 与 UI 兼容
3. `GET /tasks` / `GET /topics` 继续走 projection，不直接查 bus
4. contacts 直接纳入 `ChannelConsole`

这样能用最小风险换来最大的语义统一收益。

---

## 13) DoD

当以下条件满足时，可以认为本 feature 第一阶段完成：

1. `console` 存在正式的 `ChannelConsole`。  
2. 浏览器发起的 console chat 最终以 `BusMessage(channel=console, topic=chat.message)` 进入执行链路。  
3. assistant 最终回复也会以 `BusMessage(direction=outbound, channel=console, topic=chat.message)` 进入 console projector。  
4. Console UI 继续可用现有 `/tasks` `/topics` 读取任务与 topic。  
5. `ConsoleFileStore` 继续只承载 console 自己的 projection。  
6. `contacts.ObserveInboundBusMessage(...)` 会把 `console:user` 作为 Console 联系人写入 contacts。  
7. 相关测试覆盖：
   - console inbound message build/validate
   - console outbound message build/validate
   - bus consumer -> runner -> outbound projector -> task store
   - `/tasks` compatibility submit path
   - idempotent re-submit 不重复执行
8. managed runtime 的 topic/task 不会进入 `tasks/console/topic.json` 与 console `/topics` 结果。  
