---
date: 2026-06-14
title: 统一领域 Journal 需求
status: implemented
---

# 统一领域 Journal 需求

## 1) 背景

改造前有几类文件都放在 `file_state_dir` 下：

- `memory/log/*.jsonl`
- `tasks/console/log/*.jsonl`
- `tasks/<target>/log/tasks.jsonl`
- `logs/*.jsonl`

`logs/` 是运行观测日志，用于排障和 Console 日志页面。它不是业务状态的事实源，也不参与状态重建。

`memory/log` 和 `tasks/.../log` 更接近。它们都是 append-only JSONL，并且曾用于通过 replay 恢复当前视图：

- memory 从 `memory/log` 重建 `memory/*.md`。
- task 从 `tasks/.../log` 恢复 task/topic 视图。

问题是：同一种“事实源 + projection”模式现在有两套。后续如果再加入 turn/history、approval、compaction、pending input，会出现第三套事实源。

另一个相关目标是自我观测。系统需要能回答“某个 task 为什么失败”“某次 memory 写入来自哪里”“某个 topic 最近发生了什么”。这类问题不能只靠 `logs/` 的文本 tail，也不能把 debug log 混进业务事实源。正确边界是：journal 保存可重建业务事实，`logs/` 保存运行观测，两者通过稳定 id 关联。

## 2) 目标

引入一个统一领域 journal，作为可重建业务状态的事实源。

默认目录：

```text
<file_state_dir>/journal/
```

第一版覆盖两个现有事实源，并为自我观测保留必要关联信息：

- memory 事件
- task/topic 事件
- journal 事件与 `logs/` 的关联 id
- 未来 history replay 需要的可选关联字段

现有文件的角色变为 projection，或退出事实源路径：

- `memory/*.md` 是 memory projection。
- `tasks/<target>/projection.json` 是 task/topic projection snapshot。
- `memory/log` 和 `tasks/.../log` 不再写入、不再读取、不做导入兼容。
- `tasks/console/topic.json` 只做一次性 topic 迁移输入：当 `projection.json` 不存在时读取其中的 topic metadata，然后写入新的 projection snapshot；不迁移旧 task log。

## 3) 非目标

- 不替代 `logs/`。
- 不把 `logs/` 变成业务事实源。
- 不把 journal 变成 debug log。
- 不引入数据库。
- 不做分布式事件系统。
- 不支持多个进程同时写同一个 journal。
- 不重写 Console UI、memory UI 或 task API。
- 不在第一版实现 turn/history。

## 4) 第一性原理

统一 journal 只需要满足这些约束：

1. 状态变化必须先写 journal，再更新 projection。  
   如果进程崩溃，重启后可以从 journal 重建 projection。

2. journal 只追加，不原地修改历史事件。  
   删除、撤回、修正都写成新事件。

3. replay 顺序必须稳定，cursor 必须稳定。  
   cursor 不能指向会被 rename 的 active file。journal segment 一旦写入，文件名不能再变化。

4. projection 必须可重复执行。  
   同一组事件 replay 多次，应得到同样的状态。

5. journal envelope 稳定，payload 由领域拥有。  
   journal 层不理解 memory 或 task 的业务字段。

6. journal 和 `logs/` 必须能用稳定 id 关联。  
   自我观测不能依赖按时间猜测。

7. agent 不应直接扫描原始 journal 和 log 文件。  
   需要通过受控 projection 或 API 读取经过裁剪、脱敏和排序的观测结果。

8. journal 按敏感状态处理。  
   它可能包含用户输入、工具结果、memory 内容和 task 输出。

## 5) 存储形态

第一版使用稳定 segment 文件：

```text
<file_state_dir>/journal/
  events.000000000000000001.jsonl
  events.000000000000000002.jsonl
  index/
    task/<key>.jsonl
    topic/<key>.jsonl
```

不要使用 `events.jsonl` 作为长期 active file，也不要用 rename 方式 rotation：

- `events.000000000000000001.jsonl` 创建后，永远保持这个名字。
- segment 到达大小上限后，新事件写入下一个 segment。
- replay cursor 使用 `{file, byte, line}`。它表示最后处理事件之后的位置。`byte` 用于快速 resume；`line` 用于错误信息和人工排查。
- record ref 使用 `{file, byte, line}`。它表示单条事件的起始位置，用于 observation index 的按位置读取。
- checkpoint/snapshot 保存 replay cursor。启动时从这个 cursor 继续读。

不在第一版引入通用的 `journal/checkpoint/`。checkpoint 是 projection 的优化，不是 journal 的核心语义。

append 只负责写入事实，不为了计算 cursor 重新 replay journal。journal append 可以返回稳定 replay cursor 给领域 projection 保存 snapshot；这个 cursor 不暴露给用户工作流。

大 journal 下不能把“每次启动全量 replay”作为正常路径。每个会在启动时恢复状态的 projection 都必须有 snapshot：

- snapshot 保存读模型状态和最后处理到的 journal cursor。
- 启动时先加载 snapshot，再 replay cursor 之后的新事件。
- snapshot 缺失或被删除时，才允许从 journal 起点重建。
- journal 仍然是事实源，snapshot 只是可删除、可重建的加速文件。

同一个 journal 可以被同一进程的 memory/task 组件同时写入，所以 append 和 segment 切换必须串行化。第一版用 journal 目录下的 lock 文件保护 append 临界区；不支持多个独立进程长期同时写同一个 journal。

Observation 不能每次按 task/topic 全量扫描 journal。journal append 时写入最小索引，只记录 task/topic key 到 record ref 的映射。observation 先读索引，再按 ref 读取事件。index 不复制 event id、time、domain、type 或 trace，避免形成第二份事实。

## 6) Event Envelope

每条事件使用统一 envelope：

```json
{
  "id": "evt_...",
  "time": "2026-06-14T12:34:56.789Z",
  "domain": "task",
  "type": "updated",
  "schema_version": 1,
  "trace": {
    "trace_id": "tr_...",
    "runtime": "console",
    "target": "console",
    "topic_id": "topic_...",
    "task_id": "task_..."
  },
  "payload": {}
}
```

字段含义：

- `id`: event id，用于排查和去重。
- `time`: 写入时间，使用 UTC。
- `domain`: 领域，例如 `memory` 或 `task`。
- `type`: domain 内部事件类型，例如 `updated`。
- `schema_version`: payload schema 版本。
- `trace`: 用于把 journal 事件和 `logs/` 记录关联起来。
- `payload`: 领域 payload。

`domain` 负责路由，`type` 负责领域内事件名。事件名使用稳定字符串，例如 `domain=task,type=task_update`；不要再引入 `task.updated` 这类第二套命名格式。

`trace` 只放跨领域观测和 history replay 需要的关联字段，不放领域状态。第一版推荐字段：

- `trace_id`: 一次用户请求、后台任务或 runtime 处理链路的关联 id。
- `runtime`: 例如 `console`、`telegram`、`slack`、`line`、`lark`。
- `target`: task persistence target。
- `topic_id`: 如果事件属于某个 topic。
- `task_id`: 如果事件属于某个 task。

为未来 history 支持预留这些可选字段：

- `turn_id`: 一次 agent turn 的稳定 id。用于把 user input、assistant message、tool call、tool result、approval、compaction 串成同一条 turn timeline。
- `history_item_id`: 一条 history item 的稳定 id。用于后续支持 item 更新、compaction 引用、UI replay。
- `parent_item_id`: 当前 item 依附的上一层 item。例如 tool result 可以指向对应 assistant tool call item。
- `tool_call_id`: 工具调用 id。用于保证 tool call 和 tool result 配对。
- `request_id`: LLM provider request id 或内部 request id。用于和 `/logs` 中的 request/response 排障信息关联。

这些字段只在文档里保留方向，第一版 Go 结构不提前声明它们。JSON reader 遇到这些未知字段时应自然忽略。第一版不需要 `aggregate_type`、`aggregate_id`、`source` 这些更宽的通用字段。memory id、task id、topic id 等业务状态仍放在各自 payload 里；`trace` 只保留用于关联的副本，不能成为 projection 的事实来源。

不预留单独的 `seq` 字段。journal 的事实顺序来自文件顺序和 line 顺序。后续如果需要对外暴露 cursor，可以由 reader 根据文件名和 line 生成，不要让业务 writer 维护第二套顺序。

## 7) 第一版事件范围

Memory：

- `record`

Task/topic：

- `task_upsert`
- `task_update`
- `topic_upsert`
- `topic_title_updated`
- `topic_deleted`

第一版不定义 turn/history 事件。文档只保留方向：未来 turn/history 应写入同一个 journal，而不是写入 `logs/` 或再开一套事实源。

## 8) Projection 边界

journal 写事实。projection 生成读模型。

Memory projection：

- 输入：`domain=memory` 的事件。
- 输出：`memory/*.md`、memory index。
- 删除 projection 后，可以从 journal 重建。
- 常规启动从 memory checkpoint 之后继续 replay，不能从 journal 起点扫描。

Task projection：

- 输入：`domain=task` 的事件。
- 输出：runtime 内存 task view、topic view，以及现有 API 的读取结果。
- `tasks/console/topic.json` 不再写入。旧文件只在没有 `projection.json` 时作为 topic metadata 迁移来源。
- 常规启动先读 task/topic snapshot，再从 snapshot cursor 继续 replay。

`logs/` 不参与 memory/task 的状态 projection。

Observation projection：

- 输入：journal index、journal 事件和 `logs/` 记录。
- 关联：优先使用 `trace.trace_id`，其次使用 `trace.task_id`、`trace.topic_id` 等稳定 id。
- 输出：按时间排序、裁剪、脱敏后的观测视图。
- 第一版只需要支持按 task/topic 查看相关事件和日志摘要。
- Observation projection 不是业务事实源；删除它不能影响 memory/task 恢复。

## 9) 与 `logs/` 的关系

`logs/` 继续记录运行细节：

- slog 文本或 JSONL
- debug 信息
- tool 参数摘要
- request/response 排障信息
- Console `/logs` 页面

journal 记录业务事实：

- memory state
- task/topic state
- 后续 turn/history state

第一版要求：journal 事件带稳定 `trace_id`。Console local runtime 的 task log record 会带同一个 `trace_id`。其它 log record 如果没有 `trace_id`，不参与强关联；observation 仍可通过 `task_id`、`topic_id` 读取 journal 侧事实。

`logs/` 可以按 `logging.file.max_age` 删除。删除 `logs/` 只会损失运行细节，不应影响 journal replay 和业务状态恢复。

## 10) 实施步骤

### Phase 1: Journal 基础能力

- 新增 journal writer/reader。
- 支持 append JSONL。
- 支持稳定 segment 文件。
- 支持 `ReplayFrom(cursor)`，从 cursor 对应 segment 开始读，不从 journal 起点扫描。
- 校验 envelope 必填字段。
- 支持写入 `trace.trace_id`。
- 支持 task/topic observation index。
- 单元测试覆盖写入、读取、坏行处理和 replay 顺序。

### Phase 2: 替换 memory/task 写入

- memory 写入统一 journal，不再写旧 `memory/log`。
- task 写入统一 journal，不再写旧 `tasks/.../log`。
- journal 写失败时，本次业务状态变更失败。
- 增加 replay 测试，确认 journal 能重建 memory/task 状态。
- Console local runtime log record 写入同一个 `trace_id`，用于后续观测关联。

### Phase 3: 切换重建来源与观测读取

- 启动时先读 projection snapshot，再从 journal cursor 之后补齐 memory/task projection。
- 如果 journal 不存在，按空状态启动。
- 增加最小 observation projection/API，按 task/topic index 返回 journal 事件和相关 log 摘要。

## 11) 结合现有代码的实施 Checklist

### 11.1 Journal 基础层

- [x] 新增统一 journal 包，建议位置：`internal/domainjournal`。
- [x] 定义最小 envelope：`id`、`time`、`domain`、`type`、`schema_version`、`trace`、`payload`。
- [x] 定义 `Trace` 结构，包含 `trace_id`、`runtime`、`target`、`topic_id`、`task_id`；history 相关字段只保留在文档里。
- [x] 使用稳定 segment 文件写 journal，不再依赖会 rename active file 的 JSONL rotation。
- [x] 复用或搬迁 `memory/journal.go` 里的 replay 排序、cursor、坏行报错测试思路，但不要让新的通用 journal 依赖 `memory.MemoryEvent`。
- [x] 增加 `internal/statepaths` helper：`JournalDir()` 和 `JournalEventsPath()`。
- [x] 在 `assets/config/config.example.yaml` 加 `journal.dir_name: "journal"`。
- [x] 增加基础测试：append、read/replay、坏 JSONL、缺必填字段、未知字段兼容、segment 顺序、`ReplayFrom(cursor)` 不扫描旧 segment。

### 11.2 Memory 接入

- [x] 用 unified journal 替换 memory record 的写入路径。Console 当前写入点在 `cmd/mistermorph/consolecmd/local_runtime.go` 调用 `generation.memRuntime.Orchestrator.Record(...)` 附近。
- [x] 把现有 `memory.MemoryEvent` 包进统一 envelope：`domain=memory`，`type=record`，payload 为当前 memory event。
- [x] `trace.trace_id` 优先使用当前 run id；`trace.task_id` 使用 console/channel task id；`trace.topic_id` 使用 console topic id。
- [x] journal 写失败时，本次 memory record 失败。
- [x] memory projector 从 unified journal 的 checkpoint cursor 后读取 `domain=memory` 的事件，不再从 journal 起点扫描。
- [x] 增加 replay 测试：从 unified journal replay 后得到预期 memory projection 输入。

### 11.3 Task 接入

- [x] 给 `internal/daemonruntime.ConsoleFileStoreOptions` 和 `FileTaskStoreOptions` 增加可选 unified journal writer。
- [x] 在 `ConsoleFileStore.UpsertWithTrigger` 中，用 unified journal 替换旧 task log append。
- [x] 在 `ConsoleFileStore.UpdateWithTrigger` 中，用 unified journal 替换旧 task log append。
- [x] 在 `ConsoleFileStore.CreateTopic`、`DeleteTopic`、`SetTopicTitle` / `SetTopicTitleFromLLM` 中写 topic 事件。
- [x] 在 `FileTaskStore.RecordTaskUpsert` 和 `RecordTaskUpdate` 中写 unified journal。
- [x] task 事件使用 `domain=task`，type 使用 `task_upsert`、`task_update`、`topic_upsert`、`topic_title_updated`、`topic_deleted`。
- [x] payload 继续复用 `daemonruntime.TaskInfo`、`TaskTrigger`、`TopicInfo` 的现有字段，不把业务状态放进 `trace`。
- [x] task/topic 使用 projection snapshot 做常规启动恢复。
- [x] 增加 task replay 测试：有 snapshot 时从 snapshot cursor 后继续 replay，不解析旧 segment。

### 11.4 Trace 与 `logs/` 关联

- [x] 明确 `trace_id` 来源。第一版复用 task trigger 的 `trace_id`；没有 `trace_id` 时用 task id 作为弱关联值。
- [x] 在 console local runtime 创建 task/job 时把 `trace_id` 写入 task trigger 或运行上下文。
- [x] Console local runtime task log record 带同一个 `trace_id`。
- [x] `logging.file` 输出不因缺少 `trace_id` 失败；缺少 `trace_id` 的 log record 不参与强关联。
- [x] 增加测试：同一个 task 的 journal event 和 log/event payload 能通过 `trace_id` 关联。

### 11.5 Observation 读取

- [x] 新增最小 reader/API，按 `task_id` 或 `topic_id` 返回 observation view。
- [x] observation view 读取 journal index 指向的事件，并按 `trace_id` 关联 `/logs` 摘要。
- [x] 输出前做截断和脱敏；禁止把完整原始 `logs/` 注入 prompt。
- [x] observation 读取失败不能影响 memory/task replay。
- [x] Console 第一版可以只展示或返回 JSON，不要求重做 UI。

### 11.6 替换与清理

- [x] 新安装默认写 unified journal。
- [x] 不再写旧 `memory/log` 和 `tasks/.../log`。
- [x] 启动时从 projection snapshot + unified journal cursor 重建 memory/task projection。
- [x] 只迁移旧 `tasks/console/topic.json` 的 topic metadata，不迁移旧 task log。
- [x] 不再写 `tasks/console/topic.json`。
- [x] 如果 unified journal 不存在，按空状态启动。
- [x] 删除或停用 runtime 对旧 log replay 的依赖。

## 12) 配置

第一版尽量不新增用户需要理解的配置。

可接受的最小配置：

```yaml
journal:
  dir_name: "journal"
```

不需要第一版提供 `enabled` 或 `dual_write`。不做向上兼容，也不提供双写开关。

## 13) 错误处理

写入错误：

- journal append 失败时，本次业务状态变更失败。
- 不允许 projection 已更新但 journal 写入失败。

读取错误：

- 坏 JSONL 不静默跳过。
- 错误信息应包含文件名和 line。

未知事件：

- 未知 `domain` 可以跳过。
- 已知 `domain` 但未知 `type` 默认失败，避免 projection 悄悄缺状态。

观测读取：

- observation projection 读取失败不应影响 journal replay。
- 输出给 agent 或 UI 前必须做截断和脱敏。
- 不允许把完整原始 `logs/` 作为 prompt 注入。

## 14) 验收标准

第一版完成后应满足：

- 新安装时 memory 和 task 事件都会写入稳定 journal segment。
- 删除 `memory/*.md` 后，可以从 journal 重建 memory projection。
- 重启后 task/topic 状态默认从 snapshot 恢复，只 replay snapshot cursor 后的新事件。
- Console topic 删除、重命名、task 完成状态可以从 journal replay 得到同样结果。
- 删除或轮转 `logs/` 不影响 memory/task 恢复。
- Console local runtime 的 journal 事件和同一处理链路 log record 有共同 `trace_id`。
- 可以按 task/topic 从 journal index 获取一份裁剪后的观测视图，包含 journal 事件和相关 log 摘要。
- 旧 `memory/log` 和 `tasks/.../log` 不参与读取、导入或恢复。
- 单元测试覆盖 journal append/read/replay 的关键路径。

## 15) 后续方向

后续可以把这些事件也迁入 journal：

- turn started/completed/interrupted/failed
- user input recorded
- assistant message recorded
- tool call started/completed
- approval required/resumed/denied
- compaction created
- pending input queued/applied
- turn diff updated
- memory delete/tombstone
- topic tombstone

这些不属于第一版。第一版只要证明 memory/task 可以从同一个事实源重建。

## 16) 最小结论

统一 journal 可以替代 `memory/log` 和 `tasks/.../log` 成为业务状态事实源。

第一版不要做通用 event framework。只做本地 append-only stable segment、一个最小 envelope、memory/task 写入替换、projection snapshot、cursor replay，以及按 task/topic index 的最小观测视图。

`logs/` 保持运行观测职责。journal 不取代 `logs/`，`logs/` 也不取代 journal。两者通过 `trace_id` 关联。
