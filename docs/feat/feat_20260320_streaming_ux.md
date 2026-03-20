---
date: 2026-03-20
title: 统一流式更新方案（uniai / Telegram / console）
status: draft
---

# 统一流式更新方案（uniai / Telegram / console）

## 1) 背景

当前系统已经具备做流式更新的几个关键前提：

1. `uniai` 已支持 `OnStream` 回调，LLM 层可以拿到增量 token / tool-call delta / usage。
2. Telegram 私聊存在可用于“草稿更新”式体验的 API 能力，适合做低延迟增量回复。
3. `console serve` 走 Web 前后端，天然可以用 websocket 推送增量内容；前端 markdown renderer 也支持在 `source` 变化时重新渲染。

但这些能力现在还是分散的：

- LLM 层支持流，不等于 channel 侧已经消费流。
- Telegram 有单独设计稿，但尚未形成统一抽象。
- Console Chat 当前仍是轮询 `/tasks/{id}` 的最终态体验，而不是 push-based 流式体验。

因此，这里需要一份统一设计，回答两个问题：

1. 流更新应当在哪一层抽象？
2. Telegram / console 应该如何各自落地，而不破坏现有 bus、task、final output 语义？

---

## 2) 目标

1. 在共享 runtime 层引入统一的 stream 消费与发布抽象。
2. 在 Telegram 私聊中提供增量回复体验。
3. 在 `console serve` 中提供 websocket 驱动的增量回复体验。
4. 保持最终结果、任务状态、memory、bus outbound 等 canonical 语义不变。
5. 让“支持流式输出”成为 channel capability，而不是每个 channel 自己重新发明一套逻辑。
6. 保证同一条用户消息只执行一次，且不会因为 stream path 与 final path 产生重复可见消息。

---

## 3) 非目标

1. 不把所有流式 delta 都塞进 `internal/bus`。
2. 不重写 agent/tool loop。
3. 不要求所有 channel 同时支持流式更新。
4. 不把“流式体验”与“最终消息送达”混成一件事。
5. 不在第一阶段解决跨进程持久化 stream replay。

---

## 4) 当前状态

### 4.1 已有能力

- `llm.Request` 已定义 `OnStream llm.StreamHandler`。
- `agent.RunOptions` 已支持把 `OnStream` 继续传给 LLM 调用。
- `taskruntime.RunRequest` 也已保留 `OnStream` 字段。
- `providers/uniai/client.go` 已将 `req.OnStream` 映射到 `uniai.WithOnStream(...)`。

这意味着：

> LLM provider 能力已经具备，缺的是 runtime/channel 对 stream 的消费与投递。

### 4.2 Telegram 的现状

- 仓库里已经有 Telegram draft streaming 设计稿：
  - `docs/feat/feat_20260303_telegram_sendmessagedraft_stream.md`
- `docs/bus.md` 和 `docs/telegram.md` 也已明确一个重要边界：
  - Telegram draft stream delta 应保持为 runtime-local / channel-local
  - bus 只承载 canonical outbound message
- 这更接近“设计边界已明确”，而不是“主执行路径已经完整接线”。

这条边界是合理的，应继续保留。

### 4.3 Console 的现状

- `web/console/src/views/ChatView.js` 当前通过 `pollTask()` 每 `1200ms` 轮询 `/tasks/{id}`。
- `web/console/src/components/MarkdownContent.js` 会在 `source` 变化时执行 `renderer.update(...)`。

这意味着：

- 前端渲染链路本身并不阻碍流式输出。
- 缺的是服务端 push 通道，以及 chat item 的增量状态模型。

### 4.4 现阶段的真实缺口

现在真正缺少的，不是 provider 能不能 stream，而是：

1. 谁来接 `OnStream`。
2. stream delta 如何节流、聚合、降噪。
3. 不同 channel 如何把 stream 变成用户可见的增量输出。
4. stream 失败时如何优雅退回到现有 final-only 语义。

---

## 5) 第一性原理

### 5.1 流式更新不是 canonical message

流式更新的本质是“临时 UI/transport 体验”，不是最终业务事实。

最终业务事实仍然应该是：

- 任务完成状态
- 最终 `final.output`
- 最终 outbound 文本/错误
- memory 记录
- contacts / audit / stats 等后续观察结果

因此：

> stream delta 不应该默认进入 bus，不应该成为持久化事实源。

### 5.2 流式更新是 channel-local capability

不同 channel 的展示介质不同：

- Telegram 私聊适合 draft update
- Console 适合 websocket push

所以正确抽象不是“统一一个 transport”，而是：

> 统一 stream 生产方式，分 channel 定义 stream 发布方式。

### 5.3 应优先发布 snapshot，而不是裸 delta

对 UI / transport 来说，直接发“到当前为止的完整文本 snapshot”通常比只发增量 token 更稳妥：

- 前端更简单，不必处理 token 拼接与重放。
- websocket 重连后更容易恢复。
- Markdown 增量渲染对 snapshot 更友好。
- Telegram draft update 天然更像“覆盖更新当前草稿文本”。

因此默认策略建议是：

- 内部按 delta 累积
- 对外按 throttled snapshot 发布

### 5.4 单次 ingress，单次执行

同一条用户消息不能同时被 `bus` 和 `streamhub` 各自“再处理一遍”。

正确边界应为：

- inbound message 只进入一个 ingress path
- executor / conversation worker 只执行一次
- 执行过程中分叉出两类输出：
  - canonical event
  - ephemeral snapshot

因此：

> `streamhub` 不是第二条 ingress path，而只是 executor 的一个 output sink。

### 5.5 V1 应保持最小实现

从第一性原理看，V1 真正不可省的只有三件事：

1. 同一条用户消息只执行一次
2. 只能把 `final.output` 流出来，不能流 raw JSON
3. 一个 run 最终只能对应一个可见回复

因此 V1 不需要先做一个通用 streaming 平台。  
更小、更稳的实现是：

- shared `final.output` extractor
- per-run `ReplySink`
- Console Local 的 in-process `StreamHub`

### 5.6 一个 run 只能拥有一个可见回复

真正需要防重的，不只是执行层，还有“最终用户看到几条消息”。

因此建议定义一个非常简单的约束：

- 一个 `run_id/task_id` 在一个 channel 上只允许绑定一个最终可见回复
- stream 期间只能更新这一个回复
- final 阶段只能 finalize 这一个回复，不能再额外创建第二个可见实体

这意味着：

- Telegram draft update 与 final send 不能各发一条
- Console stream bubble 与 final bubble 不能各 append 一条

---

## 6) 核心提议

### 6.1 引入最小共享抽象：ReplySink

建议在共享 runtime 层引入一个更小的抽象，而不是通用 stream broker：

```go
type ReplySink interface {
    Update(ctx context.Context, text string) error
    Finalize(ctx context.Context, text string) error
    Abort(ctx context.Context, reason error) error
}
```

这个接口表达的不是“平台能力”，而是：

- 同一个 run 只有一个可见回复
- `Update()` 更新这条回复
- `Finalize()` 收口这条回复
- `Abort()` 终止这条回复

### 6.2 引入共享 Final Output Extractor / Throttler

建议再配一个共享聚合器，负责：

1. 接收 `llm.StreamEvent`
2. 累积 `Delta`
3. 跟踪 `ToolCallDelta`
4. 依据节流策略决定何时发布 snapshot
5. 在 `Done` 时强制 flush

建议默认节流策略：

- `min_interval`: 250ms 到 500ms
- `min_chars`: 24 到 48
- `flush_on_newline`: true
- `final_flush`: always

这样可以避免：

- Telegram API 过于频繁更新
- Console websocket 事件风暴
- Markdown renderer 因过密重绘而抖动

### 6.3 统一接线点

最合适的接线点不是 provider 里，也不是 UI 里，而是 channel runtime 发起主 LLM 调用的地方。

也就是：

- Telegram task run path
- Console local task run path
- 未来其他 channel 的 task run path

共享 runtime 做的事应当是：

1. 把 `OnStream` 传给 `taskruntime.Run(...)`
2. 把 stream event 转成 accumulator 输入
3. 将 throttled snapshot 交给当前 run 的 `ReplySink`
4. 保留现有 final 结果装配与下游处理

### 6.4 Console 和 Telegram 的最小实现差异

- Console Local 需要 `StreamHub`，因为浏览器可能有多个订阅者。
- Telegram 不一定需要 hub；每个 run 直接创建一个 `TelegramReplySink` 即可。

也就是说：

- Console: `extractor -> StreamHub -> browser bubble`
- Telegram: `extractor -> TelegramReplySink -> same draft`

---

## 7) Channel 侧方案

### 7.1 Telegram 私聊

#### 7.1.1 范围

第一阶段只做私聊：

- `private`: 支持
- `group/supergroup`: 暂不启用

原因：

- 私聊噪音成本低
- API 行为更容易预期
- 回退策略更简单

#### 7.1.2 发布策略

Telegram stream publisher 的语义应为：

1. 为当前 run 创建一个 `TelegramReplySink`
2. 首次可见文本出现后在 sink 内创建 draft
3. 后续按节流策略更新同一个 draft
4. 完成时由同一个 sink finalize 这条 draft
5. 仅当 draft 不存在或 finalize 不可用时，才回退到普通最终发送

这保证：

- draft 只是“更快看到内容”的体验层
- 真正完成语义仍由 canonical final fact 决定
- Telegram 最终不会因为 stream + final path 变成两条可见消息

#### 7.1.3 回退语义

如果 draft API 不可用、失败、限流，必须：

- 停止 draft update
- 不中断主任务
- 继续走最终正常回复路径

### 7.2 Console Serve

#### 7.2.1 传输层

Console 适合用 websocket，而不是继续用轮询模拟流式。

建议新增一个后端推送通道，例如：

- `GET /api/stream?endpoint=<ref>`

连接建立后，浏览器订阅当前 endpoint 下的 stream event。

#### 7.2.2 事件模型

建议使用很小的一组事件类型：

- `task.started`
- `task.snapshot`
- `task.final`
- `task.error`

其中 `task.snapshot` 建议字段：

```json
{
  "type": "task.snapshot",
  "task_id": "t_123",
  "topic_id": "topic_abc",
  "seq": 7,
  "status": "running",
  "text": "当前累计到这里的完整文本"
}
```

注意这里发的是 snapshot，不是单个 token delta。

#### 7.2.3 前端行为

ChatView 应改成：

1. 任务提交后先插入本地 pending agent item
2. 若 websocket 可用，则根据 `task_id` 接收 `task.snapshot`
3. 每次 snapshot 到来时直接更新该 agent item 的 `text`
4. `task.final` 到来时 finalize 同一个 `task_id` 对应的 bubble，而不是 append 第二条 agent item
5. `MarkdownContent` 根据 `source` 变化重新渲染
6. websocket 不可用或断连时，退回现有 `/tasks/{id}` 轮询

这样可以做到：

- 新能力增量上线
- 老轮询逻辑仍是保底路径

---

## 8) 与 Bus 的边界

bus 继续只承载 canonical event：

- inbound normalized message
- final outbound text
- task failure / error publish
- plan progress（如果保留当前语义）

而以下内容不进 bus：

- token-level delta
- Telegram draft update
- Console websocket snapshot

同时还要补一条执行边界：

- inbound message 只能被一个 ingress owner 接收并执行
- `streamhub` 不能再次消费 inbound message 来“再跑一次”
- stream plane 只能消费 executor 产出的 snapshot

原因：

1. 这些都是高频、短暂、可丢失的体验层信号。
2. bus 的价值在于 canonical ordering / routing / idempotency，而不是高频 UI repaint。
3. 将流式 delta bus 化会明显增加复杂度，但不会显著提升最终一致性。
4. 若让 `bus` 和 `streamhub` 双重处理同一条入站消息，会天然引入重复执行与重复展示问题。

---

## 9) 建议的落地顺序

### Phase 1: Shared streaming core

- 在共享 runtime 增加 accumulator / throttler / publisher interface
- 打通 `taskruntime.RunRequest.OnStream`
- 确保 final output 装配、tool calling、parse、memory 路径不受影响

Acceptance:

- 不接 publisher 时，行为与现在一致
- 接 publisher 时，可得到稳定 snapshot 序列

### Phase 2: Console Local websocket streaming

- 后端新增 websocket stream endpoint
- Console local runtime 为 task 建立 in-process `StreamHub`
- ChatView 接入 websocket，并保留 polling fallback

Acceptance:

- Chat 页面可看到增量文本
- 同一个 `task_id` 最终只对应一个 agent bubble
- websocket 断开后仍可看到最终结果

### Phase 3: Telegram private-chat streaming

- 新增 Telegram `ReplySink`
- 私聊启用，群聊保持关闭
- 加入节流和失败回退

Acceptance:

- 私聊能看到增量更新
- 同一个 run 最终只产生一个可见回复
- draft 失败不影响最终正常回复

### Phase 4: Rollout hardening

- 指标、日志、开关
- 压测和限流验证
- 评估是否扩展到 group/supergroup 或其他 channel

---

## 10) 配置建议

建议增加独立开关，而不是一个模糊总开关：

```yaml
streaming:
  enabled: true
  min_interval_ms: 300
  min_chars: 32

telegram:
  stream_private_chat: true

console:
  websocket_stream: true
```

这样便于：

- 分 channel 回滚
- 独立调参
- 线上逐步放量

---

## 11) 观测与调试

建议至少记录：

- `stream_started`
- `stream_snapshot_published`
- `stream_publish_failed`
- `stream_completed`
- `stream_aborted`
- `stream_fallback_to_final_only`

关键维度：

- channel
- task_id
- conversation_key
- topic_id
- snapshot_count
- duration_ms
- input/output tokens

---

## 12) 风险

1. tool-call 与文本 delta 交错时，stream callback 的顺序处理容易出错。
2. Telegram draft update 过频可能触发限流。
3. Console websocket 如果只推 delta、不推 snapshot，会让前端状态恢复变复杂。
4. 若把 stream 当成 canonical 事实，容易污染现有 bus / task / memory 边界。
5. 若没有 per-run `ReplySink` 收口，Telegram 和 Console 都容易出现“stream 一份、final 又来一份”的重复显示。

对应缓解：

- 统一 accumulator
- snapshot 发布而非裸 delta
- 严格节流
- stream failure 不影响 final path
- 单次执行、双输出，但每个 run 只允许一个最终可见回复

---

## 13) 结论

这项能力不应被实现为“Telegram 特判”或“Console 特判”，而应被定义为：

> shared runtime 产出 stream，channel runtime 决定如何把 stream 变成用户可见的渐进式体验。

在这个模型下：

- `uniai` 提供 stream source
- Telegram 提供 `TelegramReplySink`
- Console 提供 `StreamHub + same-bubble finalize`
- bus 继续承载最终的 canonical message

更准确地说：

- `bus` 是 canonical plane
- streaming 是 executor 的 side path
- 二者由同一个 executor 产出，而不是各自重新处理一次消息

这是当前最稳、边界最清晰、也最容易逐步落地的方案。
