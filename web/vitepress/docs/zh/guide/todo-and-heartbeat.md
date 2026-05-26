---
title: 待办事项与 Heartbeat
description: cron.yaml 和 HEARTBEAT.md 如何让 agent 在当前对话之外继续跟踪工作。
---

# 待办事项与 Heartbeat

MisterMorph 提供两种 Awareness 机制：Heartbeat 和待办事项：Heartbeat 是一种简单的固定唤醒行为；待办事项可以更加精确地设定需要定时执行或者反复执行的工作。

## 待办事项

一次性或定时重复的工作更适合写入代办事项。

你可以直接编辑 `cron.yaml`，或者在 Console GUI 界面编辑，或者让 Agent 需要新增或删除待办事项时，使用 `todo_update` 工具编辑。

每个待办事项包含标题、时间规则、时区和内容。内容是实际交给 agent 处理的文本，可以包含联系人引用，例如 `[John](tg:@john)`。

### 到期执行

Agent 每分钟检查一次 `cron.yaml`。到期的待办事项会以 `behavior=cron` 的 awareness task 运行。

其中：

- 一次性待办事项在成功执行后从被删除。若执行失败或超时时会保留，后续会重试。
- 重复待办事项会一直留在列表中。

> 备注：
>
> 1. 如果 Agent 错过了多个触发时间，恢复后不会逐个待办事项补跑。
> 2. 若同一个待办事项已经在队列中或正在执行，本次触发会被跳过。

### 配置

待办事项保存在 `file_state_dir/cron.yaml`。

`cron.yaml` 的基本结构如下：

```yaml
version: 1
tasks:
  - id: submit-report
    title: Submit report
    at: "2026-05-12 09:00"
    tz: "Asia/Tokyo"
    content: "Remind [John](tg:@john) to submit the report."

  - id: weekly-invoice-review
    title: Invoice review
    cron: "0 10 * * 1"
    tz: "UTC+8"
    content: "Review open invoices."
```

字段说明：

- `id`：稳定标识，用于去重、更新和删除。
- `title`：待办事项标题。
- `at`：一次性待办事项的执行时间，格式为 `YYYY-MM-DD HH:mm`。
- `cron`：重复待办事项的五段 cron 表达式。
- `tz`：IANA 时区或固定 UTC 偏移，例如 `Asia/Tokyo`、`UTC+8`。
- `content`：到期后交给 agent 的任务内容。
- `chat_id`：可选的通道上下文，只作为 metadata，不负责路由。

`at` 和 `cron` 必须且只能提供一个。提供 `at` 表示只执行一次；提供 `cron` 表示按规则重复执行。

## Heartbeat

Heartbeat 通过 cron 服务运行。只有 `cron.enabled`、`heartbeat.enabled` 都开启，且 `heartbeat.interval > 0` 时，系统才会注册内置 cron task `__heartbeat__`。

每次 heartbeat tick 时：

1. Agent 会读取 `HEARTBEAT.md`。
2. 如果 `HEARTBEAT.md` 不是空的，它的内容会成为 heartbeat task。
3. Agent 处理这次任务。

如果 `HEARTBEAT.md` 为空，就不会启动 agent task。

如果 `cron.enabled` 为 false，Heartbeat 不运行。Heartbeat 不读取用户待办事项；用户待办事项由同一个 cron 服务读取 `cron.yaml` 后触发。

### HEARTBEAT.md

`HEARTBEAT.md` 是每次 heartbeat 的固定说明。它描述 Agent 每次 Heartbeat 时要检查什么。

适合放在这里的内容：

- 查找到期的后续跟进。
- 检查例行文件。


更新待办事项的工具见 [`todo_update`](/zh/guide/built-in-tools#todo_update)。状态目录的位置见 [文件系统根目录](/zh/guide/filesystem-roots)。
