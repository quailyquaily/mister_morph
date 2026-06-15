---
title: 记忆（Memory）
description: 介绍基于 unified journal 的 memory 架构、注入和回写规则。
---

# Memory

Mister Morph 的 memory 系统先把已接受的事件写入统一 journal，再异步投影。

## Journal

说白了，就是已接受的 memory 事件会按发生顺序写成 JSONL。统一 journal 是事实源。

journal 位于 `<file_state_dir>/journal/`。

当前 segment 达到配置大小后，下一次 append 会写入新的稳定 segment，例如 `events.000000000000000002.jsonl`。

## 投影

Mister Morph 会根据 journal 事件进行简单投影，投影的对象有两个：

- `memory/index.md`（长期记忆）
- `memory/YYYY-MM-DD/*.md`（短期记忆）
  - 短期记忆的文件会根据所在的 Channel 进行隔离。例如，Telegram 下不同群聊的记忆是不互通的。

所谓投影，就是读取 journal 事件，使用 LLM 进行总结归纳，写入到对应的对象文件中。

投影的记录点文件在 `memory/projection_checkpoint.json`，内容大概是：

```json
{
  "file": "events.000000000000000001.jsonl", // 当前处理的 journal segment
  "line": 18,                                // 当前处理行
  "byte": 4096,                              // 当前处理位置之后的 byte cursor
  "updated_at": "2026-02-28T06:30:12Z"   // 更新时间
}
```

## 注入

当下面的配置开启时，一部分记忆的投影会被注入到 prompt 中：

- `memory.enabled = true`
- `memory.injection.enabled = true`

配置 `memory.injection.max_items` 控制注入条目数上限。

## 备注

1. Memory 投影损坏可由 journal 重建，删除 `memory/projection_checkpoint.json` 然后让 Agent 持续运行即可。
2. 生产环境把 `file_state_dir` 放到持久化存储来维持运行状态（包括记忆）。
