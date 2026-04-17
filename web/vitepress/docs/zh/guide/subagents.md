---
title: Subagents
description: "分而治之"
---

# Subagents

## 典型场景

Subagent 适合这几类事情：

- 一条 shell 命令很慢、输出很多，想把它和父 Loop 隔开。
- 一段工作还是多步的，但依然想要用内置工具和配套的体系。
- 希望任务只返回一个结果，而不是把中间噪声都回灌给父 Loop。
- 子任务应该跑在外部 ACP agent 里，而不是另一个本地 Mister Morph loop 里。

## Overview

Mister Morph 现在有三条隔离任务入口：

| 入口 | 会不会使用 LLM | 更适合什么 | 返回什么 |
|---|---|---|---|
| `spawn` | 会 | 内部 subagent 还要自己调用工具和做推理 | JSON |
| `acp_spawn` | 不会起本地内层 Mister Morph loop；会启动外部 ACP session | 外部 ACP agent 或 adapter | JSON |
| `bash.run_in_subtask=true` | 不会 | 单条 shell 命令，想隔离执行和输出 | JSON |

共同点：

- 三条路径现在都是同步阻塞，父 Loop 会等内部执行结束。
- 三条路径返回同一种 JSON envelope。
- 都不会把内部执行的原始 transcript 发给父 Loop。

ACP 补充说明：

- `acp_spawn` 也是一条内层 agent 边界，只是这个边界由外部 ACP agent 进程处理，不是本地 Mister Morph engine。

Subagent 解决隔离、收敛和结果回传的问题，不是完整的后台任务系统

## `spawn` 工具

`spawn` 是 engine 级工具。

参数：

- `task`：必填，给内部 agent 的提示词。
- `tools`：必填，非空工具名数组。
- `model`：可选，内部 agent 的模型覆盖。
- `output_schema`：可选，结构化输出标识。
- `observe_profile`：可选，观察提示。目前支持 `default`、`long_shell`、`web_extract`。

当前行为：

- 未知工具名或父 registry 里不存在的工具名会被忽略。
- 如果没有可用工具，调用会失败。
- `tools` 会忽略 `spawn`。

## `acp_spawn` 工具

`acp_spawn` 也是 engine 级工具。

参数：

- `agent`：必填，`acp.agents` 里的 profile 名
- `task`：必填，交给外部 ACP agent 的任务
- `cwd`：可选，工作目录覆盖
- `output_schema`：可选，结构化输出标识
- `observe_profile`：可选，观察提示

当前行为：

- 一次调用创建一个 ACP session
- 当前只支持 `stdio`
- 子任务过程中会处理 ACP 的 permission、文件和终端回调
- 最终结果会归一化成和 `spawn` 相同的 `SubtaskResult` envelope

profile 配置和传输细节见 [ACP](/zh/guide/acp)。

## `bash.run_in_subtask=true`

更轻的一条隔离执行路径

- 不会内置调用 LLM。
- 内部 `output_schema` 固定为 `subtask.bash.result.v1`。
- 内部 `observe_profile` 固定为 `long_shell`。

## 限制

当前 subagent 深度限制是 `1`。即根任务最多进入一层隔离执行。已经在这一层里的 run 不能再继续进入下一层。

## 输出

### `output_schema`

`output_schema` 只是一个约定名，不是内建的 JSON Schema 注册中心。

给 `spawn` 传了它以后：

- 内部 agent 会被提示产出 JSON 最终输出；
- 运行时会要求最终输出是 JSON，或者至少是可解析 JSON 的字符串；

Mistermorph 现在不会按真实 schema 校验对象字段。

### 返回的 JSON Envelope

```json
{
  "task_id": "sub_123",
  "status": "done",
  "summary": "subtask completed",
  "output_kind": "text",
  "output_schema": "",
  "output": "child result",
  "error": ""
}
```

其中，

- `status`：现在是 `done` 或 `failed`。
- `summary`：这次 subagent 执行的摘要。
- `output_kind`：`text` 或 `json`。
- `output_schema`：纯文本输出时为空；结构化输出时回显传入的标识。
- `output`：结果本体，可能是文本或者 JSON
- `error`：只在失败时有内容。

对 `bash.run_in_subtask=true` 来说，`output` 会是结构化 JSON，里面包含 `exit_code`、截断标记、`stdout`、`stderr`。

## 测试 Prompt

**Prompt 1** - `spawn + bash`，只返回一行：

```text
必须调用 spawn tool，不要直接回答。只允许内部 agent 使用 bash。
让它执行 `printf 'alpha\nbeta\ngamma\n' | sed -n '2p'`。最后只返回第二行。
```

预期结果：`beta`

**Prompt 2** - `spawn + bash`，返回结构化 JSON：

```text
必须调用 spawn tool，并把 output_schema 设为 `subagent.demo.echo.v1`。
只允许内部 agent 使用 bash。让它执行 `echo '{"ok":true,"value":42}'`。最终只返回结构化 JSON，不要解释。
```

预期结果：

```json
{"ok":true,"value":42}
```

**Prompt 3** - `bash.run_in_subtask=true`：

```text
请调用 bash tool，并把 `run_in_subtask` 设为 true。执行 `printf 'one\ntwo\nthree\n' | tail -n 1`。不要解释，只返回最后一行。
```

预期结果：`three`

**Prompt 4** - 更长一点的隔离 shell 执行：

```text
请调用 bash tool，并把 `run_in_subtask` 设为 true。执行 `sleep 1; echo SUBAGENT_BASH_OK`。最后只回复 stdout。
```

预期结果：`SUBAGENT_BASH_OK`

## 配置

- `tools.spawn.enabled` 只控制显式 `spawn` 工具入口。
- `tools.acp_spawn.enabled` 只控制显式 `acp_spawn` 工具入口。
- ACP profile 配在 `acp.agents`。
- 即使 `tools.spawn.enabled=false`，`bash.run_in_subtask=true` 这种 direct path 仍然可以工作。

## Integration 开发

- `integration.Config.BuiltinToolNames` 可以包含 `spawn` 和 `acp_spawn`，也可以不包含。
- 如果你直接用 `agent.New(...)` 组 engine，`spawn` 默认开启，`acp_spawn` 默认关闭；可用 `agent.WithSpawnToolEnabled(...)`、`agent.WithACPSpawnToolEnabled(...)`、`agent.WithACPAgents(...)` 覆盖。

示例：

```go
cfg := integration.DefaultConfig()
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "spawn", "acp_spawn"}
cfg.Set("tools.spawn.enabled", true)
cfg.Set("tools.acp_spawn.enabled", true)
```

另见 [ACP](/zh/guide/acp)。
