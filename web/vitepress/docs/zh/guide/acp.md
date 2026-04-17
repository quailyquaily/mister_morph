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
      command: "npx"
      args: ["-y", "@archkk/acp-codex"]
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
- ACP profile 固定作为本地 `stdio` 子进程启动。
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

还有一点要看清：ACP command 本身仍是本地子进程。ACP 回调层的约束，不会自动把这个进程自己的直接行为也沙箱化。

## Codex

Codex 现在应该按外部 ACP adapter 来接。

常见选择：

- `npx -y @archkk/acp-codex`
- 先执行 `npm i -g @archkk/acp-codex`，再用 `mistermorph-acp-codex`
- `codex-acp`
- `npx -y @zed-industries/codex-acp`

联调前先检查：

1. `codex` 自己先能正常工作
2. `mistermorph tools` 里能看到 `acp_spawn`
3. ACP profile 的 `command` 指向你的 Codex ACP adapter

可选 live 集成测试：

```bash
MISTERMORPH_ACP_CODEX_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CodexACPIntegration -v
```

这个测试默认找 `codex-acp`。如果你要验证已发布的 npm 包，可以设置 `MISTERMORPH_ACP_CODEX_COMMAND=npx` 和 `MISTERMORPH_ACP_CODEX_ARGS="-y @archkk/acp-codex"`。

## Claude

Mistermorph 主仓里不再自带 Claude wrapper。

请改用任何外部 Claude ACP adapter。示例：

```yaml
acp:
  agents:
    - name: "claude"
      command: "npx"
      args: ["-y", "@archkk/acp-claude"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
      session_options:
        permission_mode: "dontAsk"
        allowed_tools: ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
```

公开可用的选择：

- `npx -y @archkk/acp-claude`
- 先执行 `npm i -g @archkk/acp-claude`，再用 `mistermorph-acp-claude`

可选 live 集成测试：

```bash
MISTERMORPH_ACP_CLAUDE_INTEGRATION=1 \
MISTERMORPH_ACP_CLAUDE_COMMAND=npx \
MISTERMORPH_ACP_CLAUDE_ARGS="-y @archkk/acp-claude" \
go test ./internal/acpclient -run TestRunPrompt_ClaudeACPIntegration -v
```

## Cursor CLI（`agent acp`）

Cursor CLI 自己就直接讲 ACP（stdio），所以主仓里不再保留透明 proxy。

先在本机安装 Cursor CLI，保证 `agent` 在 `PATH` 中，并完成认证（`agent login`，或通过环境变量/参数传入 API key，见 [Cursor ACP 文档](https://cursor.com/cn/docs/cli/acp)）。

配置示例：

```yaml
acp:
  agents:
    - name: "cursor"
      command: "agent"
      args: ["acp"]
      env: {}
      cwd: "."
      read_roots: ["."]
      write_roots: ["."]
```

如果需要 API key 之类的参数，把它们放在最后一个 `acp` 之前，例如 `args: ["--api-key", "${CURSOR_API_KEY}", "acp"]`。

可选联调（需本机已安装并登录 Cursor CLI）：

```bash
MISTERMORPH_ACP_CURSOR_INTEGRATION=1 \
go test ./internal/acpclient -run TestRunPrompt_CursorACPIntegration -v
```

另见：

- [Subagents](/zh/guide/subagents)
- [内置工具](/zh/guide/built-in-tools)
- [配置字段](/zh/guide/config-reference)
