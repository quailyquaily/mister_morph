---
date: 2026-05-26
title: Awareness 调度与本地脚本说明清理
status: implemented
---

# Awareness 调度与本地脚本说明清理

## 1) 背景

当前有两个遗留设计需要清理：

- `SCRIPTS.md` 用来给模型提供本地脚本说明。
- `HEARTBEAT.md` 用来给 heartbeat awareness 提供固定检查内容。

这两个文件都来自早期设计。现在系统已有更明确的能力边界：

- 本地可复用能力应放到 Skill 里，而不是写到一个全局脚本说明文件。
- cron 和 heartbeat 底层都运行 awareness task，不需要两套调度机制。

## 2) 目标

### 2.1 移除 `SCRIPTS.md`

`SCRIPTS.md` 和相关支持可以完全移除。

原因：

- 它只是一个全局 prompt 附加文件，没有明确触发规则。
- 它会让模型维护额外约定，容易和真实文件状态脱节。
- 如果用户需要本地脚本能力，创建 Skill 更合适。Skill 有独立说明、触发条件、脚本目录和复用边界。

需要移除：

- `assets/config/SCRIPTS.md`
- install/setup 对 `SCRIPTS.md` 的默认创建
- Console setup 对 `SCRIPTS.md` 的自动补全
- State Files 里的固定 `SCRIPTS.md` 入口
- `AppendLocalToolNotesBlock` 及其 prompt block
- 所有运行时对 `SCRIPTS.md` 的读取
- 相关测试和文档引用

迁移语义：

- 已存在的用户 `SCRIPTS.md` 不再被读取。
- 不提供自动迁移。
- 文档应建议用户把有用的脚本说明迁到 Skill。

### 2.2 保留 `HEARTBEAT.md`，移除独立 heartbeat 调度

`HEARTBEAT.md` 文件可以保留，但它只作为可选 checklist 输入。

独立 heartbeat scheduler 应移除。heartbeat 周期任务应挂接到 cron runner 上，作为一个内置系统任务运行。

原因：

- heartbeat 和 cron 最终都运行 awareness task。
- 两套调度机制会产生重复的状态、日志和跳过逻辑。
- 用户可见的周期任务模型应尽量统一。

## 3) 新模型

cron runner 负责两类任务：

1. 用户任务：来自 `file_state_dir/cron.yaml`。
2. 系统任务：内置 heartbeat task，不写入 `cron.yaml`。

内置 heartbeat task 的输入来自：

- `heartbeat.enabled`
- `heartbeat.interval`
- `HEARTBEAT.md`

行为：

- `cron.enabled: false` 时不运行内置 heartbeat task。
- `heartbeat.enabled: false` 时不注册内置 heartbeat task。
- `heartbeat.interval <= 0` 时不注册内置 heartbeat task。
- `HEARTBEAT.md` 不存在或为空时，触发时跳过，不报错。
- `HEARTBEAT.md` 有有效内容时，其内容作为 awareness task 运行。

内置 task 不应写入 `cron.yaml`。

内置 task ID 固定为：

- `__heartbeat__`

原因：

- 避免污染用户配置文件。
- 避免用户在 Console/Todo/Cron UI 中误删系统任务。
- 避免把系统实现细节暴露成普通用户任务。

## 4) 调度要求

现有 `heartbeat.interval` 是 Go duration，不是五字段 cron 表达式。实现时应把它转换成现有 cron runner 支持的五字段 cron 表达式，不新增 interval runner 概念。

可转换规则：

- 小于 1 小时：分钟数必须整除 60。
  - `1m` -> `* * * * *`
  - `5m` -> `*/5 * * * *`
  - `10m` -> `*/10 * * * *`
  - `15m` -> `*/15 * * * *`
  - `20m` -> `*/20 * * * *`
  - `30m` -> `*/30 * * * *`
- 等于或大于 1 小时：必须是整小时，且小时数必须整除 24。
  - `1h` -> `0 * * * *`
  - `2h` -> `0 */2 * * *`
  - `3h` -> `0 */3 * * *`
  - `4h` -> `0 */4 * * *`
  - `6h` -> `0 */6 * * *`
  - `8h` -> `0 */8 * * *`
  - `12h` -> `0 */12 * * *`
  - `24h` -> `0 0 * * *`

无法转换的值：

- 例如 `45m`、`90m`、`2h30m`
- fallback 到配置系统设置的默认值。
- 如果默认值仍然无法转换，记录 warn，不注册内置 heartbeat task。

推荐实现：

- 在注册内置 heartbeat task 前，把 `heartbeat.interval` 规范化为五字段 cron。
- 内置 heartbeat task 和普通 cron task 共用 queue、in-flight、worker、日志和 awareness 执行路径。
- metadata 中保留原始 interval 和实际使用的 cron schedule。

## 5) Poke 要求

poke 不是周期任务，不应塞进 cron。

保留 poke 的请求入口，但它应只负责接收即时 wake signal，然后进入 awareness 执行路径。

如果当前 scheduler 同时负责 heartbeat 和 poke，重构后应拆开：

- heartbeat 周期触发交给 cron runner 的内置 task。
- poke 保持即时触发入口。

## 6) Console / Telegram / Slack 一致性

Console、Telegram、Slack 应使用同一套 awareness 调度语义：

- 用户 cron task 走 cron runner。
- 内置 heartbeat task 走 cron runner。
- poke 走即时入口。

各 runtime 仍可保留自己的通知策略。

例如：

- Slack 可以继续把 awareness notifier 接到 Slack。
- Telegram 可以继续只记录 heartbeat alert，不主动推送。
- Console 和 Telegram/Slack 一样直接运行 awareness task，不再把 awareness 运行写入 Console task store。

## 7) 安装与 Setup

安装和 setup 继续创建：

- `HEARTBEAT.md`
- `cron.yaml`

安装和 setup 不再创建：

- `SCRIPTS.md`

其中：

- `SCRIPTS.md` 完全移除。
- `HEARTBEAT.md` 仍可被运行时读取，并保留默认模板。
- `cron.yaml` 继续作为用户 cron task 文件创建。

## 8) State Files

State Files 不再固定展示：

- `SCRIPTS.md`

State Files 继续允许编辑：

- `HEARTBEAT.md`

后续如果 Console UI 要改 awareness/heartbeat 的编辑入口，另开需求处理。

## 9) 验收标准

- `SCRIPTS.md` 文件从模板、安装流程、setup 流程、State Files、prompt block 中消失。
- 运行 agent 时不会读取 `SCRIPTS.md`。
- `HEARTBEAT.md` 缺失时，heartbeat 不报错。
- `HEARTBEAT.md` 有内容且 heartbeat 开启时，会按 `heartbeat.interval` 周期运行 awareness task。
- `cron.enabled: false` 时，heartbeat 不运行。
- `heartbeat.interval` 会转换成五字段 cron；无法转换时 fallback 到配置默认值。
- heartbeat 周期任务和 `cron.yaml` 任务共用 cron runner 的排队、in-flight 和 worker 逻辑。
- heartbeat 内置 task 不写入 `cron.yaml`。
- heartbeat 内置 task 使用固定 ID `__heartbeat__`。
- poke 仍可即时触发 awareness task。
- Console、Telegram、Slack 的 awareness 行为保持可用。

## 10) 非目标

- 不迁移已有用户的 `SCRIPTS.md`。
- 不改变 Skill 格式。
- 不改变 `cron.yaml` 的用户文件格式。
- 不要求删除 `HEARTBEAT.md` 兼容读取。
