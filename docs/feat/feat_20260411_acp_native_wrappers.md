---
date: 2026-04-11
title: 自研 ACP Wrapper 设计
status: draft
---

# 自研 ACP Wrapper 设计

> 2026-04-14 补记：本文记录的是当时把 wrapper 放在主仓内的设计。后续 `codex` / `claude` adapter 已迁到独立目录 `mistermorph-acp-adapters/`，下面的仓库内路径仅作为历史记录。

## 1) 目标

当前 ACP client 已经能跑通。

下一步要解决的是另一个问题：

- 不依赖第三方 ACP adapter。
- 继续保留 ACP 作为 MisterMorph 和外部 agent 之间的统一边界。
- 让 MisterMorph 自己提供 `codex` 和 `claude` 的 wrapper。

这样做的直接好处是：

- 少一层外部依赖。
- 问题定位更短。
- 配置和行为可以按 MisterMorph 自己的需求收敛。

## 2) 基本判断

这里要把两层事情分开。

第一层是 ACP 协议本身。

- MisterMorph 已经是 ACP client。
- 这层先不重写。

第二层是目标 agent 的桥接层。

- 这层以前依赖 `codex-acp` 之类的 adapter。
- 现在改成我们自己写。

所以这轮不是“重做 ACP”，而是“自己实现 wrapper”。

## 3) 总体结构

结构保持简单：

```text
MisterMorph (Go, ACP client)
  <-> self-owned ACP wrapper
  <-> target agent native interface
```

当前规划：

- `codex` wrapper
  - 后端接 `codex app-server`
- `claude` wrapper
  - 后端接 `claude -p --output-format stream-json`

这两个 wrapper 都单独跑成子进程，通过 `stdio` 讲 ACP。

原因很直接：

- MisterMorph 当前已经是 `stdio` ACP client。
- `pi-acp` 这类 adapter 也是这个形状。
- 子进程边界最清楚，调试也简单。

## 4) 语言选择

wrapper 先用 Node.js 写。

原因是：

- `codex app-server` 官方直接给了 Node / TypeScript 示例。
- `claude -p` 也是 Claude Code 官方的程序化入口。
- 用 Node 内置模块就能先把第一版做出来，不必先引入额外构建链。

这期先写成不依赖第三方包的 ESM 脚本。

后面如果类型复杂度上来，再收敛成 TypeScript 编译产物。

## 5) Codex Wrapper 一期范围

第一版先只做 `codex`。

目录建议：

```text
wrappers/acp/codex/
```

协议面只做最小可用集合：

- `initialize`
- `authenticate`
  - 先做 no-op
- `session/new`
- `session/set_config_option`
  - 只支持一小部分 option id
- `session/prompt`
- `session/cancel`

事件先做：

- `agent_message_chunk`
- 基础 `tool_call`
- 基础 `tool_call_update`

后端桥接关系：

- ACP `session/new`
  - 对应 `codex app-server` 的 `thread/start`
- ACP `session/prompt`
  - 对应 `turn/start`
- ACP `session/cancel`
  - 对应 `turn/interrupt`
- Codex `item/agentMessage/delta`
  - 映射成 ACP `agent_message_chunk`
- Codex 命令执行 / 文件变更通知
  - 映射成 ACP `tool_call` / `tool_call_update`

## 6) Codex Wrapper 的刻意收缩

这期不追求把 Codex 全部能力都桥接出来。

先明确不做：

- 会话持久化
- slash commands
- MCP passthrough
- 动态 tool call
- review mode
- 图像输入
- 复杂 approvals
- 多 session 并发优化

第一版默认策略也先写死：

- 每个 ACP session 对应一个 Codex thread
- `approval_policy` 默认用 `never`
- wrapper 不做交互式用户确认

这样做不是最终形态，但能先把“自研 wrapper 能跑起来”这件事做实。

## 7) 为什么先做 Codex

Codex 更适合作为第一个目标。

原因不是主观偏好，而是接口形状更合适：

- `codex app-server` 本身就是 `JSON-RPC` over `stdio`
- 它已经明确提供 thread / turn / event stream
- 这和 ACP 的 session / prompt / update 非常接近

所以 `codex` 这条桥接更像协议映射问题，而不是黑盒驱动问题。

## 8) Claude Wrapper 规划

`claude` wrapper 放在第二步。

方向先定成：

- wrapper 继续讲 ACP
- 后端接 `claude -p --output-format stream-json`

这里直接桥接 Claude Code CLI，而不是先引 SDK 包。

原因很直接：

- 这就是直接操控本机 Claude Code。
- `claude -p` 本身就是官方程序化入口。
- `stream-json` 已经提供了可解析事件流。
- 第一版不需要额外依赖。

当前桥接关系：

- ACP `session/new`
  - 建立本地 wrapper session，并保存默认 CLI 选项
- ACP `session/prompt`
  - 启动一次 `claude -p`
- ACP `session/cancel`
  - 终止当前 `claude` 子进程
- Claude `stream-json`
  - 文本增量映射成 `agent_message_chunk`
  - 结果事件映射成 ACP `stopReason`

有一个边界要单独写明：

- `bare: true` 不能作为默认值
- Claude Code 文档明确说明 bare mode 会跳过 OAuth 和 keychain 读取
- 如果用户依赖 Claude.ai 登录态，通常必须保持 `bare: false`

## 9) 配置入口

MisterMorph 主体配置暂时不变。

仍然用：

- `tools.acp_spawn.enabled`
- `acp.agents`

只是 `acp.agents[].command` 不再必须指向第三方 adapter，也可以指向仓库自带 wrapper。

例如 `codex`：

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

## 10) 交付顺序

按返工成本，顺序定成：

1. 写设计文档和实现跟踪文档。
2. 落 `codex` wrapper 最小骨架。
3. 用现有 Go ACP client 跑 live smoke test。
4. 补基本文档和配置示例。
5. 再开始 `claude` wrapper。

## 11) 当前验收标准

`codex` wrapper 第一版达到下面几条就算过线：

- `acp_spawn` 可以直接调用仓库内 wrapper
- `Say exactly: Hello` 能稳定返回
- 读取本地文件并总结能稳定返回
- 命令不会再因为未处理的协议收尾而挂死
- 文档里明确写清范围和限制
