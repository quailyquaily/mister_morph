---
date: 2026-06-16
title: Coder Tool 直接调用本机 Coding CLI
status: implemented
---

# Coder Tool 直接调用本机 Coding CLI

## 1) 背景

当前 MisterMorph 调用 Codex / Claude Code 的外部子任务路径主要是：

```text
MisterMorph acp_spawn
  <-> ACP adapter
  <-> codex app-server / claude -p
```

这个形状能跑，但对 Codex / Claude Code 来说多了一层协议适配。

事实更简单：

- Codex CLI 已有非交互入口：`codex exec`。
- Claude Code 已有非交互入口：`claude -p`。
- 我们要的是把一个自包含 coding 子任务交给本机 coding agent。
- 这不是一定要用 ACP 才能表达的事情。

所以这期新增一个更直接的 tool：`coder`。

## 2) 目标

新增 `coder` tool，用本机 CLI 直接运行 Codex 或 Claude Code。

一期目标：

- 支持 `codex` 和 `claude` 两个后端。
- 默认都走非交互 CLI。
- 默认直接 bypass approval / permission，不再让子 agent 询问父 agent。
- 复用现有 `SubtaskRunner` direct path。
- 返回现有 `SubtaskResult` envelope。
- 继续发出 `subtask_start` / `subtask_done` 事件。
- 使用 CLI 的流式 JSON 输出，提供实时 Console 观察。

这期不再通过 ACP adapter 调 Codex / Claude。

ACP 仍保留给真正需要 ACP 协议的外部 agent。

## 3) 第一性原理

这里最重要的问题不是“怎么保留 wrapper”，而是“本机 coding agent 已经暴露了什么稳定入口”。

Codex 和 Claude Code 都已经有程序化 CLI：

- Codex：`codex exec`
- Claude Code：`claude -p`

因此最小系统应该是：

```text
MisterMorph coder tool
  -> start child process
  -> parse streaming JSON stdout
  -> convert final output to SubtaskResult
```

不需要：

- 把 CLI 再包成 ACP server。
- 为两个 CLI 做一套假的统一 session 协议。
- 把模型、工具、权限模式等 CLI 自带配置提前复制到 tool 参数里。

## 4) Tool API

Tool name：`coder`

参数只保留最小集合：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `coder` | `string` | 是 | `codex` 或 `claude` |
| `task` | `string` | 是 | 交给 coding agent 的任务文本 |
| `cwd` | `string` | 否 | 子进程工作目录。支持 `workspace_dir`、`file_cache_dir`、`file_state_dir`；留空表示当前 workspace |

示例：

```json
{
  "coder": "codex",
  "task": "Run tests, fix the failing issue, and report the changed files.",
  "cwd": "."
}
```

```json
{
  "coder": "claude",
  "task": "Review the current git diff and list correctness issues only."
}
```

## 5) 默认 CLI 行为

### 5.1 Codex

默认命令：

```bash
codex exec \
  --dangerously-bypass-approvals-and-sandbox \
  --json \
  -C <cwd> \
  -
```

`task` 通过 stdin 写入。

原因：

- 避免长 prompt 进入 argv。
- 不需要 shell 转义。
- `--json` 输出 JSONL 事件，能实时观察进度。
- bypass 参数符合本期默认“不 ask，直接执行”的目标。

### 5.2 Claude Code

默认命令：

```bash
claude \
  -p <task> \
  --output-format stream-json \
  --verbose \
  --include-partial-messages \
  --no-session-persistence \
  --dangerously-skip-permissions
```

原因：

- `-p` 是 Claude Code 的非交互入口。
- `stream-json` 输出 JSONL 事件，能实时观察进度。
- `--include-partial-messages` 能输出更及时的文本增量。
- `--no-session-persistence` 让一次 tool call 对应一次独立子任务。
- bypass 参数符合本期默认“不 ask，直接执行”的目标。

## 6) 返回值

`coder` 返回现有 `SubtaskResult` JSON envelope。

成功时：

```json
{
  "task_id": "sub_xxx",
  "status": "done",
  "summary": "subtask completed",
  "output_kind": "text",
  "output_schema": "",
  "output": "...",
  "error": ""
}
```

失败时：

```json
{
  "task_id": "sub_xxx",
  "status": "failed",
  "summary": "subtask failed",
  "output_kind": "text",
  "output_schema": "",
  "output": "...",
  "error": "..."
}
```

输出规则：

- stdout 按 JSONL 解析。
- 只解析最小公共信息：文本增量、最终文本、失败信息。
- 文本增量转成 `tool_output` event，供 Console 观察。
- 最终 output 优先取 CLI 明确给出的最终文本。
- 如果无法识别最终文本，fallback 到已收集文本。
- stderr 只保留尾部用于错误摘要，避免巨大输出撑爆上下文。
- 子进程非零退出码视为失败。

## 7) 子任务语义

`coder` 必须走 `SubtaskRunner` direct path，而不是普通 shell tool。

原因：

- 复用 `subtask_start` / `subtask_done` 事件。
- 复用 subtask depth limit。
- 返回 envelope 和 `spawn` / `acp_spawn` 一致。
- 父 agent 看到的是一个完成的子任务，而不是一段裸命令输出。

执行形状：

```text
coder.Execute
  -> runner.RunSubtask(ctx, SubtaskRequest{RunFunc: ...})
  -> RunFunc starts codex/claude child process
  -> parse stream events
  -> return SubtaskResult
```

父 context 取消时，必须终止子进程。

不新增 `timeout` 参数。已有 engine tool timeout 和上层 task timeout 仍然通过 context 生效。

## 8) 配置

新增一个显式开关：

```yaml
tools:
  coder:
    enabled: false
    path_extra: []
```

原因：

- 本 tool 默认使用 bypass 参数。
- 这等价于允许本机 coding CLI 在当前工作区直接读写和执行命令。
- 不能在默认配置里静默暴露。

本期新增 `path_extra`，用于把 Codex / Claude Code CLI 所在目录追加到 coder 子进程的 PATH 前面。它只影响 `coder` tool，不影响 `bash`。

本期不新增这些配置：

- codex command path
- claude command path
- default model
- allowed tools
- permission mode
- max turns
- timeout

如果后续真的需要自定义 command，再从实际需求加配置。第一版仍调用 PATH 上的 `codex` 和 `claude`，但 PATH 可以通过 `tools.coder.path_extra` 扩展。

## 9) 安全边界

`coder` 不是沙箱。

它默认传 bypass 参数：

- Codex：`--dangerously-bypass-approvals-and-sandbox`
- Claude：`--dangerously-skip-permissions`

所以它的安全模型等同于用户在同一个工作区手动运行这些命令。

MisterMorph 这一层只负责：

- 限定启动目录。
- 控制子进程生命周期。
- 收集 stdout / stderr。
- 把输出转成事件和最终 envelope。

MisterMorph 这一层不负责：

- 拦截 Codex / Claude 内部每一次读写文件。
- 拦截 Codex / Claude 内部每一次 shell 执行。
- 把 ACP roots 套到 CLI 内部行为上。

因此 `coder` 必须保持 opt-in：`tools.coder.enabled=true` 可以默认暴露它，用户显式写 `$coder` 时也可以只对当前任务暴露它。`$coder` 只暴露 schema，不会直接执行 tool。

## 10) 非目标

这期不做：

- 不通过 ACP adapter 调 Codex / Claude。
- 不删除 `acp_spawn`。
- 不做 `coder` session persistence。
- 不做 `coder` resume。
- 不做交互式 approval。
- 不暴露 model 参数。
- 不暴露 allowed tools 参数。
- 不暴露 permission mode 参数。
- 不暴露 timeout / max turns 参数。
- 不做图片输入。
- 不做结构化 output schema。
- 不做 MCP passthrough。
- 不做 WebSocket / HTTP transport。

## 11) 实现清单

- [x] 新增 `tools.coder.enabled` 默认值，默认 false。
- [x] 在 config example 里补 `tools.coder.enabled`。
- [x] 新增 `tools.coder.path_extra`，用于查找不在服务 PATH 中的 `codex` / `claude`。
- [x] 在 runtime snapshot / channel deps 里传递 coder tool 开关。
- [x] 在 builtin tool name 里加入 `coder`。
- [x] 新增 `agent/coder_tool.go`。
- [x] 新增 coder CLI runner，使用 `exec.CommandContext`。
- [x] Codex runner：
  - [x] 调用 `codex exec --dangerously-bypass-approvals-and-sandbox --json -C <cwd> -`。
  - [x] 通过 stdin 写入 task。
  - [x] 按 JSONL 读取 stdout。
  - [x] best-effort 提取文本增量和最终文本。
- [x] Claude runner：
  - [x] 调用 `claude -p <task> --output-format stream-json --verbose --include-partial-messages --no-session-persistence --dangerously-skip-permissions`。
  - [x] 按 JSONL 读取 stdout。
  - [x] 提取 stream delta、assistant text 和 result。
- [x] stderr 使用 tail buffer。
- [x] 父 context 取消时终止子进程。
- [x] 返回 `SubtaskResult` envelope。
- [x] 把文本增量转成 `tool_output` event。
- [x] 更新文档，把 Codex / Claude Code 推荐入口从 ACP adapter 改为 `coder`。
- [x] 保留 ACP 文档，但注明 Codex / Claude adapter 是 legacy / optional。

## 12) 测试清单

- [x] `coder` 参数校验：
  - [x] 缺 `coder` 报错。
  - [x] 缺 `task` 报错。
  - [x] 非 `codex` / `claude` 报错。
- [x] `coder` 走 `SubtaskRunner` direct path。
- [x] subtask depth limit 生效。
- [x] Codex runner 参数包含 bypass、json、cwd、stdin prompt。
- [x] Claude runner 参数包含 bypass、stream-json、no-session-persistence。
- [x] coder runner 会把 `tools.coder.path_extra` 加到命令查找和子进程 PATH。
- [x] Codex JSONL 文本能转成 output。
- [x] Claude stream-json 文本能转成 output。
- [x] 文本增量会发出观察事件。
- [x] 非零 exit code 返回 failed envelope。
- [x] stderr tail 出现在错误摘要里。
- [x] context cancel 会终止子进程。
- [x] `tools.coder.enabled=false` 且没有显式 `$coder` 时不注册 tool。
- [x] `tools.coder.enabled=false` 且显式 `$coder` 时注册 tool。
- [x] `tools.coder.enabled=true` 时注册 tool。

## 13) 迁移策略

短期：

- 保留 `acp_spawn`。
- 新增 `coder`。
- 文档推荐 Codex / Claude Code 优先使用 `coder`。

中期：

- ACP 文档保留给真正 ACP agent。
- Codex / Claude ACP adapter 标为 legacy / optional。

不做自动迁移。

已有用户如果继续用 `acp_spawn`，行为不变。
