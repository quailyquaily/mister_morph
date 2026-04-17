---
date: 2026-04-10
title: ACP 外部 Agent 支持设计
status: draft
---

# ACP 外部 Agent 支持设计

## 1) 目标

这期要解决的问题很直接：

- 让 MisterMorph 能把一个子任务委托给外部 ACP Agent。
- 目标对象包括 Claude Code、Codex，以及别的 ACP Agent。
- 复用现有子任务 envelope、事件和 Console 预览链路。
- 让外部 Agent 在本地工作区里执行时，继续受本地路径、写入和命令执行约束。

一期范围收敛为：

- MisterMorph 只实现 ACP client，不实现 ACP agent/server。
- 只做 `stdio` 传输。
- 只做同步、一次性子任务。
- 每次调用新建一个 ACP session，任务结束后关闭。
- 只发送文本 prompt，不做图片和多模态桥接。
- 支持 `authenticate`。
- 对 `session/new` 返回的已声明 config option，支持 `session/set_config_option`。
- 支持外部 Agent 常见必需能力：
  - `session/request_permission`
  - `fs/read_text_file`
  - `fs/write_text_file`
- 支持最小 `terminal/*`：
  - `terminal/create`
  - `terminal/output`
  - `terminal/wait_for_exit`
  - `terminal/kill`
  - `terminal/release`
- 不实现 MCP 透传。

## 2) 非目标

这期先不做下面这些事：

- 不把 ACP 塞进 `llm.Client` / provider 层。
- 不把 MisterMorph 暴露成一个 ACP agent。
- 不做 `session/load`、会话恢复、会话列表。
- 不做交互式权限弹窗。
- 不做 ACP session mode / config option 的完整抽象。
- 不把现有本地 tools 全部再包成 ACP tool。
- 不做自动路由，让主 agent 自己把普通任务默认委托给 ACP。
- 不做 MCP 透传。
- 不做 HTTP / SSE ACP transport。

## 3) 协议事实

ACP 本身的关键点很明确：

- ACP 基于 JSON-RPC 2.0。
- 基本顺序是：
  - `initialize`
  - 某些 agent 会先要求 `authenticate`
  - `session/new`
  - 某些 agent 会通过 `session/new` 返回 `configOptions`
  - `session/prompt`
  - 过程中持续接收 `session/update`
  - 最后由 `session/prompt` 返回 `stopReason`
- ACP agent 在运行中可以反向调用 client 能力：
  - 权限请求
  - 文件读写
  - 终端执行
- `session/new` 要求 client 提供：
  - `cwd`
  - `mcpServers`
  - 没有 MCP 时也应传空列表
- `session/update` 里会出现：
  - `agent_message_chunk`
  - `tool_call`
  - `tool_call_update`
  - 以及终端、diff 等内容块

截至 2026-04-11，ACP Registry 已列出 Claude Agent 和 Codex CLI 适配项。这说明“用 ACP 去操控外部 coding agent”不是私有扩展，而是协议的标准用法之一。

## 4) 当前仓库的正确落点

当前代码已经有几块现成的基础设施：

- `spawn` 已经是 engine-scoped tool。
- `SubtaskRunner` 已经统一了子任务执行入口。
- `SubtaskResult` 已经统一了子任务返回 envelope。
- `taskruntime.Runtime.RunSubtask(...)` 已经能接住同步子任务。
- `agent.Event` 和 Console 本地观察链已经能显示子任务和工具过程。
- `mcp.servers` 和 `mcphost` 已经能把 MCP server 接到本地 runtime。

所以 ACP 最合适的定位不是“另一种模型 provider”，而是“另一种外部子 agent 执行路径”。

原因很简单：

- provider 只负责一次 LLM 请求。
- ACP 是一个会话型 agent 协议。
- ACP agent 会主动回调 client 的文件系统和终端能力。
- ACP 还会持续发 `session/update`，这和当前子任务观察模型更接近。

## 5) 核心判断

### 5.1 不改造现有 `spawn` 语义

当前 `spawn` 的核心语义是：

- 给子 agent 一段任务描述
- 给它一个本地 tool 白名单
- 让它跑本地 `SubtaskRunner`

这套语义和 ACP 并不相同。

ACP 子任务不是“父 agent 白名单里的本地 tools 子集”，而是：

- 一个外部 agent 自己的执行栈
- 加上 client 侧暴露的 ACP 能力
- 再加上可选 MCP servers

如果硬把两者合成一个工具，会出现两个问题：

- `tools` 参数语义会变得含混。
- 旧 prompt 对 `spawn` 的理解会被破坏。

所以一期不建议把 ACP 塞进 `spawn`。

更稳的方案是新增一个单独的 engine-scoped tool，例如 `acp_spawn`。

### 5.2 ACP 的安全边界在 client 能力，不在 permission 请求

`session/request_permission` 更像 UX 协议，不是强安全边界。

真正决定外部 agent 能不能做某件事的，是我们在 client 侧提供的这些方法：

- `fs/read_text_file`
- `fs/write_text_file`
- `terminal/*`

所以一期的原则是：

- permission 请求可以按本地策略自动应答
- 真正的拒绝发生在文件和终端方法实现里

### 5.3 一期先做“每次任务一个 session”

当前本仓库的子任务就是同步的一次性调用。

ACP 一期直接对齐这个模型：

- 每次 `acp_spawn` 都新建 session
- 发一轮 `session/prompt`
- 收到终态后立刻关闭连接

这样最简单，也最符合当前 `SubtaskResult` 的边界。

## 6) 一期方案

### 6.1 新增配置：`acp.agents`

建议新增独立配置块：

```yaml
tools:
  acp_spawn:
    enabled: false

acp:
  agents:
    - name: codex
      enable: true
      type: stdio
      command: "<acp-wrapper-command>"
      args: []
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options: {}

    - name: claude
      enable: true
      type: stdio
      command: "<acp-wrapper-command>"
      args: []
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options: {}
```

字段说明：

- `tools.acp_spawn.enabled`
  - 显式工具入口开关。
  - 默认 `false`。
- `name`
  - 给主 agent 看的 profile 名。
- `type`
  - 一期固定只支持 `stdio`。
- `command` / `args` / `env`
  - 启动 ACP agent wrapper 的命令。
  - 这里不在文档里冻结具体 wrapper 命令，避免把某个外部实现写死。
- `cwd`
  - session 默认工作目录。
  - 配置允许相对路径，运行时再解成绝对路径。
- `read_roots`
  - ACP agent 允许读取的路径根。
- `write_roots`
  - ACP agent 允许写入的路径根。
- `session_options`
  - 透传给外部 ACP wrapper / session 的附加字段。
  - 一期会先原样放进 `session/new._meta`。
  - 如果 `session/new` 明确声明了某个 config option id，也会再补一轮 `session/set_config_option`。
  - MisterMorph 不对这些字段做通用语义解释。

### 6.2 新增工具：`acp_spawn`

建议新增 engine-scoped tool：

- `agent`
  - 必填，ACP profile 名。
- `task`
  - 必填，交给外部 ACP agent 的任务文本。
- `cwd`
  - 可选，覆盖 profile 默认工作目录。
- `output_schema`
  - 可选，沿用现有子任务 JSON 输出契约。
- `observe_profile`
  - 可选，沿用现有观察策略。

返回值不另起格式，继续复用现有 `SubtaskResult`：

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

这样父 agent 不需要知道子任务到底是本地 `spawn`，还是 ACP `acp_spawn`。

### 6.3 执行流程

```text
main agent
  -> acp_spawn(agent, task, cwd?, output_schema?, observe_profile?)
     -> load ACP profile
     -> spawn ACP process (stdio)
     -> initialize
     -> authenticate? (if advertised by agent)
     -> session/new(cwd, mcpServers=[], session_options?)
     -> session/set_config_option* (only for option ids advertised by session/new)
     -> session/prompt(prompt[])
        -> receive session/update*
        -> answer session/request_permission
        -> serve fs/*
        -> serve terminal/*
     -> collect final stopReason + final assistant text
     -> map to SubtaskResult
     -> close ACP session / process
```

### 6.4 ACP client 侧能力映射

#### A. `session/request_permission`

一期不做交互式确认。

处理原则：

- 读类操作默认可放行。
- 写类操作先看 profile 是否开放对应能力。
- 执行类操作也优先选择 allow 选项。
- 就算 permission 已放行，真正执行时仍要再过本地路径和命令约束。

这层的目标是兼容 ACP agent 的工作流，不是替代底层安全检查。

#### B. `fs/read_text_file`

这里不直接复用 `read_file` tool 的 JSON 接口，但要复用同一套约束原则：

- 路径必须落在 `read_roots` 内。
- 返回文本内容。
- 支持 ACP 的 `line` / `limit` 语义。
- 继续遵守 deny path 之类的本地规则。

#### C. `fs/write_text_file`

这里不直接调用 `write_file` tool，但应复用同一套写入边界：

- 路径必须落在 `write_roots` 内。
- 只支持 ACP 定义的整文件写入。
- 继续复用现有写入大小限制。
- 失败时返回明确错误，不做隐式回退。

#### D. `terminal/*`

真实联调表明，这组能力对 Codex 适配器是必需的。

所以一期补一个最小实现：

- `terminal/create`
  - 启本地 shell 命令。
  - `cwd` 只能落在 profile 的工作区约束内。
  - 输出按字节上限缓冲。
- `terminal/output`
  - 返回当前累计输出和是否截断。
- `terminal/wait_for_exit`
  - 等进程结束并返回退出状态。
- `terminal/kill`
  - 终止进程。
- `terminal/release`
  - 回收本地句柄。

这不是完整沙箱，只是最小兼容层。

### 6.5 `session/update` 到本地观察事件的映射

一期不追求把 ACP update 一比一塞进现有 `agent.Event`。

原因是当前 `agent.Event` 很轻，只够支撑本地 Console 文本预览。

所以一期做最小映射：

- `tool_call(status=pending)`
  - 映射成 `tool_start`
- `tool_call_update(status=in_progress)`
  - 维持运行中状态
- `tool_call_update(content=...)`
  - 映射成 `tool_output`
- `tool_call_update(status=completed|failed)`
  - 映射成 `tool_done`
- `agent_message_chunk`
  - 追加到最终输出 buffer
  - 同时按观察策略决定是否推送到运行中预览
这套映射的目标是：

- Console 能看到过程
- 父 agent 不吞进大量原始噪声
- 我们不用现在就重写整个事件协议

### 6.6 结果映射

ACP 最终要落回现有 `SubtaskResult`。

建议规则如下：

- `stopReason=end_turn`
  - 视为成功
- `stopReason=cancelled`
  - 视为失败
- `stopReason=refusal`
  - 视为失败
- `stopReason=max_tokens`
  - 视为失败
- `stopReason=max_turn_requests`
  - 视为失败

输出内容处理：

- 最终 `output` 使用累积后的最终 assistant 文本。
- 如果调用方声明了 `output_schema`，则沿用当前 `BuildSubtaskTask(...)` 的做法，在 prompt 里明确要求最终输出是 JSON。
- 终态时复用现有 `SubtaskResultFromFinal(...)` 逻辑做 JSON 归一化。

中间过程数据处理：

- tool call 原始内容
- diff

这些只进入运行中观察和日志，不自动塞回最终 `output`。

### 6.7 MCP

仓库现在已经有 `mcp.servers` 配置和 `mcphost` 结构。

但 ACP 这边一期先不做透传。

所以：

- `session/new.mcpServers` 固定传空列表。
- `acp.agents` 一期不增加 `mcp_servers` 配置。
- MCP 接入放到后续项。

## 7) 代码结构建议

建议新增一个独立包：

- `internal/acpclient/`

这个包先只做四件事：

- 传输层和 JSON-RPC client
- ACP session 生命周期
- client 能力实现：
  - permission
  - fs
  - terminal
- 把 ACP prompt turn 跑成一个 `SubtaskResult`

仓库接线点建议保持克制：

- `agent/registerEngineTools(...)`
  - 注册 `acp_spawn`
- `internal/channelopts`
  - 加载 `acp.agents`
- `taskruntime` / `integration`
  - 把 ACP profile 和 cleanup 接到 runtime assembly

一期先不要引入新的“通用外部 agent 调度总线”。

先把一个 ACP backend 跑通，再决定要不要抽象成更大的 dispatcher。

## 8) 实施顺序

### M1. 协议骨架

- ACP profile 配置解析
- `internal/acpclient` 传输层
- `initialize`
- `authenticate`
- `session/new`
- `session/set_config_option`
- `session/prompt`
- `session/update` 基础消费
- fake ACP server 测试

### M2. 工具接线

- engine-scoped `acp_spawn`
- `SubtaskResult` 映射
- `output_schema` 约束复用
- context cancel -> `session/cancel`

### M3. client 能力

- `session/request_permission`
- `fs/read_text_file`
- `fs/write_text_file`
- `terminal/*`
- Console 运行中预览接线

### M4. 真实联调

- 针对 Codex / Claude ACP adapter 的 opt-in 集成测试
- 记录兼容性差异

## 9) 主要风险

- 不同 ACP wrapper 的能力覆盖不完全一致。
- `stdio` 模式下，外部 ACP wrapper 自身仍是本地子进程。
- 如果 wrapper 本身直接访问宿主文件系统或执行命令，这部分权限不受 ACP client 的 `fs/*` 方法约束。
- `terminal/*` 现在已补上，但这层仍不是强沙箱。
- 某些 wrapper 可能依赖更多 session mode / config option，当前只支持“agent 已声明的 option id”这一层。
- 长输出和高频 `session/update` 可能让 Console 预览抖动。
- ACP 的 permission 语义和我们现有 guard approval 不是一回事，后面要避免把两者混成一层。

## 10) 测试要求

至少要覆盖下面这些测试：

- ACP client 和 fake server 的协议往返。
- `authenticate` 的方法选择。
- `stopReason` 到 `SubtaskResult` 的映射。
- `output_schema` 的 JSON 输出约束。
- 路径越界时 `fs/read_text_file` / `fs/write_text_file` 会拒绝。
- `session_options` 会进入 `session/new._meta`，并对已声明 option id 发 `session/set_config_option`。
- `terminal/*` 的 create / output / wait / kill / release 往返。
- `session/cancel` 后连接和子进程会正确清理。
- Console 预览能看到 ACP tool call 和 `agent_message_chunk`。

真实 ACP wrapper 联调测试建议做成 opt-in：

- 默认 CI 不跑。
- 本地有 wrapper 时再开。

## 11) 后续项

如果一期跑通，再考虑后面的事：

- `session/load`
- 更完整的 session mode / config option 抽象
- HTTP ACP transport
- MCP 透传
- 评估 `spawn` 和 `acp_spawn` 是否需要在更高一层合并

## 12) 参考

- ACP Protocol Overview: <https://agentclientprotocol.com/protocol/overview>
- ACP Initialization: <https://agentclientprotocol.com/protocol/initialization>
- ACP Session Setup: <https://agentclientprotocol.com/protocol/session-setup>
- ACP Prompt Turn: <https://agentclientprotocol.com/protocol/prompt-turn>
- ACP Tool Calls: <https://agentclientprotocol.com/protocol/tool-calls>
- ACP File System: <https://agentclientprotocol.com/protocol/file-system>
- ACP Terminals: <https://agentclientprotocol.com/protocol/terminals>
- ACP Registry: <https://agentclientprotocol.com/registry>
