---
title: Prompt 组织
description: 介绍 Agent 的 Prompt 机制
---

# Prompt 组织

在 Mister Morph 主 Loop 中，Prompt 的唯一目的是为 Agent 拼装出合理的状态。

> 我们人为切分的 skill 也好 identity 也好 soul 也好 todo 也好 memory 也好，本质上都是在维护这个状态，属于 memory 的语法糖。

在 Mister Morph 里，这些语法糖由 `agent/prompts/system.md` 作为骨架组装。

## 主循环

### 静态 Prompt 骨架

这个模板大致长这样：

```md
## Persona
{{ identity }}

## Available Skills
{{ skills }}

## Reference Format
{{ 约定的内部引用格式 }}

## Additional Policies
{{ 附加的大段策略 }}

## Response Format
{{ 约定的输出格式 }}

## Rules
{{ 内置规则 和 附加规则 }}
```

### 运行时 Prompt

进入某一次具体运行后，runtime 会把当前上下文继续补进去。

常见来源包括：

- 本地 persona 文件，`IDENTITY.md` 和 `SOUL.md`
- 当前启用的 skill 元数据
- 本地脚本说明，例如 `SCRIPTS.md`
- 附加的策略块
- memory summary

会随着当前任务、当前通道和当前本地状态变化。在 CLI、Telegram、Slack 里跑出来的最终 prompt，不一定完全一样。

### Prompt 以外的消息编排

准备好最终 system prompt 之后，主 Agent 还会在请求里编排消息栈。

运行时 metadata 会作为一个 user 角色的 JSON envelope 注入，外层 key 是 `mister_morph_meta`。常见字段包括已有的 trigger / correlation 信息、运行时钟字段 `now_utc` / `now_local` / `now_local_weekday`，以及 `host_os` 这类宿主环境事实。

顺序可以理解成：

```text
[system] 最终 system prompt
   ->
[user] 运行时 metadata
   ->
[history] 历史消息
   ->
[user] 当前消息或原始 task
```

## 独立 Prompt

Mister Morph 里还有一类调用不会先拼出主 Agent 的完整 system prompt，也不一定会进入带工具的多步 Loop，而是直接构造一组更小的 prompt，单次调用 `llm.Chat`。

这类独立 Prompt 会用于：

- 确定如何介入群聊
- 任务规划
- Memory 整理
- 一些语义判断或语义匹配类的小任务

### 和主 Loop 的关系

可以简单理解成两条路：

```text
主 Agent 主 Loop
  -> 完整 system prompt
  -> runtime metadata / history / current message
  -> 可多步、可调工具

独立 llm.Chat
  -> 专用 system prompt / user prompt
  -> 单次调用
  -> 只解决一个很窄的问题
```

例如，Agent 在判断如何介入群聊时，会检查：

- 这条消息是不是在叫我
- 现在要不要插话
- 需不需要直接轻量回应（发表情）

这个判断发生在主 Loop 之前，所以它更适合走一个更小、更专用的 Prompt，而不是用整套主 system prompt 去判断。
