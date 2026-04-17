---
date: 2026-04-11
title: 自研 ACP Wrapper 实现进度
status: in_progress
---

# 自研 ACP Wrapper 实现进度

> 2026-04-14 补记：这里记录的是 wrapper 还在主仓时的实现过程。后续 `codex` / `claude` adapter 已迁到独立目录 `mistermorph-acp-adapters/`，下面的主仓路径仅作为历史记录。

## 当前范围

本轮先做最小可用版本：

- 新增自研 `codex` ACP wrapper
- 位置放在仓库内
- 不依赖第三方 npm 包
- 通过 `stdio` 讲 ACP
- 后端桥接 `codex app-server`

当前不做：

- wrapper 安装发布流程
- MCP passthrough
- session 持久化
- 复杂 approval 桥接

## 任务清单

- [x] 明确“继续走 ACP，但改成自研 wrapper”
- [x] 写设计文档
- [x] 建 `codex` wrapper 目录和脚手架
- [x] 接 `codex app-server`
- [x] 跑通 `initialize -> session/new -> session/prompt -> session/cancel`
- [x] 补最小单测
- [x] 补配置示例
- [x] 补用户文档
- [x] 跑 live smoke test

## 进度记录

### 2026-04-11

- 已确认继续保留 ACP，改成自研 wrapper。
- 已确认参考方向是 `pi-acp` 这类独立 adapter 进程，而不是把桥接逻辑塞回 Go 主体。
- 已确认 `codex` 适合作为第一个目标：
  - `codex app-server` 是官方接口
  - 传输是 `stdio`
  - 协议是 `JSON-RPC`
  - 有清晰的 thread / turn / event stream
- 已调整 `claude` 路线：
  - 第一版不接 SDK 包
  - 直接桥接 `claude -p --output-format stream-json`
  - 这样更接近“直接操控 Claude Code”
  - 也避免先引额外依赖
- 已落第一版 `codex` wrapper：
  - 目录：`wrappers/acp/codex/`
  - 运行形态：Node.js `stdio` ACP agent
  - 后端：`codex app-server`
  - 当前支持：
    - `initialize`
    - `authenticate`（no-op）
    - `session/new`
    - `session/set_config_option`
    - `session/prompt`
    - `session/cancel`
    - 文本 `agent_message_chunk`
    - 基础 `tool_call` / `tool_call_update`
- 已补最小单测：
  - `node --test wrappers/acp/codex/test/*.test.mjs`
- 已在用户机环境跑通 live 集成测试：
  - `MISTERMORPH_ACP_CODEX_INTEGRATION=1 MISTERMORPH_ACP_CODEX_COMMAND=node MISTERMORPH_ACP_CODEX_ARGS="./wrappers/acp/codex/src/index.mjs" go test ./internal/acpclient -run TestRunPrompt_CodexACPIntegration -v`
- 这轮修了一个 wrapper 自己的协议问题：
  - `codex app-server` 的 turn 状态会返回 `inProgress`
  - 第一版状态映射没有统一转小写，导致 `session/prompt` 提前报错
- 当前结论：
  - 仓库内自带 wrapper 已经能替代第三方 `codex-acp` 跑最小主线
  - 复杂 approval 和更细的能力映射仍然留在后续迭代
- 已落第一版 `claude` wrapper：
  - 目录：`wrappers/acp/claude/`
  - 运行形态：Node.js `stdio` ACP agent
  - 后端：`claude -p --output-format stream-json`
  - 当前支持：
    - `initialize`
    - `authenticate`（no-op）
    - `session/new`
    - `session/set_config_option`
    - `session/prompt`
    - `session/cancel`
    - 文本 `agent_message_chunk`
- 已补两层测试：
  - Node 单测：`node --test wrappers/acp/claude/test/*.test.mjs`
  - Go 端到端假后端集成测试：`internal/acpclient/claude_wrapper_integration_test.go`
- 已新增 opt-in live 集成测试：
  - `internal/acpclient/claude_integration_test.go`
  - 依赖本机 `claude` 和可用认证
- 当前已确认一个 Claude 侧边界：
  - `claude auth status` 成功，不等于当前组织一定能实际执行 Claude 请求
  - live 测试仍以真实 `claude -p` 结果为准
- 当前已确认一个配置边界：
  - bare mode 不能默认打开
  - 文档明确说明 bare mode 会跳过 OAuth / keychain
  - 对依赖 Claude.ai 登录态的用户，这会直接影响可用性

## 当前风险

- `codex app-server` 的审批和权限请求面比第一版 wrapper 想做的范围更宽。
- 如果某些任务必须经过 approval request，第一版可能只能通过默认 `approval_policy: never` 先避开。
- `claude` 这条线的 live 可用性依赖真实账号权限。
- 当前 wrapper 只桥接 Claude 的输出流，不把 Claude 内部工具行为再拆回 ACP 的 file / terminal callback。
