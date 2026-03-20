# Cross Short-Term Memory Aperture (同一用户跨会话短期记忆)

## 1. 背景

目标：在不推翻现有 memory 主结构的前提下，让同一用户在不同会话（例如 Telegram 私聊 + 群聊）中的短期记忆可被有条件地互相感知。

典型场景：

- 用户 A 在私聊里告诉 agent 一件事。
- 用户 A 在群聊再次提及时，agent 理应能记起私聊里的短期上下文。

当前行为偏离这个预期。

## 2. 现状梳理（基于当前实现）

### 2.1 写入与投影键

- WAL 事件里同时有 `session_id` 和 `subject_id`。
- 短期投影正文文件按 `(day, subject_id)` 分桶：
  - `memory/YYYY-MM-DD/{sanitize(subject_id)}.md`
- 各渠道 `subject_id` 当前主要是会话容器维度：
  - Telegram: `tg:<chat_id>`
  - Slack: `slack--<team>--<channel>`
  - LINE/Lark: `line--<chat_id>` / `lark--<chat_id>`

结论：同一人跨不同 chat/channel 时，会落在不同短期文件。

### 2.2 注入读取

- `BuildInjection(subjectID, reqCtx, maxItems)`：
  - 私聊上下文会注入 long-term；
  - short-term 当前从最近 N 天文件集合读取摘要。
- 现有 short-term 注入未以“同一用户”做稳定筛选。

### 2.3 已有可利用身份信号

WAL `MemoryEvent` 已有：

- `session_context.counterparty_id`
- `session_context.counterparty_handle`
- `participants[]`

短期 frontmatter 也支持 `contact_id/contact_nickname`，但当前 projector 写短期正文时只传 `SessionID`，未稳定补齐 `contact_id`。

## 3. 第一性原则与约束

### 3.1 第一性原则

- 检索优先级应是“谁（counterparty）”优先于“在哪说（session）”。
- 写路径要保持 WAL-first 简单性，不引入重型双写一致性问题。
- 跨会话共享应可控（开关 + 配额），避免污染当前会话上下文。

### 3.2 约束

- 不破坏现有 WAL 回放语义。
- 不重做历史所有文件结构。
- 不引入外部索引服务。

## 4. 方案对比

### A. 直接把 `subject_id` 改成人维度

优点：天然跨会话共享。

缺点：

- 会破坏当前 `(day, subject_id)` 语义与测试预期。
- 群聊上下文隔离被削弱，副作用大。
- 迁移成本高。

结论：不选。

### B. 双写两套完整短期正文（按会话 + 按人）

优点：检索直观。

缺点：

- 写放大与去重复杂度高。
- 排障和一致性成本高。

结论：当前阶段过度设计。

### C. 保留会话正文投影，同时新增“按人索引投影”（推荐）

优点：

- 主正文仍单源，风险低。
- 注入可以按人快速命中，避免每次全量扫文件。
- 与现有按天回放/清理机制兼容。

缺点：

- 需要维护一层额外索引文件。
- 历史数据需要逐步补齐索引。

结论：推荐。

## 5. 推荐设计（最小可行）

### 5.1 核心思路

保持不变：

- WAL schema 主语义不变。
- 短期正文继续按 `subject_id` 投影。

新增：

- projector 同步生成“按人索引投影”。
- 注入读取时改为：
  - 先读当前会话正文摘要（same-subject）。
  - 再通过按人索引补充跨会话摘要（same-counterparty）。

### 5.2 目录组织（日期在前）

短期正文（现有）：

- `memory/<day>/<subject>.md`

按人索引（新增）：

- `memory/<day>/by-person/<counterparty_ref>.md`

说明：

- 采用“日期在前”，与当前按天投影、按天回放、按天清理一致。
- 不建议“人在前”作为主目录，因为近期窗口处理会更别扭。

### 5.3 `counterparty_ref` 规范（建议）

格式：`<channel>:<stable_user_key>`

生成优先级：

1. `session_context.counterparty_id`
2. `session_context.counterparty_handle`（归一化）
3. `participants` 中可确定发送者标识（兜底）

约束：

- 先只做同渠道跨会话，不做跨平台 identity linking。
- 无法稳定生成时，回退为仅 same-subject 注入。

### 5.4 按人索引文件内容（建议）

按人索引文件只存“引用 + 轻摘要”，不复制完整正文。示例字段：

- `session_id`
- `subject_id`
- `rel_path`（指向 `memory/<day>/<subject>.md`）
- `summary`
- `updated_at`

这样避免正文双写和一致性问题。

### 5.5 注入选择策略

1. 先取 same-subject（当前会话）摘要。
2. `cross_short.enabled=true` 且有 `counterparty_ref` 时：
   - 读取最近 `memory.short_term_days` 的 `by-person` 文件；
   - 排除 `session_id == current session`；
   - 按时间降序补充。
3. 总量受 `memory.injection.max_items` 限制。
4. 跨会话补充还受 `memory.injection.cross_short.max_items` 子上限限制。

### 5.6 配置建议

```yaml
memory:
  injection:
    cross_short:
      enabled: false
      max_items: 10
```

说明：

- 默认 `false` 灰度上线。
- 主上限仍是 `memory.injection.max_items`。

### 5.7 代码落点（建议）

- `memory/projector.go`
  - 保持现有短期正文投影。
  - 新增 by-person 索引投影写入逻辑。
  - 写短期正文时补齐 `WriteMeta.ContactIDs`。
- `memory/inject.go`
  - 注入从“同会话 + 按人索引”组合加载。
- `internal/memoryruntime/orchestrator.go`
  - `PrepareInjectionRequest` 传入 `CounterpartyRef`（可空）。
- `internal/memoryruntime/adapter.go`
  - 增加可选接口供各渠道解析 `CounterpartyRef`。

## 6. 隐私与边界

- 只在同一 `counterparty_ref` 命中时放宽。
- 无身份锚点时不放宽。
- 不做跨渠道自动合并。
- 记录命中数量与文件，不输出敏感正文到日志。

## 7. 迁移与回放策略

### 7.1 无阻塞迁移

- 上线后新事件会逐步形成 `by-person` 索引文件。
- 历史无索引数据时，仍可用 same-subject。

### 7.2 可选补齐

- 提供“近 N 天回放重投影”补齐 by-person 索引。
- 不要求全量历史重建。

## 8. 测试计划

- 单测：
  - `counterparty_ref` 归一化与回退规则。
  - by-person 索引写入与去重。
  - aperture 开关与配额生效。
- 渠道测试（Telegram 重点）：
  - 同一 `from_user_id` 在私聊/群聊写入后，群聊可命中私聊短期索引。
  - 不同用户同群不串记忆。
- 回归：
  - WAL append/replay/checkpoint 语义不变。

## 9. 非目标（本次不做）

- 不改 long-term 文件组织（`memory/index.md` 保持现状）。
- 不引入外部数据库或搜索服务。
- 不做跨平台统一身份图谱。

## 10. 分阶段落地建议

Phase 1（最小可行）：

- projector 增加按人索引投影（日期在前目录）。
- 注入改为 same-subject + by-person 补充。
- 配置开关默认关闭，灰度验证。

Phase 2（按需优化）：

- 为 by-person 检索增加轻量缓存。
- 根据线上命中率调整 `cross_short.max_items`。
