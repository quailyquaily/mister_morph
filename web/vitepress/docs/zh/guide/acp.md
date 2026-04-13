---
title: ACP
description: 通过 acp_spawn 调用外部 ACP agent。
---

# ACP

Mister Morph 现在可以把一个隔离子任务委托给外部 ACP agent。

当前实现刻意收得很窄：

- Mister Morph 只做 ACP client，不做 ACP server。
- 只支持 `stdio`。
- 每次 `acp_spawn` 都是一个同步 session，只跑一轮 prompt turn。
- 外部 agent 进程来自 `acp.agents` 配置。

## 什么时候用 ACP

当子任务应该跑在“外部 agent 执行栈”里，而不是另一个本地 Mister Morph loop 里时，用 ACP。

典型场景：

- 通过 ACP 适配层调用 Codex
- 接别的 ACP 兼容 coding agent
- 父 loop 只负责调度，把文件读写和命令执行交给外部专业 agent

如果你只是想再起一个本地 Mister Morph 子 agent，用 [Subagents](/zh/guide/subagents) 里的 `spawn`。

## 当前支持

现在已经支持：

- `authenticate`
- `session/new`
- 对 `session/new` 已声明 option id 的 `session/set_config_option`
- `session/prompt`
- `session/request_permission`（含 Cursor 文档中的 `allow-once` 等连字符形式）
- `fs/read_text_file`
- `fs/write_text_file`
- 最小 `terminal/*`
- Cursor ACP 的阻塞扩展方法：`cursor/ask_question` 跳过；`cursor/create_plan` 自动接受（无交互审阅），避免子进程无限等待

暂时还不支持：

- MCP 透传
- session 复用
- HTTP / SSE transport

## 配置

要配两件事：

1. 打开显式工具入口
2. 定义至少一个 ACP profile

```yaml
tools:
  acp_spawn:
    enabled: true

acp:
  agents:
    - name: "codex"
      enable: true
      type: "stdio"
      command: "codex-acp"
      args: []
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        mode: "auto"
        reasoning_effort: "low"
```

补充说明：

- `tools.acp_spawn.enabled` 只控制 `acp_spawn` 这个显式入口。
- `session_options` 会先透传到 `session/new._meta`。
- 如果 ACP agent 在 `session/new` 里声明了 config option id，同名字段还会再通过 `session/set_config_option` 发一遍。

## Prompt 写法

要在父 agent 的任务里明确要求它调用 `acp_spawn`。

例如：

```text
只允许调用 acp_spawn。agent 用 codex。读取 ./README.md，并用中文写 5 句话总结。禁止调用 spawn，禁止自己读文件。
```

`acp_spawn` 支持这些参数：

- `agent`
- `task`
- `cwd`
- `output_schema`
- `observe_profile`

返回值沿用现有的 `SubtaskResult` envelope。

## 运行时行为

一次 `acp_spawn` 调用会做这些事：

1. 启动配置里的 wrapper 进程
2. `initialize`
3. 需要时 `authenticate`
4. `session/new`
5. 对已声明选项发 `session/set_config_option`
6. `session/prompt`
7. 在 turn 中处理文件、权限和终端回调
8. 收集最终文本输出

## 安全说明

ACP 的 permission 请求不是唯一边界。

真正的限制发生在已经实现的 client 方法里：

- 允许读取和写入的路径根
- 允许的终端工作目录
- 本地写入和进程执行规则

还有一点要看清：wrapper 本身仍是本地子进程。ACP 回调层的约束，不会自动把 wrapper 自己的直接行为也沙箱化。

## Codex 的两条接法

现在 Codex 有两条接法。

### 外部适配层

你仍然可以继续用 `codex-acp` 这类外部 ACP 适配层。

联调前先检查：

1. `codex` 自己先能正常工作
2. `mistermorph tools` 里能看到 `acp_spawn`
3. ACP profile 的 `command` 指向 `codex-acp`

### 仓库内自带 wrapper

仓库里现在也有一个 MisterMorph 自己维护的 Codex wrapper：

```yaml
acp:
  agents:
    - name: "codex"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/codex/src/index.mjs"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        approval_policy: "never"
```

这个 native wrapper 当前的范围：

- 后端直接接 `codex app-server`
- 不依赖第三方 ACP adapter
- 还没有交互式 approval 流程
- 默认 `approval_policy` 是 `never`

现有的 opt-in live 集成测试也能直接打这个 wrapper：

```bash
MISTERMORPH_ACP_CODEX_INTEGRATION=1 \
MISTERMORPH_ACP_CODEX_COMMAND=node \
MISTERMORPH_ACP_CODEX_ARGS="./wrappers/acp/codex/src/index.mjs" \
go test ./internal/acpclient -run TestRunPrompt_CodexACPIntegration -v
```

## Claude 的 native wrapper

仓库里现在也有一个 Claude 的 native wrapper：

```yaml
acp:
  agents:
    - name: "claude"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/claude/src/index.mjs"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        permission_mode: "dontAsk"
        allowed_tools: ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
```

这个 wrapper 当前的范围：

- 后端直接接 `claude -p --output-format stream-json`
- 不依赖第三方 ACP adapter
- 还没有交互式 approval 流程
- Claude 内部工具不会再拆回 ACP 的文件或终端回调

注意两点：

- `bare: true` 只是可选项，不该默认打开
- 如果你依赖 Claude.ai 登录态，通常要保持 `bare: false`，因为 bare mode 会跳过 OAuth 和 keychain 读取

仓库里也加了 opt-in 的 live 集成测试：

```bash
MISTERMORPH_ACP_CLAUDE_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_ClaudeNativeWrapperIntegration -v
```

## Cursor CLI（`agent acp`）

Cursor 命令行自带的 `agent acp` 本身就是 ACP server（stdio），与 Codex/Claude 的「桥接 wrapper」不同：仓库里提供的是 **透明 stdio 代理**，把 MisterMorph 的 JSON-RPC 原样转发给 Cursor CLI。

先在本机安装 Cursor CLI，保证 `agent` 在 `PATH` 中，并完成认证（`agent login`，或通过环境变量/参数传入 API key，见 [Cursor ACP 文档](https://cursor.com/cn/docs/cli/acp)）。

配置示例：

```yaml
acp:
  agents:
    - name: "cursor"
      enable: true
      type: "stdio"
      command: "node"
      args: ["./wrappers/acp/cursor/src/index.mjs"]
      env:
        MISTERMORPH_CURSOR_ARGS: "--api-key ${CURSOR_API_KEY}"
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
```

说明：

- `MISTERMORPH_CURSOR_COMMAND` 可覆盖 `agent` 可执行文件路径
- `MISTERMORPH_CURSOR_ARGS` 为 `acp` 之前的额外参数（空格分隔）
- 仪表盘级团队 MCP 在 ACP 模式下不可用（以 Cursor 文档为准）

可选联调（需本机已安装并登录 Cursor CLI）：

```bash
MISTERMORPH_ACP_CURSOR_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CursorACPProxyIntegration -v
```

另见：

- [Subagents](/zh/guide/subagents)
- [内置工具](/zh/guide/built-in-tools)
- [配置字段](/zh/guide/config-reference)
