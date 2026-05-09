---
title: 待办事项与 Heartbeat
description: TODO 文件和 HEARTBEAT.md 如何让 agent 在当前对话之外继续跟踪工作。
---

# 待办事项与 Heartbeat

Heartbeat 是 awareness runtime 的定时行为。每次 heartbeat 都会从 `HEARTBEAT.md` 创建一条新的 runtime task，不包含聊天历史。

`POST /poke` 使用同一个 awareness runtime 防重入状态，但它是另一种行为。Poke 不会读取 `HEARTBEAT.md`；请求 body 会成为 task 内容。空 body 会直接返回 `400 Bad Request`。

## Heartbeat 流程

每次 heartbeat tick 时：

1. Runtime 读取 `HEARTBEAT.md`。
2. 如果 `HEARTBEAT.md` 不是空的，它的内容会成为 heartbeat task。
3. 如果 heartbeat task 有内容，agent 用普通工具处理这次任务。

如果 `HEARTBEAT.md` 为空，就不会启动 agent task。

Heartbeat 不再读取 `TODO.md`，也不再展开 `TODO.RECUR.md`。

## Poke 流程

已认证的 `POST /poke` 请求会这样处理：

1. Server 读取请求 body 的一小段文本预览。
2. 如果 body 为空，或者不能作为文本使用，server 返回 `400 Bad Request`。
3. Poke body 成为 task 内容。
4. Task 不带聊天历史，也不带 `HEARTBEAT.md`。

## HEARTBEAT.md

`HEARTBEAT.md` 是每次 heartbeat 的固定说明。它应该描述 agent 要检查什么，而不是保存某个一次性用户请求。

适合放在这里的内容：

- 查找到期的后续跟进。
- 检查例行文件。
- 根据 heartbeat 明确读取的文件或服务状态发送提醒。

不要把一次性任务直接写进 `HEARTBEAT.md`。一次性任务放进 `TODO.md`；重复任务放进 `TODO.RECUR.md`。

## TODO 流程

TODO 文件保存具体待办。`todo_update` 工具会在包含 TODO workflow 的普通 agent task 中写入和完成 TODO 记录。Heartbeat 和 poke task 不会自动包含 TODO workflow。

TODO 文件有三个。

### TODO.md

`TODO.md` 保存只需要执行一次的工作：

```text
- [ ] [Created](2026-05-01 12:41), [ChatID](tg:-100123) | Remind [John](tg:@john) to submit report.
```

一次性提醒和一次性任务放在 `TODO.md`。

### TODO.DONE.md

`TODO.DONE.md` 保存已经完成的一次性待办。`todo_update` 完成 `TODO.md` 里的项目时，会把记录移动到这里。

循环待办不会移动到 `TODO.DONE.md`。

### TODO.RECUR.md

`TODO.RECUR.md` 保存重复规则：

```text
- [ ] [Next](2026-05-07 15:00), [Repeat](weekly), [TZ](Asia/Tokyo) | Play tennis.
- [ ] [Next](2026-05-02 09:00), [Repeat](every 6 hours) | Check the report queue.
```

当前支持的 `Repeat` 值：

- `daily`
- `weekly`
- `every N days`
- `every N hours`

`TZ` 可选。省略时使用 runtime 的本地时区。

循环记录会留在 `TODO.RECUR.md`。Heartbeat 目前不会把到期循环记录复制到 `TODO.md`。

## 该写到哪里

| 需求 | 文件 |
|---|---|
| 告诉 agent 每次 heartbeat 要检查什么 | `HEARTBEAT.md` |
| 只做一次 | `TODO.md` |
| 保存已完成的一次性待办 | `TODO.DONE.md` |
| 重复执行 | `TODO.RECUR.md` |

更新 TODO 文件的工具见 [`todo_update`](/zh/guide/built-in-tools#todo_update)。状态目录的位置见 [文件系统根目录](/zh/guide/filesystem-roots)。
