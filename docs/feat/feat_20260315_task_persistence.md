---
date: 2026-03-15
title: 任务持久化（按 Target 选择开启，JSONL 文件存储）
status: implemented
---

# 任务持久化（按 Target 选择开启，JSONL 文件存储）

## 1) 背景与当前状态

这个 feature 已经有可运行实现；本文档现在以“当前实现快照 + 剩余尾项”为主，而不是纯设计草案。

历史问题：

- `console local runtime`、`serve`、Telegram/Slack/LINE/Lark 的任务视图原先都主要依赖内存，重启后历史不可恢复。
- console 缺少 topic 元数据和 topic 级 chat 恢复能力。

当前实现：

- `console local runtime` 使用 `ConsoleFileStore`；开启 `tasks.persistence_targets: ["console"]` 后，会维护内存态 task/topic 视图，并落盘到 `topic.json` 与每日 topic 日志。
- `serve`、Telegram/Slack/LINE/Lark 通过 `daemonruntime.NewTaskViewForTarget(...)` 选择 `MemoryStore` 或 `FileTaskStore`；开启持久化时，任务事件写入统一 `tasks.jsonl` 日志并回放到内存视图。
- `cmd/mistermorph/daemoncmd.TaskStore` 仍负责执行队列、approval/resume 与 worker 协调；新的 file-backed store 只负责元数据视图、回放与查询。
- console UI 已支持 topic 二级 sidebar、新 topic 自动创建、topic 删除、隐藏 heartbeat topic 切换，以及首轮成功任务后的异步 topic 重命名。

## 2) 第一性原理

任务持久化在当前阶段只需要满足 4 个不可约约束：

1. 重启后任务不可丢。  
2. 文件存储下写入必须低成本且可靠。  
3. 当前读取核心是“按 topic 看会话 + 按时间倒序列出”，不是复杂聚合检索。  
4. 当前删除主诉求是“删 topic 可见性”，不是高频物理删单条任务。  

由这 4 条约束可直接推导出最小方案：

- 事实源使用 append-only JSONL（避免大文件全量覆写）。
- console 按天+topic 分文件；其他 target 走统一日志并 rotation。
- topic 元数据与 task 分离（`topic.json` + `task.topic_id`）。
- 不做 projection，不做本地搜索索引；启动回放即可。
- `GET /tasks` 固定按 `created_at desc` 返回；`GET /topics` 固定按 `updated_at desc` 返回。
- topic 删除走 tombstone，物理清理延后到 compact。
- 每条任务事件带 `trigger`，标识来源（ui/heartbeat/webhook/api/system）。

## 3) 本轮共识

1. 开关采用 `tasks.persistence_targets: ["console", "telegram"]` 这种白名单形式。  
2. 这期不做 projection。  
3. `TaskInfo` 结构只新增 `topic_id`，不引入 `topic_title/deleted_at/search_text` 等字段。  
4. 删除优先满足“删 topic”场景，并使用逻辑删除（tombstone）更稳妥。  
5. Console 任务日志文件名不需要 `seq`，同一 `topic_id` 不做 rotation。  

## 4) 非目标

- 这期不做内建全文搜索索引。
- 这期不做 embedding 检索库。
- 这期不做运行中任务断点续跑。

## 4.1 TaskStore 边界

新 `TaskStore` 只负责任务持久化相关能力：

- 任务事件落盘
- 启动回放恢复内存视图
- topic 元数据读写与逻辑删除
- 任务与 topic 的查询/排序视图

说明：

- `internal/daemonruntime.MemoryStore` 会被持久化视图替代，但语义仍保持为“任务元数据视图”。
- `cmd/mistermorph/daemoncmd.TaskStore` 的执行队列、worker 协调、resume/pending 控制不归新 `TaskStore` 接管。
- heartbeat loop 继续留在各 runtime/daemon 执行层；console local runtime 也需要独立 heartbeat loop。

## 4.2 当前实现架构

```text
submit/inbound event
  -> runtime-owned execution lane
     - console: ConversationRunner keyed by console:<topic_id>
     - serve: cmd/.../daemoncmd.TaskStore queue
     - channels: ConversationRunner keyed by conversation
  -> daemonruntime.TaskView update
     - queued / running / pending / done / failed / canceled
  -> optional file append
     - console: ConsoleFileStore
       - topic.json
       - log/YYYY-MM-DD_<topic_key>.jsonl
     - serve/telegram/slack/line/lark: FileTaskStore
       - log/tasks.jsonl (+ rotation)
  -> runtime API reads
     - /tasks
     - /tasks/{id}
     - /topics (only when runtime provides TopicReader)
```

关键点：

- `TaskView` 同时维护内存态查询视图；文件只是事实源，不是每次读取都直接扫盘。
- `ConsoleFileStore` 额外持有 topic 元数据与 heartbeat topic 过滤逻辑。
- `/topics` 路由是通用 handler，但当前真正提供 topic 列表/删除的是 Console Local runtime。

## 5) 存储设计

### 5.1 目录布局

```text
<file_state_dir>/
  tasks/
    console/
      topic.json
      log/
        <YYYY-MM-DD>_<topic_key>.jsonl
    serve/
      log/
        tasks.jsonl
        tasks.jsonl.1
    telegram/
      log/
        tasks.jsonl
        tasks.jsonl.1
    slack/
      log/
        tasks.jsonl
    line/
      log/
        tasks.jsonl
    lark/
      log/
        tasks.jsonl
```

说明：

- `console`：按“日期 + topic”分文件，不做 rotation，不带 `seq`。
- 其他 target：单流 `tasks.jsonl`，超过 `tasks.rotate_max_bytes` 后按 `tasks.jsonl.N` 追加轮转。
- `topic_key` 由 `topic_id` 运行时计算（建议 URL-escape 或安全字符映射），避免文件名非法字符。

### 5.2 `topic.json`

`tasks/console/topic.json` 作为 topic 元数据文件：

```json
{
  "version": 1,
  "updated_at": "2026-03-15T12:00:00Z",
  "items": [
    {
      "id": "topic_20260315_abc123",
      "title": "Quarterly planning",
      "created_at": "2026-03-15T11:00:00Z",
      "updated_at": "2026-03-15T12:00:00Z",
      "deleted_at": ""
    }
  ]
}
```

语义：

- `deleted_at` 非空表示 topic 逻辑删除。
- `updated_at` 表示 topic 最近活跃时间，用于 `/topics` 排序。
- topic 提交任务、title 变更、逻辑删除时刷新 `updated_at`。
- 默认列表不返回已删除 topic。
- `default` 只作为兼容旧任务/空 topic 的保底 id；当前 UI 不再预置或主动展示这个 topic，除非确实存在任务落在其上。
- 不持久化 `topic_key`，由 `topic_id` 即时计算文件名映射。

### 5.3 JSONL 行格式（任务事件）

建议每行写“任务快照事件”，示例：

```json
{
  "type": "task_upsert",
  "at": "2026-03-15T12:00:00Z",
  "channel": "console",
  "trigger": {
    "source": "ui",
    "event": "chat_submit",
    "ref": "web/console"
  },
  "task": {
    "id": "console_xxx",
    "status": "done",
    "task": "hello",
    "model": "gpt-5.2",
    "timeout": "10m0s",
    "created_at": "2026-03-15T11:59:00Z",
    "started_at": "2026-03-15T11:59:01Z",
    "finished_at": "2026-03-15T11:59:10Z",
    "error": "",
    "result": {"output": "..."},
    "topic_id": "default"
  }
}
```

补充说明：

- console 事件 envelope 当前使用 `channel: "console"`。
- 共享 `FileTaskStore` 事件 envelope 使用 `target: "<serve|telegram|slack|line|lark>"`。
- 两者的核心负载都是 `task` 快照 + 可选 `trigger`，启动时统一回放成内存态 `TaskView`。

可预留（非必须）：

- `type: "task_deleted"`（未来若要支持单任务删除）。

`trigger` 字段约定：

- `source`: `ui | heartbeat | webhook | api | system`
- `event`: 触发动作名（例如 `chat_submit`、`heartbeat_tick`、`webhook_inbound`）
- `ref`: 可选来源标识（例如路由名、webhook path、job id）

默认值建议：

- console UI 提交：`source=ui`
- heartbeat 触发：`source=heartbeat`
- webhook 入站触发：`source=webhook`
- 外部 API 提交：`source=api`
- Telegram poll 入站：`source=system`, `event=poll_inbound`
- Slack/LINE/Lark webhook 入站：`source=webhook`, `event=webhook_inbound`

与 console chat 呈现关系：

- 当前 chat 是“每个 task 对应一轮 user+assistant 展示”。
- 只要 `task.task/status/result/error/topic_id/created_at` 完整，重启后即可重建现有 chat 视图。
- console 的 `conversation_key` / memory subject key 需要包含 `topic_id`，例如 `console:<topic_id>`。
- console heartbeat 任务进入保留 topic（例如 `_heartbeat`）；该 topic 默认不作为当前可见 topic，但用户在 UI 显式切换后可以查看。
- `trigger` 可用于后续在 UI 标记“该任务是否来自 UI/heartbeat/webhook”。

## 6) 配置设计

```yaml
tasks:
  dir_name: "tasks"
  persistence_targets: ["console"]
  rotate_max_bytes: 67108864
  targets:
    console:
      heartbeat_topic_id: "_heartbeat"
```

默认行为：

- 默认只有 `console` 开启持久化。
- 其他 target 默认关闭，需要时加入白名单。

## 7) API 设计（本期）

### 7.1 提交任务

`POST /tasks`

新增可选字段：

- `topic_id`
- `topic_title`
- `trigger`（可选；未传则由 runtime 自动填充）

规则：

- 若 `topic_id` 为空：自动创建新 topic，并回填 `topic_id`。
- console 中每个 task 都必须带 `topic_id`。
- `topic_title` 只用于更新 `topic.json`，不写入 `TaskInfo`。
- 当前实现里，若未显式传 `topic_title`，runtime 会先用首条 task 文本裁剪生成初始 title。
- 当前实现里，新 topic 首轮任务成功完成后，会额外调用一次 LLM 生成更合适的 title，并异步更新 `topic.json`（不阻塞主任务返回）。
- `trigger` 未传时按运行上下文自动写入（例如 console chat 默认 `source=ui`）。
- 响应体只需回传 `topic_id`，便于 console 侧建立当前 topic 上下文。

### 7.2 列表任务

`GET /tasks`

保留：

- `status`
- `limit`
- `topic_id`（console 场景）

说明：

- 这期不加内建全文搜索参数。
- 搜索需求先通过文件工具（`rg/jq`）解决。
- 返回顺序固定为 `created_at desc`。

### 7.3 Topic 列表

`GET /topics`

当 runtime 提供 `TopicReader` 时可用。当前 Console Local runtime 返回 `topic.json` 中未删除的 topic，并按 `updated_at desc` 排序。

### 7.4 删除 Topic

`DELETE /topics/{topic_id}`

语义：

- 逻辑删除 topic（写 `topic.json.deleted_at`）。
- 默认任务列表过滤该 topic 下任务。
- 本期不做物理清理；后续可加 `compact` 命令做离线清理。
- 当前 Console Local runtime 已实现；其他 runtime 若未注入 `TopicDeleter` 会返回 `503`。

## 8) 启动恢复语义

1. 读取本 channel 对应日志文件并回放到内存视图。  
2. 对非终态任务（`queued/running/pending`）统一收敛为 `canceled`，错误为 `runtime restarted`。  
3. 将收敛后的快照追加写回对应 JSONL。  

排序规则：

- `GET /tasks` 按 `created_at desc` 返回。
- `GET /topics` 按 `updated_at desc` 返回。

## 9) 为什么本期不做 Projection

本期范围下，projection 不是必需：

- 任务量阶段性可控；
- 暂无强搜索需求；
- 使用文件工具即可满足排查；
- 不做 projection 可显著降低实现复杂度和维护成本。

后续如果出现以下信号，再引入 projection：

- 任务量持续增长导致启动回放明显变慢；
- Console 内建搜索/聚合需求变重；
- 需要更稳定的分页统计能力。

## 10) 风险与控制

- 风险：console 单 topic 文件长期增长。  
控制：按天分文件（`YYYY-MM-DD_topic`），天然切分。

- 风险：topic 逻辑删除后文件仍在。  
控制：先逻辑删除保证安全，后续提供离线 compact。

- 风险：topic_id 含非法文件名字符。  
控制：统一 `topic_key` 映射规则；`topic.json` 只保留原始 `topic_id`。

## 11) DoD

- `tasks.persistence_targets` 生效，默认仅 `console`。  
- console 可按 topic 写入并在重启后可见。  
- console topic 删除为逻辑删除，并在列表层生效。  
- 服务端提供 `/topics` 与 `DELETE /topics/{topic_id}`。  
- tasks 按 `created_at desc` 返回；topics 按 `updated_at desc` 返回。  
- console local runtime 可按配置运行 heartbeat loop。  
- console heartbeat 任务写入保留 topic，并可在 UI 显式切换查看。  
- 其他 target 可选开启，写入统一 `tasks.jsonl` 轮转日志。  
- 文档与配置模板同步（`assets/config/config.example.yaml`、`docs/console.md`）。  

## 12) 任务拆分（建议实施顺序）

### Phase 0: 设计收敛

- [x] 将配置键统一为 `tasks.persistence_targets`，并明确支持值包含 `console/serve/telegram/slack/line/lark`。
- [x] 保持 `TaskInfo` 最小增量字段集合，仅补 `topic_id`；`trigger` 留在事件层，`topic.updated_at` 留在 topic 元数据层。
- [x] 固化 console 的 `topic_id -> conversation_key -> memory subject` 映射规则，统一为 `console:<topic_id>`。
- [x] 将 `POST /tasks` 的返回体扩展为回传 `topic_id`。
- [x] 固化重启语义：`pending` / approval 任务统一收敛为 `canceled`。

### Phase 1: 核心模型与配置

- [x] 新增 `tasks.*` 配置解析与默认值，更新 `assets/config/config.example.yaml`。
- [x] 为任务持久化补齐路径解析工具，统一生成 `<file_state_dir>/tasks/...` 目录。
- [x] 扩展 `daemonruntime.TaskInfo`、`SubmitTaskRequest`、`SubmitTaskResponse` 以及相关 JSON 序列化结构。
- [x] 定义 topic 元数据结构、任务事件结构、trigger 结构和必要的辅助类型。
- [x] 抽象统一的任务读写接口，替代当前直接绑定 `MemoryStore` 的调用点。

### Phase 2: 持久化存储内核

- [x] 实现 append-only JSONL writer，支持原子追加、目录初始化和基础错误处理。
- [x] 实现 console 专用日志路径选择器（按天 + topic 分文件）。
- [x] 实现其他 runtime 的共享日志与 rotation 策略。
- [x] 实现启动回放，将事件恢复为内存视图。
- [x] 实现非终态任务在回放后的收敛写回逻辑。
- [x] 实现 `topic.json` 的读写、逻辑删除和默认过滤。

### Phase 3: Runtime 接入

- [x] 将 `internal/daemonruntime.MemoryStore` 替换为新的持久化任务视图实现，保留现有读写语义。
- [x] 重构 `cmd/mistermorph/daemoncmd.TaskStore`，让新 `TaskStore` 仅承接持久化/回放/查询职责，执行队列继续独立。
- [x] 接入 console local runtime，提交/更新任务时写入持久化事件。
- [x] 为 console 引入 topic-aware conversation key，并同步修正 memory record/injection 的 subject。
- [x] 为 console local runtime 增加 heartbeat loop，并将 heartbeat 任务写入保留 topic（例如 `_heartbeat`）。
- [x] 接入 Telegram/Slack/LINE/Lark/serve，按白名单开关决定是否落盘。
- [x] 为各 runtime 注入默认 trigger 信息。

### Phase 4: API 与 Console UI

- [x] 扩展 `POST /tasks`、`GET /tasks` 参数与返回结构，补齐 `topic_id` 语义。
- [x] 新增 `GET /topics` 和 `DELETE /topics/{topic_id}`。
- [x] 调整任务列表与 chat 历史读取逻辑，保持 tasks 按 `created_at`、topics 按 `updated_at` 工作。
- [x] 为 heartbeat 保留 topic 增加显式切换入口，但默认不作为当前可见 topic。
- [x] 为 console 增加 topic 列表、topic 切换、topic 删除后的隐藏逻辑。
- [x] 接入新 topic 首轮任务完成后的异步命名流程。

### Phase 5: 测试与文档

- [x] 为任务存储补齐核心单元测试：回放、topic 过滤、tombstone、rotation。
- [x] 为 daemon HTTP 路由补齐接口测试：提交、列表、topic 列表、topic 删除。
- [ ] 为任务存储补齐更细的排序与边界条件测试。
- [ ] 为 console/serve/channel runtime 补齐重启恢复测试，覆盖非终态任务收敛。
- [ ] 为 approval 相关场景补齐回归测试，确认重启后行为符合设计。
- [x] 同步更新 `docs/console.md`、架构文档和相关 feature 文档。
- [ ] 补一份单独的运维说明（rotation/compact/恢复排查）。
