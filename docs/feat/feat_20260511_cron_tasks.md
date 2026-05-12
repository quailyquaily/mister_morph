---
date: 2026-05-11
title: Cron Task Service
status: draft
---

# Feature: Cron Task Service

## 概要

新增一个分钟级 cron 服务，用于长期运行的 agent runtime。该服务以 `cron.yaml` 作为唯一任务数据源，替换现有 `TODO.md`、`TODO.DONE.md`、`TODO.RECUR.md` 工作流。

每个 tick 检查到期任务。到期任务会按 awareness 语义提交，`behavior` 为 `cron`，任务内容作为 awareness task text。

## 目标

- 加载并解析 `cron.yaml`。
- 支持一次性任务和重复任务。
- 支持类 cron 时间表达式，并支持 IANA 时区和 `UTC+8` 这类固定偏移时区。
- 任务内容沿用当前 TODO item 的内容风格，支持 `[John](tg:@john)` 这类 ref 语法。
- cron 服务每分钟 tick 一次。
- 每个到期任务都以 awareness 语义提交，`behavior` 为 `cron`。
- 同一个 tick 内有多个任务时，按 `cron.yaml` 中的顺序执行，不并行跑 cron 任务。
- 同一任务已经在队列或执行中时，跳过本次触发。
- 一次性任务成功完成后，从 `cron.yaml` 删除。
- `todo_update` 改为操作 `cron.yaml`，不再写 TODO 文件。

## 非目标

- 不兼容旧的 `TODO.md`、`TODO.DONE.md`、`TODO.RECUR.md` 机制。
- 不实现完成任务归档文件。
- 不支持秒级调度。
- 不回放进程停止期间错过的每一次重复任务触发。
- 第一版不新增 cron 专用 LLM route；cron 使用 awareness route。

## 配置

新增配置：

```yaml
cron:
  enabled: true
```

`cron.enabled` 默认 `true`。当 `cron.yaml` 不存在时，服务自然 no-op；当 `cron.enabled=false` 时，即使 `cron.yaml` 存在也不加载、不调度。

## 数据源

`cron.yaml` 位于 `file_state_dir` 下，是定时任务状态的唯一来源。

如果文件不存在，runtime 视为空任务表。写入必须是原子写：在同一目录写临时文件，然后 rename 覆盖 `cron.yaml`。

建议根结构：

```yaml
version: 1
tasks:
  - id: submit-report
    at: "2026-05-12 09:00"
    tz: "Asia/Tokyo"
    content: "Remind [John](tg:@johnwick) to submit the report."

  - id: weekly-invoice-review
    cron: "0 10 * * 1"
    tz: "UTC+8"
    content: "Review open invoices."
```

任务字段：

- `id`：必填，稳定标识。用于队列去重、更新、删除和日志。
- `at`：用于一次性任务，格式为 `YYYY-MM-DD HH:mm`。
- `cron`：用于重复任务，五段表达式：分钟、小时、月内日期、月份、星期。
- `at` 和 `cron` 必须且只能提供一个。提供 `at` 表示一次性任务，提供 `cron` 表示重复任务。
- `tz`：可选 IANA 时区或固定 UTC 偏移。省略时使用 runtime 本地时区。
- `content`：必填，传给 awareness 的任务文本。
- `chat_id`：可选 channel 上下文，例如 `tg:-1001234567890`。第一版只作为 metadata，不负责路由。真正通知谁，由任务内容和现有工具决定。

`content` 是 markdown-like 文本，不是完整 TODO checkbox 行。创建时间、下一次执行时间、完成时间等调度元数据不应写入 `content`。

## 时间语义

tick 间隔固定为 1 分钟。每次 tick 使用当前 UTC instant，并按任务自己的时区求值。

一次性任务：

- 当任务时区里的 `at <= now` 时，任务到期。
- 如果 runtime 在精确到期分钟停机，重启后任务仍会执行，因为它还留在 `cron.yaml` 里。
- agent run 没有返回 error 时，视为成功，并从 `cron.yaml` 删除该任务。
- awareness run 失败或超时后，保留该任务，后续 tick 可重试。

重复任务：

- 当 cron 表达式匹配任务时区里的当前分钟时，任务到期。
- 如果 runtime 停机期间错过了多个匹配分钟，不逐个补跑。
- 如果 runtime 启动时正处于一个匹配分钟内，允许为当前分钟执行一次。

cron 表达式支持：

- 使用标准五段 cron：`minute hour day-of-month month day-of-week`。
- 语义尽量和标准 cron 保持一致。
- 支持 `*`、逗号列表、范围、步进值，例如 `*/15`。
- 第一版不支持秒、`@daily`、`@reboot`、`L`、`W`、`#`。
- 第一版只支持数字字段，不支持 `MON`、`JAN` 这类名称。
- 星期字段接受 `0-7`，其中 `0` 和 `7` 都表示周日。
- 当 day-of-month 和 day-of-week 同时被限制时，使用标准 cron 的 OR 语义。

时区行为：

- `tz` 支持 `time.LoadLocation` 可以加载的 IANA 时区，例如 `Asia/Tokyo`。
- `tz` 支持固定 UTC 偏移，例如 `UTC+8`、`UTC-5`、`UTC+08:30`。
- 无效 `tz` 使该任务无效；该任务不得执行。
- DST 切换时，不存在的本地分钟直接跳过。
- 如果一个本地分钟出现两次，以任务 id 和 UTC minute 去重，保证每个实际 UTC minute 最多执行一次。

## Runtime 行为

cron 服务运行在已经承载 awareness 的长期 runtime 中：

- daemon
- console local runtime
- Telegram
- Slack
- Lark，当 Lark 已接入 awareness 时

每个 tick 的流程：

1. 读取并解析 `cron.yaml`。
2. 校验任务。
3. 选出到期任务。
4. 按文件顺序为每个到期任务提交一次 `behavior=cron` 的 awareness run。
5. 如果同一任务 id 已经 queued 或 running，跳过该任务。
6. 继续处理下一个到期任务。

awareness task text 必须等于任务的 `content`。

cron 任务需要保留 FIFO 语义。同一个 tick 内，后一个 cron awareness run 必须等前一个结束后再启动。实现可以复用现有队列，但不能让同一个 tick 的 cron 任务并发执行。

建议 metadata：

```json
{
  "trigger": "cron",
  "awareness": {
    "behavior": "cron",
    "source": "cron",
    "task_id": "weekly-invoice-review",
    "scheduled_at_utc": "2026-05-11T01:00:00Z",
    "schedule": "0 10 * * 1",
    "tz": "Asia/Tokyo"
  }
}
```

`behavior` 必须是 `cron`，不是 `heartbeat`。实现时应扩展现有 awareness behavior 枚举，不要复用 heartbeat。

## 队列去重

去重 key 是任务 `id`。

如果 `weekly-invoice-review` 已经 queued 或 running，而下一次 tick 又发现它到期，则跳过后一次触发。cron 不应为慢任务堆积无限 backlog。

对于一次性任务，跳过不等于删除。只有对应 awareness run 成功后，才删除任务。

第一版只做进程内去重。默认同一个 `cron.yaml` 同时只有一个 runtime 负责执行；不实现跨进程文件锁。

## 错误处理

- `cron.yaml` 不存在：视为空任务表，最多 debug 日志。
- YAML 解析失败：跳过整个 tick，并记录解析错误。
- 单个任务无效：跳过该任务，记录 `id` 和校验错误，继续处理其他有效任务。
- 队列满：跳过该任务本次 tick，并记录日志。
- awareness run 失败：保留任务。一次性任务会在后续 tick 重试。

## `todo_update` 改动

`todo_update` 保留工具名，但操作对象改为 `cron.yaml`。

当前 `todo_update` 没有 `delete` action。现有相近行为是 `complete`：它用 LLM 对 `TODO.md` 里的 WIP 项做语义匹配，匹配成功后从 `TODO.md` 移除，并追加到 `TODO.DONE.md`；无匹配或匹配歧义时返回错误。

cron 化以后移除旧的 `complete` action。删除任务使用新的 `delete` action，但匹配语义沿用现有 `complete`：当调用方没有传 `id` 时，用 LLM 在 `cron.yaml.tasks` 中按 `content` 选出唯一任务。唯一匹配才删除；无匹配或匹配歧义都返回错误。

必需 action：

- `add_once`：新增一次性任务。
- `add_recurring`：新增重复任务。
- `delete`：优先按 `id` 删除任务；缺少 `id` 时，沿用现有 `complete` 的语义匹配风格，按 `content` 匹配删除。

必需参数：

- `action`
- `content`
- `at`，用于 `add_once`
- `cron`，用于 `add_recurring`
- 可选 `tz`
- 可选 `people`，用于解析 `content` 中的引用
- 可选 `chat_id`
- 可选 `id`；新增任务时如果省略，由工具生成

引用处理保持现有行为：

- 已存在的 `[John](tg:@johnwick)` 合法。
- 传入 `people` 时，工具可以通过 contacts 解析人名，并把内容改写成稳定 ref。
- ref 语法无效时拒绝写入。

新工作流不保留 `complete` action。删除一个计划任务使用 `delete`。

`delete` 的语义匹配只允许唯一匹配。无匹配或多个候选都必须报错，要求调用方传入更精确的 `content` 或直接传 `id`。

## Prompt 改动

用 cron workflow guidance 替换 TODO workflow prompt block：

- 计划任务保存在 `cron.yaml`。
- 使用 `todo_update` 新增一次性或重复 cron 任务。
- 不再提及 `TODO.md`、`TODO.DONE.md`、`TODO.RECUR.md`。
- cron awareness task 到期时，直接处理任务内容，不向用户解释调度器内部细节。

## 实现备注

- 在现有 state path helper 附近新增 `CronFilename = "cron.yaml"` 和 `CronPath()`。
- 新增内部 cron package，负责解析、校验、due matching、原子写入。
- scheduler 逻辑和文件修改逻辑分开。
- 尽量复用 awareness task 执行路径，但增加真实的 `cron` behavior。
- 执行顺序以 `cron.yaml` 为准，不按 id 或 schedule 重新排序。
- 增加 parser 校验、due matching、时区、一次性任务删除、队列去重、`todo_update` 写入等测试。

## 需要确认

- 无。

## 实现 Checklist

- [x] 增加 `cron.enabled` 配置默认值和 `config.example.yaml` 示例。
- [x] 增加 `CronFilename = "cron.yaml"` 和 `CronPath()`。
- [x] 新增内部 cron package，支持 `cron.yaml` 读写、校验、原子写入。
- [x] 任务类型由 `at` / `cron` 互斥关系决定，不在 `cron.yaml` 中保存 `kind`。
- [x] 实现五段数字 cron 表达式解析与 due matching。
- [x] 支持 IANA 时区和 `UTC+8` 这类固定偏移时区。
- [x] 实现一次性任务到期判断、成功后删除、失败后保留。
- [x] 实现进程内 queued/running 去重。
- [x] 为 awareness 增加 `cron` behavior。
- [x] 将 cron tick 接入长期 runtime，tick 间隔为 1 分钟。
- [x] 保证同一 tick 内多个到期任务按 `cron.yaml` 顺序串行执行。
- [x] 将 `chat_id` 作为 metadata 传入，不做路由。
- [x] 改造 `todo_update`：写 `cron.yaml`，支持 `add_once`、`add_recurring`、`delete`。
- [x] 移除 `todo_update complete` 行为。
- [x] 更新 prompt：用 cron workflow 替换 TODO workflow。
- [x] console state files 展示 `cron.yaml`。
- [x] 增加 cron parser、due matching、时区、删除、工具写入、runtime 去重测试。
- [x] 运行 `go fmt ./...`。
- [x] 运行 `go test ./...`。
