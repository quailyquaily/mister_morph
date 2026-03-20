---
date: 2026-03-20
title: 流式更新细化拆分与技术评估
status: draft
---

# 流式更新细化拆分与技术评估

本文是 `feat_20260320_streaming_ux.md` 的细化版，目标不是再讲一遍愿景，而是基于当前代码回答三个问题：

1. 现在的代码到底已经到哪一步？
2. 真正的技术阻塞点是什么？
3. 实施时应该按什么任务包拆分，哪些范围先做，哪些范围暂缓？

---

## 1) 代码现状摘要

### 1.1 Shared LLM / Agent 层

当前共享层已经具备 stream callback 的主干链路：

- `llm.Request.OnStream`
- `providers/uniai/client.go`
- `agent.RunOptions.OnStream`
- `internal/channelruntime/taskruntime.RunRequest.OnStream`

结论：

- provider 侧不是阻塞点。
- `taskruntime` 也不是阻塞点。
- 真正缺的是各 channel task path 没把 `OnStream` 接进去。

### 1.2 共享 agent loop 的真实约束

`agent/engine_loop.go` 当前每次 LLM 调用都使用 `ForceJSON: true`。  
系统 prompt 也明确要求模型在非 tool call 时返回 JSON：

```json
{
  "type": "final",
  "reasoning": "...",
  "output": "..."
}
```

这意味着：

> 原始 stream delta 不是可直接展示给用户的自然语言，而是 JSON 文本流。

因此，不能简单把 `OnStream` 的 `Delta` 原样推给 Telegram / console 用户，否则会出现：

- `{"type":"final"...}` 这类 JSON 壳子直接暴露
- `reasoning` 字段被流出去
- plan/tool 重试阶段的 JSON 也可能误推给用户

这是当前最关键的技术约束。

### 1.3 Console Local Runtime

`cmd/mistermorph/consolecmd/local_runtime.go` 和 `local_runtime_bus.go` 已经把 Console Local 的任务链路集中起来了：

- `submitTask()` -> `submitTaskViaBus()`
- `acceptTask()` 负责分配 `task_id / topic_id`
- `publishConsoleInbound()` 发布 console inbound bus message
- `handleConsoleBusInbound()` 把任务排入 `ConversationRunner`
- `handleTaskJob()` / `runTask()` 执行真正的 agent run

结论：

- Console Local 的接线点非常清晰。
- 如果只做 `Console Local` 流式，当前代码基础足够好。

### 1.4 Console Frontend / Server

当前 console 前后端的真实状态：

- `web/console/src/views/ChatView.js` 通过 `pollTask()` 每 `1200ms` 轮询 `/tasks/{id}`
- `web/console/src/components/MarkdownContent.js` 在 `source` 变化时会重新 `renderer.update(...)`
- `cmd/mistermorph/consolecmd/serve.go` 目前只提供：
  - `/api/auth/*`
  - `/api/endpoints`
  - `/api/proxy`
- `runtimeEndpointClient` 只有两个方法：
  - `Health(...)`
  - `Proxy(...)`

结论：

- 前端渲染链路支持增量刷新。
- 后端没有 websocket / stream endpoint。
- `console serve` 对远端 runtime endpoint 也只有 request/response proxy，尚无流式协议。

### 1.5 Telegram Runtime

Telegram 当前主执行链路是：

- `internal/channelruntime/telegram/runtime.go`
- worker 中调用 `runTelegramTask(...)`
- `runTelegramTask(...)` 内部再调用 `taskruntime.Run(...)`

但当前 `runTelegramTask(...)` 还没有把 `OnStream` 传给 `taskruntime.Run(...)`。

另外：

- `internal/channelruntime/telegram/telegram_api.go` 目前已有：
  - `sendMessage...`
  - `editMessage...`
  - `sendMessageChunkedReply...`
- 但还没有 `sendMessageDraft` wrapper。

结论：

- Telegram 的 stream 接线本身不复杂。
- Telegram transport 侧还缺 draft API 封装，这是一块独立任务。

---

## 2) 关键技术评估

### 2.1 最大阻塞不是 websocket，而是 JSON 协议

如果只看 transport，会以为“接个 `OnStream` + websocket 就行”。  
但当前 agent loop 的真实约束是：

- `OnStream` 发生在 provider raw output 层
- raw output 当前是 JSON protocol
- 且一个 run 里可能有多次 LLM 调用

所以 V1 必须先解决：

> 如何从 raw JSON stream 中只提取最终用户可见的 `final.output` 字段。

否则 channel 侧流式发布没有意义。

### 2.2 推荐做法：提取 `final.output` snapshot，而不是 raw delta

建议在 shared streaming core 中实现一个“final output extractor”：

1. 按单次 LLM 调用维护 buffer
2. 只在判断当前响应是 `type="final"` 后继续处理
3. 从增量 JSON 文本中提取 `output` 字段的已解码字符串
4. 对外发布的是 `output` 的完整 snapshot，而不是 raw JSON delta

好处：

- channel 侧不需要理解 agent JSON 协议
- UI 直接消费用户可见文本
- 后续即使 prompt 协议小改，也只影响 extractor

代价：

- 需要实现一个增量 JSON 字段提取器
- 这是 shared streaming V1 里风险最高的一段

### 2.2.1 V1 不要先做通用 streaming 平台

从第一性原理看，V1 真正不可省的只有三件事：

1. 同一条用户消息只执行一次
2. 只能把 `final.output` 流出来，不能流 raw JSON
3. 一个 run 最终只能对应一个可见回复

因此当前阶段不需要先做通用 stream broker 或平台层。  
更小、更稳的实现是：

- shared `final.output` extractor
- per-run `ReplySink`
- Console Local 的 in-process `StreamHub`

### 2.2.2 不允许双重处理同一条入站消息

如果同一条 inbound message 同时进入 `bus` 和 `streamhub` 两条处理链路，就会带来：

- 重复执行
- 重复展示
- 重复终态

因此正确边界必须是：

- inbound 只进入一个 ingress owner
- executor / conversation worker 只执行一次
- `bus` 与 `streamhub` 都只是该 executor 的 output sink

所以：

> `streamhub` 不能订阅 inbound message 来“再跑一次”，它只能消费 executor 产出的 snapshot。

### 2.2.3 需要 per-run ReplySink

即使执行只发生一次，如果 stream path 和 final path 都拥有“创建可见消息”的权力，也仍然会重复。

V1 最小解法不是做一个通用 coordinator，而是：

- 每个 run 创建一个 `ReplySink`
- stream 期间只调用 `Update()`
- final 阶段只调用 `Finalize()`

这个 `ReplySink` 自己负责保证：

- 一个 run 只对应一个可见回复
- Console 里是 same bubble finalize
- Telegram 里是 same draft finalize

### 2.3 Console 流式的范围必须先收窄

如果把“console 流式”定义成：

> 所有 `console.endpoints[]` 指向的远端 runtime endpoint 都支持流式

那么当前代码不够，原因有三层：

1. `runtimeEndpointClient` 只有 `Health/Proxy`
2. `daemonruntime` 没有 stream/watch API
3. `serve.go` 的 `/api/proxy` 是标准 request/response，不是 upgrade 通道

因此建议 V1 范围收敛为：

- `Console Local` 支持 websocket 流式
- 远端 endpoint 保持现有 polling

这是当前最合理的切法。

### 2.4 Console websocket 还有一个鉴权问题

当前 Console SPA 所有 API 请求都走：

- `fetch`
- `Authorization: Bearer <token>`

但浏览器原生 WebSocket 不能像 `fetch` 一样自由设置 `Authorization` header。

因此 websocket 不能直接复用现有前端鉴权写法。

V1 可选方案：

1. 最小实现：ws query 参数带 token
2. 更稳妥实现：先走受保护的 HTTP API 申请短时 `stream_ticket`，再用 ticket 建立 websocket

建议采用第 2 种：

- 避免长期 bearer token 出现在 URL
- 更适合后续做 ticket 过期和 endpoint 绑定

### 2.5 Console 不适合把 partial snapshot 写进 `TaskInfo.Result`

当前 `ConsoleFileStore` 是持久化 task/topic projection store，不是 stream hub。

如果把高频 snapshot 写进 `TaskInfo.Result`，会带来三个问题：

1. 把临时 UI 信号混进 durable task state
2. 持久化写放大明显
3. `/tasks/{id}` 语义会从“任务事实”变成“事实 + 高频临时状态”

因此不建议复用 store 作为流式通道。  
更好的方式是只在 Console Local 增加最小的 in-process `StreamHub`。

### 2.6 Telegram 不能直接拿现有 `sendMessage/editMessage` 顶替 draft API

当前仓库已经有：

- `sendMessage...`
- `editMessage...`

看起来似乎可以“先发一条消息，再不停 edit”。  
但在当前设计里，如果 Telegram stream path 先创建了一个可见实体，而 final path 又无条件发送最终消息，就会重复。

这会带来明显问题：

- stream path 先占用了一个可见 output slot
- final path 若不感知该 slot，又会再发一条
- 最终形成重复内容

因此如果要保持现有 canonical final fact，又不想重复显示，就需要：

- 一个 per-run `TelegramReplySink`
- final 阶段优先 finalize 已存在的 draft
- 只有在 draft 不存在或 finalize 不可用时，才回退到普通最终发送

在这个前提下，普通 `send/editMessage` 仍不适合作为 draft 的替代品。  
V1 更合理的方向仍是：

- 补 draft API wrapper
- draft 仅作为 channel-local 临时展示
- canonical final fact 保留
- 最终可见交付由同一个 `TelegramReplySink` 收口

### 2.7 现有依赖足够支撑 console websocket

仓库 `go.mod` 已经有 `github.com/gorilla/websocket`，Slack runtime 也在使用它。

结论：

- Console backend 做 websocket 不需要新引第三方库
- 改造成本主要在路由、鉴权和 stream hub，不在依赖管理

---

## 3) 推荐的 V1 范围

建议把本次实现范围明确收敛为：

1. shared streaming core
2. per-run `ReplySink` + Console Local in-process `StreamHub`
3. Console Local websocket streaming
4. Telegram private-chat draft streaming
5. 远端 console endpoint 仍保留 polling
6. Telegram group / supergroup 暂不启用

不建议把下面这些纳入 V1：

- 所有 console endpoints 的统一流式
- daemonruntime 通用 stream API
- Telegram 群聊 draft streaming
- 把 stream snapshot 持久化进 bus 或 task store

---

## 4) 任务拆分

### Workstream A: Shared streaming core

#### A1. 新增 shared stream package

建议新增包，例如：

- `internal/channelruntime/streaming`

建议职责：

- `CallTracker`
- `FinalOutputExtractor`
- `SnapshotThrottler`
- `ReplySink` interface

#### A2. 定义统一事件模型

建议先只做文本 snapshot，不做 token-level public API：

```go
type Snapshot struct {
    RunID            string
    TaskID           string
    Channel          string
    ConversationKey  string
    TopicID          string
    Seq              int64
    Text             string
    Done             bool
    Failed           bool
}
```

#### A3. 实现 `final.output` 增量提取

这是 shared core 中最关键的任务：

- 输入：`llm.StreamEvent`
- 输出：用户可见 `output` snapshot

需要明确处理：

- 只提取 `type=final`
- 忽略 `reasoning`
- 忽略 tool-call delta
- 处理 JSON string escape
- 每个模型调用 `Done` 后重置 call-local 状态

#### A4. 加入节流

建议策略：

- `min_interval_ms`
- `min_chars`
- `flush_on_done`

#### A5. 单元测试

重点覆盖：

- `{"type":"final","output":"hello"}` 增量拆包
- 包含 escaped chars 的 `output`
- `plan` 响应不产生 snapshot
- tool-call 响应不产生 snapshot
- 多次 LLM 调用之间的状态重置

复杂度评估：

- 中高
- 风险最高
- 是后续所有 channel 方案的前置依赖

### Workstream B: Console Local backend

#### B1. 新增 in-memory stream hub

建议放在：

- `cmd/mistermorph/consolecmd/`

职责：

- task 级别订阅
- endpoint 级别广播
- 断开连接自动清理
- 非持久化
- 只消费 executor 产出的 snapshot，不处理 inbound

#### B2. 在 `consoleLocalRuntime` 中接入 stream

改造点：

- `consoleLocalRuntime` 增加 `streamHub`
- `runTask()` 调用 `bundle.taskRuntime.Run(...)` 时传 `OnStream`
- `handleTaskJob()` 在 started/final/error 时向 hub 发 lifecycle event
- 同一 `task_id` 只维护一个逻辑 reply handle

#### B3. 定义 websocket 事件格式

建议事件：

- `task.started`
- `task.snapshot`
- `task.final`
- `task.error`

注意：

- `task.final` 的语义是 finalize 同一个 `task_id` 的 bubble
- 不是 append 第二条 agent 消息

#### B4. Console server 新增 websocket route

建议增加：

- `GET /api/stream`

注意：

- 这条路由不走 `/api/proxy`
- 它属于 console server 自己的 API，而不是 daemonruntime API

#### B5. websocket auth

建议新增受保护的 HTTP API 申请短时 ticket，例如：

- `POST /api/stream-ticket`

ticket 绑定：

- console session
- endpoint ref
- 过期时间

复杂度评估：

- 中
- 主要是后端 glue code
- 对现有 task 执行路径侵入较小

### Workstream C: Console Frontend

#### C1. 新增 websocket client manager

建议放在：

- `web/console/src/core/`

职责：

- 建立连接
- 订阅当前 endpoint
- 按 `task_id` 分发事件
- 自动重连
- 回退到 polling

#### C2. 改造 `ChatView`

现有 `submitTask()` 逻辑已经会：

- 先插入 user item
- 再插入 pending agent item
- 获得 `task_id` 后开始 `pollTask()`

建议改成：

1. 任务提交后保持现有本地 pending item
2. 若 websocket 已就绪，则优先消费 `task.snapshot`
3. 若 websocket 不可用，则继续走 `pollTask()`
4. `task.final` 到来后 finalize 同一个 `task_id` 对应的 bubble
5. 停止对该 task 的 stream 更新

#### C3. 维持 polling fallback

不要删除现有 polling 逻辑。  
原因：

- 远端 endpoint 仍无流式
- websocket 可能断线
- rollout 初期需要保底路径

复杂度评估：

- 中
- 风险低于 shared core
- 主要是状态同步和重连处理

### Workstream D: Telegram private-chat streaming

#### D1. 补 Telegram draft API wrapper

改造点：

- `internal/channelruntime/telegram/telegram_api.go`

内容：

- request/response struct
- API wrapper
- error mapping

#### D2. 增加 TelegramReplySink

建议职责：

- 首次 snapshot 为当前 run 创建 draft
- 后续 snapshot update draft
- done 时优先 finalize 同一个 draft
- 出错时 fallback

#### D3. 在 `runTelegramTask()` 接入 `OnStream`

改造点：

- `internal/channelruntime/telegram/runtime_task.go`

#### D4. 私聊范围控制

只在 `chat_type=private` 时启用。

#### D5. 失败回退

如果 draft 失败：

- 记录日志
- 停止 draft update
- 继续 final canonical fact

复杂度评估：

- 中高
- 依赖 shared core 完成
- 另有 transport API 不确定性

### Workstream E: 配置、观测、测试

#### E1. 配置开关

建议：

- `streaming.enabled`
- `streaming.min_interval_ms`
- `streaming.min_chars`
- `console.websocket_stream`
- `telegram.stream_private_chat`

#### E2. 日志 / 指标

建议日志点：

- `stream_started`
- `stream_snapshot`
- `stream_publish_failed`
- `stream_completed`
- `stream_disabled`
- `stream_fallback_to_polling`

#### E3. 测试

建议新增测试面：

- shared extractor 单测
- console stream hub 单测
- console websocket route 测试
- ChatView 前端状态测试
- Telegram draft API 测试

---

## 5) 实施 Checklist

### Milestone 0: 范围冻结

- [ ] 明确 V1 范围只包含 `Console Local` 与 Telegram 私聊，不包含远端 console endpoint 流式。
- [ ] 明确 stream 不进入 `bus`，也不写入 `ConsoleFileStore` / `TaskInfo.Result`。
- [ ] 明确采用 `single execution / dual output`：同一 inbound message 只执行一次。
- [ ] 明确采用 per-run `ReplySink`，禁止 stream path 与 final path 各自产生一条可见消息。

### Milestone 1: Shared streaming core

- [ ] 新增 `internal/channelruntime/streaming` 包。
- [ ] 定义 shared snapshot 数据结构，至少包含 `run_id`、`task_id`、`channel`、`conversation_key`、`seq`、`text`。
- [ ] 定义 `ReplySink` 接口。
- [ ] 实现 `final.output` 增量提取器，不暴露 raw JSON delta。
- [ ] 实现节流与 coalescing，默认输出 snapshot 而不是 token delta。
- [ ] 为 extractor 补齐单元测试，覆盖 `final` / `plan` / tool-call / 多次模型调用重置。
- [ ] 验证不接 publisher 时，现有运行路径行为不变。

### Milestone 2: Console Local backend

- [ ] 新增 in-process `StreamHub`，支持按 `task_id` / endpoint 广播 snapshot。
- [ ] `consoleLocalRuntime.runTask()` 接入 `OnStream`，将 snapshot 发布到 `StreamHub`。
- [ ] `handleTaskJob()` 发布 `task.started` / `task.final` / `task.error` lifecycle event。
- [ ] 为 Console Local 定义 per-task reply handle 规则：一个 `task_id` 只对应一个 bubble。
- [ ] 在 `console serve` 新增 `POST /api/stream-ticket`。
- [ ] 在 `console serve` 新增 `GET /api/stream` websocket 路由。
- [ ] websocket 鉴权改为短时 ticket，不直接暴露长期 bearer token。
- [ ] 为 stream hub 与 websocket route 补齐后端测试。

### Milestone 3: Console Frontend

- [ ] 新增 websocket client manager，支持连接、重连、订阅、断线回退。
- [ ] `ChatView` 提交任务后继续复用现有 pending agent item。
- [ ] `task.snapshot` 到来时更新同一个 `task_id` 对应的 agent bubble。
- [ ] `task.final` 到来时 finalize 同一个 bubble，而不是 append 第二条消息。
- [ ] websocket 不可用时继续走现有 `pollTask()` fallback。
- [ ] 补齐前端状态测试，验证 reconnect / fallback / same-bubble finalize。

### Milestone 4: Telegram private-chat streaming

- [ ] 在 `internal/channelruntime/telegram/telegram_api.go` 补 `sendMessageDraft` wrapper。
- [ ] 实现 `TelegramReplySink`，负责 create/update/finalize draft。
- [ ] `runTelegramTask()` 接入 `OnStream`。
- [ ] final fact 到来时优先 finalize 已存在的 draft。
- [ ] 仅当 draft 不存在或 finalize 不可用时，才回退到普通最终发送。
- [ ] 范围严格限制为 `private` chat。
- [ ] 补齐 Telegram transport 与 `ReplySink` 测试。

### Milestone 5: 配置、观测、回滚

- [ ] 增加 `streaming.enabled`、`streaming.min_interval_ms`、`streaming.min_chars`。
- [ ] 增加 `console.websocket_stream`、`telegram.stream_private_chat`。
- [ ] 增加日志点：`stream_started`、`stream_snapshot`、`stream_publish_failed`、`stream_completed`。
- [ ] 增加明确的 rollback 开关，支持按 channel 禁用 streaming。
- [ ] 验证 stream 失败不影响 final canonical path。

### Milestone 6: 联调与验收

- [ ] Console Local 手工验证：看到增量文本，最终只有一个 bubble。
- [ ] Console websocket 断开验证：自动回退到 polling，任务最终仍完成。
- [ ] Telegram 私聊验证：看到 draft 增量更新，最终只有一条可见回复。
- [ ] Telegram draft 失败验证：停止 draft update，但 final 仍正常发送。
- [ ] 验证 `bus` 中仍只有 canonical final event，没有高频 snapshot。
- [ ] 验证没有重复执行、重复展示、重复终态。

---

## 6) 推荐实施顺序

### Step 1. 先攻 shared extractor

这是全局最高风险项。  
如果 `final.output` 无法稳定从 raw JSON stream 中提取，后面的 websocket 和 Telegram 都没有稳定输入。

### Step 2. 再做 Console Local

原因：

- 接线点清晰
- 同进程
- 易调试
- 不依赖外部 transport API

它应该作为 streaming 架构的第一块验证场。

### Step 3. 最后做 Telegram 私聊

原因：

- 依赖 shared core
- 依赖 Telegram draft API 封装
- transport 失败和限流处理比 console 更复杂

---

## 7) 不建议的实现路线

### 7.1 不建议直接 stream raw provider delta

原因：

- 当前 agent 协议是 JSON，不是 plain text
- 会把 `reasoning` / JSON 壳子 / 计划响应直接暴露给用户

### 7.2 不建议把 partial output 写回 `TaskInfo.Result`

原因：

- 混淆 durable state 与 ephemeral state
- 增加持久化压力
- 污染 `/tasks/{id}` 的既有语义

### 7.3 不建议 V1 直接支持远端 endpoint 流式

原因：

- 当前 console 与远端 runtime 之间没有 stream 协议
- 需要同步改 `daemonruntime` API、`daemonTaskClient`、`serve.go`

---

## 8) 最终建议

当前代码条件下，最稳妥的实施决策是：

1. 把 shared streaming core 的核心问题定义为“提取 `final.output`”，而不是“拿到 token delta”。
2. 把 Console 流式的 V1 范围收敛为 `Console Local`。
3. 用独立的 in-process stream hub + websocket，而不是污染 `TaskInfo.Result` 或 `ConsoleFileStore`。
4. 把 Telegram 流式依赖明确拆成两块：
   - shared core 接线
   - draft API transport 接入
5. 明确坚持“single execution / dual output”边界：inbound 只执行一次，`bus` 和 `streamhub` 都只是 output sink。
6. 为每个 run 引入 `ReplySink`，避免 stream path 与 final path 产生两条可见消息。

如果按这个边界推进，整个项目会得到一条清晰路径：

- shared core 解决“如何安全地产出用户可见 stream”
- Console Local 先验证 UX 与通路
- Telegram 私聊随后复用同一套核心能力，并通过 `TelegramReplySink` 收口 final delivery

这是当前代码现实下，风险最低、收益最高的落地方式。
