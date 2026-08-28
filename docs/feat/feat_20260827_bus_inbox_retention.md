---
date: 2026-08-27
title: Bus Inbox Seen Record Retention
status: proposed
---

# Bus Inbox Seen Record Retention

## 1. 结论

`contacts/bus_inbox.json` 是消息去重状态，不是聊天历史或审计日志。每条记录只需要覆盖平台可能重投同一消息的时间窗口，不应该永久保存。

采用一个固定规则：

- Bus inbox seen record 保留八天。
- 每次写入新记录时，在现有原子写入流程内删除超过八天的记录。
- 所有 Channel 使用同一个规则。
- 不新增配置、后台 goroutine、定时任务、手动清理命令或新的存储格式。

八天覆盖 Mixin 明确公布的七天 pending message 上限，也远长于 Telegram 公布的 24 小时 update 保留期。其他平台没有都公布严格的最长重投时间，因此任何有限保留期都不能提供数学上的永久去重保证。Morph 当前消息处理本来就是 at-least-once；八天是在去重价值和文件增长之间采用的固定工程边界。

## 2. 当前实现

`BusInboxRecord` 已包含所需字段：

```go
type BusInboxRecord struct {
    Channel           string
    PlatformMessageID string
    ConversationKey   string
    SeenAt            time.Time
}
```

去重 key 是：

```text
channel + platform_message_id
```

当前 `contacts.FileStore` 的行为是：

1. `GetBusInboxRecord` 读取完整的 `bus_inbox.json`，逐条查找 key。
2. `PutBusInboxRecord` 再次读取完整文件，替换或追加一条记录。
3. `saveBusInboxLocked` 排序后原子重写完整文件。
4. 没有任何删除条件。

因此记录数量只增不减。运行时间越久，查询、排序和重写的成本越高。

## 3. 记录的真实用途

Inbound flow 的顺序是：

```text
查询 seen record
  -> 未命中
  -> 发布到进程内 bus
  -> 写入 seen record
  -> Channel 向平台确认收到
```

seen record 主要处理以下情况：

- Morph 已处理消息，但平台 ACK 没有成功送达。
- Channel 连接断开后，平台重新发送未确认消息。
- 平台在短时间内重复投递同一个 event。

它不承担以下职责：

- 保存聊天历史。
- 记录任务和工具执行。
- 审计用户行为。
- 恢复七天或更早以前的会话。

这些职责已经由 chat history、task events 和 journal 承担。永久保存 seen record 没有额外业务价值。

## 4. 保留期依据

| Channel | 官方能够确认的事实 | 八天规则 |
| --- | --- | --- |
| Telegram | Bot API 的 incoming updates 最多保留 24 小时 | 覆盖 |
| Mixin | 未确认的 pending messages 最多保留七天 | 多留一天边界余量 |
| Slack | Socket Mode 要求 ACK，未确认时平台会 retry；官方页面未给出长期保留上限 | 采用统一规则 |
| LINE | webhook 可以在一段时间内重投，但官方不公开次数和间隔 | 采用统一规则，接受极晚重投可能重复 |
| Lark | 当前 Morph 通过长连接接收 event；不把平台重投当作永久消息存档 | 采用统一规则 |
| Discord | seen record 只是 Morph 侧去重状态 | 采用统一规则 |

LINE 官方明确表示重投次数和间隔不公开。因此不能证明八天覆盖所有未来重投。把 TTL 做成配置也不能消除这个不确定性，只会把内部存储策略暴露给用户。若平台在八天以后重新投递同一个 event，Morph 可能再次处理；这是本文接受的边界。

## 5. 最小实现

只修改 `contacts.FileStore.PutBusInboxRecord` 的现有 locked transaction：

1. 读取并 normalize 当前 records。
2. normalize 待写入 record。
3. 以待写入记录的 `SeenAt` 为当前时间基准，计算 `cutoff = SeenAt - 8 days`。
4. 在查找和替换 key 的同一次循环中，丢弃 `SeenAt < cutoff` 的记录。
5. 替换同 key 记录；不存在时追加。
6. 调用现有 `saveBusInboxLocked` 排序并原子写回。

边界规则：

- `SeenAt == cutoff` 的记录保留。
- 待写入记录始终保留。
- 时间在未来的已有记录保留，避免时钟回拨时误删。
- pruning 对所有 Channel 一致，不按 Channel 分支。
- 文件 schema 和 `busInboxFileVersion` 不变。

使用待写入记录的 `SeenAt`，是因为 inbound flow 已经在同一处生成当前 UTC 时间。这样无需给 FileStore 再增加 clock、scheduler 或新的公开方法。

## 6. 何时执行

只在 `PutBusInboxRecord` 时执行。

不在以下位置执行：

- `GetBusInboxRecord`：读操作不应产生文件写入。
- Morph 启动：没有必要让每个 runtime 同时发起一次清理。
- 后台 timer：没有新消息时，旧文件占用少量磁盘不会影响运行。
- CLI command：用户不应该维护内部去重状态。

升级后收到第一条新消息时，会读取一次旧文件，并在随后的写入中完成清理。之后每次写入本来就要遍历和重写保留记录，过滤过期项不会新增一次文件 I/O。

## 7. 并发和故障语义

pruning 保持在现有 `s.mu` 和 `fsstore.WithLock` 内，继续使用 `WriteJSONAtomic`。不能在锁外先删除或单独写文件，否则多个 runtime 可能互相覆盖记录。

清理失败时，`PutBusInboxRecord` 返回错误，Channel 不应 ACK 当前消息。平台随后重投，旧文件仍然可读，不会因为部分清理而损坏。

这项改动不改变 inbound flow 的 at-least-once 语义。如果进程在 bus publish 成功后、seen record 写入前退出，消息仍可能再次处理；retention 不能也不试图解决这个事务窗口。

## 8. 性能边界

该方案解决的是记录随运行年限无限累积的问题：文件只包含最近八天内处理的消息。

它没有改变 FileStore 的复杂度：

- 查询仍是 `O(n)`。
- 写入仍要排序并重写完整 JSON。
- 八天内消息量很大时，文件仍可能较大。

当前 Morph 是个人 Agent Runtime，不需要仅为了可能出现的高吞吐场景引入 SQLite、分片文件、append-only log 或内存索引。实现后应测量真实的 record 数、文件大小和写入耗时；只有八天窗口仍成为明确瓶颈时，才单独设计存储替换。

## 9. 代码和文档范围

需要修改：

- `contacts/file_store.go`：写入时过滤过期 inbox records。
- `contacts/file_store_test.go`：覆盖保留期和原子写入行为。
- `docs/bus.md`、`docs/bus_impl.md`：注明 inbox retention。

不需要修改：

- `BusInboxRecord`。
- `InboxStore` interface。
- 各 Channel runtime 和 adapter。
- `internal/bus/adapters.InboundFlow`。
- 配置文件、Console Settings、Runtime API 和 VitePress 页面。
- `bus_outbox.json`。Outbox 有发送状态和故障诊断用途，不属于本方案。

## 10. 测试

正式实现前先增加 table-driven tests：

| Case | 预期 |
| --- | --- |
| 新旧记录都在八天内 | 全部保留 |
| 旧记录早于 cutoff | 写入后删除 |
| 旧记录等于 cutoff | 保留 |
| 多个 Channel 混合 | 按时间统一清理，不影响 key |
| 新记录替换已有 key | 只保留更新后的记录 |
| 已有记录时间在未来 | 保留 |
| pruning 后保存失败 | 返回错误，原文件保持完整 |
| 旧版无过期控制的文件 | 首次成功 Put 后完成清理，version 不变 |

测试使用固定 `SeenAt`，不依赖 wall clock，也不需要 sleep。

## 11. 验收条件

1. `bus_inbox.json` 不再保留早于最新写入时间八天的 seen records。
2. cutoff 边界稳定，不因纳秒或本地时区产生差异。
3. Telegram、Slack、LINE、Lark、Discord 和后续 Mixin 使用相同实现。
4. 不增加配置项、清理线程、公开 store 方法或文件格式版本。
5. 文件写入仍在锁内并保持原子性。
6. 已有 inbox 去重测试和全仓 Go tests 通过。

## 12. 非目标

- exactly-once message processing。
- Bus inbox 高吞吐存储重构。
- 清理 `bus_outbox.json`、task、journal 或 chat history。
- 按 Channel 配置不同 TTL。
- 在 Console 展示或编辑内部去重记录。
- 保存平台 ACK 的完整生命周期。

## 13. 调研来源

- [Telegram Bot API](https://core.telegram.org/bots/api)：incoming updates 最多保留 24 小时。
- [Mixin Send and Receive Messages](https://developers.mixin.one/docs/app/getting-started/messages)：pending messages 最多保留七天。
- [Slack Socket Mode](https://api.slack.com/apis/connections/socket)：event acknowledgement 和 retry 语义。
- [LINE Webhook Redelivery](https://developers.line.biz/en/docs/messaging-api/receiving-messages)：重复 event、redelivery 以及未公开的次数和间隔。
