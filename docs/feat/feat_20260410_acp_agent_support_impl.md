---
date: 2026-04-10
title: ACP 外部 Agent 支持实现进度
status: in_progress
---

# ACP 外部 Agent 支持实现进度

## 当前范围

本轮先做一期最小实现：

- 新增 `acp.agents` 配置读取。
- 新增 `tools.acp_spawn.enabled` 开关，默认 `false`。
- 新增 engine-scoped `acp_spawn`。
- 新增 `internal/acpclient/`，实现：
  - `stdio` 传输
  - `initialize`
  - `authenticate`
  - `session/new`
  - `session/set_config_option`
  - `session/prompt`
  - `session/update` 基础消费
  - `session/request_permission`
  - `fs/read_text_file`
  - `fs/write_text_file`
  - `terminal/*`
- 一期不实现：
  - MCP 透传
  - session 复用
- `session_options` 先写入 `session/new._meta`，再按 `session/new.configOptions` 补 `session/set_config_option`。
- 结果继续复用现有 `SubtaskResult`。

## 任务清单

- [x] 建立实现跟踪文档
- [x] 梳理当前接线点和最小改动路径
- [x] 定义 ACP 配置结构与读取逻辑
- [x] 定义 `internal/acpclient` 基础类型和 JSON-RPC client
- [x] 跑通 `initialize -> session/new -> session/prompt`
- [x] 接 `authenticate`
- [x] 接 `session/set_config_option`
- [x] 接 `session/request_permission`
- [x] 接 `fs/read_text_file`
- [x] 接 `fs/write_text_file`
- [x] 接 `terminal/*`
- [x] 新增 `acp_spawn`
- [x] 接入 runtime / integration 装配
- [x] 补 fake ACP server 测试
- [x] 修正 timeout 竞争导致的假成功
- [x] 新增 opt-in 的真实 Codex adapter 集成测试
- [x] 跑相关测试并记录结果
- [x] 收敛主仓 ACP 范围，只保留 `acp_spawn`、`internal/acpclient` 和回调边界
- [x] 删除仓库内 `wrappers/acp/cursor/` 透明 proxy，文档改为直接配置 `agent acp`
- [x] 评估并执行 `wrappers/acp/codex/` 迁出主仓，改为单独 repo 或可选项目维护
- [x] 评估并执行 `wrappers/acp/claude/` 迁出主仓，改为单独 repo 或可选项目维护
- [x] 清理 ACP 文档和配置样例，去掉对仓库内 native wrapper / cursor proxy 的默认依赖

## 进度记录

### 2026-04-10

- 已建立 ACP 设计文档：
  - `docs/feat/feat_20260410_acp_agent_support.md`
- 已新建实现分支：
  - `feat/acp`
- 已确认一期口径：
  - 使用单独 `acp_spawn`
  - `tools.acp_spawn.enabled=false`
  - 不接现有 guard approval
  - 不做 MCP 透传
  - `session_options` 先保留透传口，再按真实 wrapper 需要补协议映射
- 已完成最小实现：
  - 新增 `internal/acpclient/config.go`
  - 新增 `internal/acpclient/client.go`
  - 新增 engine-scoped `acp_spawn`
  - `EngineToolsConfig` 已扩展 `ACPSpawnEnabled`
  - `run` / `console` / `taskruntime` / `integration` 已接 ACP tool 开关和 profile
  - `assets/config/config.example.yaml` 已补 `tools.acp_spawn.enabled` 和 `acp.agents`
- 已完成测试：
  - `go test ./internal/acpclient ./agent ./internal/channelruntime/taskruntime ./integration ./cmd/mistermorph/consolecmd`
  - `go test ./internal/channelopts ./cmd/mistermorph -run 'TestToolsCommand_IncludesRuntimeTools|TestBuildTelegramRunOptionsTaskTimeoutFallback|TestBuildSlackRunOptionsTaskTimeoutFallback|TestTelegramConfigFromReaderImageSources'`
- 已确认一个实现边界：
  - `stdio` 模式下，外部 ACP wrapper 自身仍是本地子进程
  - 如果 wrapper 直接访问宿主文件系统或执行命令，这部分权限不受 ACP client 的 `fs/*` 方法约束
  - 这点已回写设计文档风险部分

### 2026-04-11

- 已确定 Codex 的真实联调对象不是 `codex` CLI 本体，而是外部 ACP adapter：
  - 当前优先目标为 `zed-industries/codex-acp`
  - `codex` CLI 公开帮助里暴露的是 `app-server` / `mcp-server`，不是 ACP 入口
- 已确认 `codex-acp` 在 `initialize` 后会声明认证方式：
  - 当前已兼容 `chatgpt`
  - 当前已兼容 `codex-api-key`
  - 当前已兼容 `openai-api-key`
- 已确认 `codex-acp` 的 `session/new` 会返回 `configOptions`：
  - 当前实现会把 `session_options` 先放入 `session/new._meta`
  - 再对已声明的 option id 发 `session/set_config_option`
- 真实联调发现仅有 `fs/*` 不够，已补最小 `terminal/*`：
  - `terminal/create`
  - `terminal/output`
  - `terminal/wait_for_exit`
  - `terminal/kill`
  - `terminal/release`
- 权限策略已调整：
  - 对执行类 permission 不再预先拒绝
  - 优先选择 allow 选项，再把真正限制放在终端和文件实现里
- 已修正一个关键竞争条件：
  - 外层 context 超时后会发 `session/cancel`
  - 旧实现可能把超时后的迟到响应误判成成功
  - 现在 `session/prompt` 返回后会再次检查 `ctx.Err()`
- 已新增 opt-in 的真实集成测试：
  - `internal/acpclient/codex_integration_test.go`
  - 目标是直接验证 `initialize -> session/new -> session/prompt` 能否和真实 Codex ACP adapter 跑通
- 集成测试约定：
  - 默认不跑，需显式设置 `MISTERMORPH_ACP_CODEX_INTEGRATION=1`
  - 默认优先查找 `codex-acp`
  - 也可通过 `MISTERMORPH_ACP_CODEX_COMMAND` 指定命令
  - 可通过 `MISTERMORPH_ACP_CODEX_ARGS` 指定参数，例如 `-y @zed-industries/codex-acp`
  - 可通过 `MISTERMORPH_ACP_CODEX_SESSION_OPTIONS` 传入 JSON
  - live test 对单次 `RunPrompt()` 额外设置了时长上限，避免静默挂死
- 配置模板已补 Codex adapter 注释示例：
  - `assets/config/config.example.yaml`
- 当前本地测试结论：
  - fake ACP server 往返已覆盖 auth / fs / terminal / session option
  - 真实 Codex adapter 是否最终跑通，仍以用户本机 shell 为准
  - 当前执行环境的网络限制会影响 `codex-acp` 连接外部服务，因此这里不把本地 sandbox 结果当成最终结论
- 用户本机真实联调已通过：
  - `只允许调用 acp_spawn。agent 用 codex，让 codex 说 Hello。`
  - `读取 README 并总结` 这类文件任务也已恢复正常
- 这轮真实联调修正了两个关键问题：
  - `terminal/create` 最初没有按 ACP 规范处理 `command + args[] + env[]`
  - `RunPrompt()` 成功路径里，cancel watcher 的收尾顺序会造成死锁，进而把成功请求拖到外层总超时
- 为了定位真实 wrapper 问题，曾临时加过 `acp_*` 运行日志：
  - 联调完成后已降为 `debug`
  - 默认 `info` 日志下不再污染正常输出

### 2026-04-14

- 已形成下一轮简化方向，目标是把 ACP 收回“主仓只保留核心能力”：
  - 保留 `acp_spawn`
  - 保留 `internal/acpclient`
  - 保留本地文件、终端、权限回调边界
- 当前判断：
  - `wrappers/acp/codex/` 和 `wrappers/acp/claude/` 属于 backend 适配器，不是 ACP 核心能力
  - `wrappers/acp/cursor/` 只是透明转发，不增加协议能力，适合直接删除
- 已记录的后续动作：
  - Cursor 改为文档直接指导配置 `command: "agent"` 和 `args: ["acp"]`
  - Codex / Claude native wrapper 改为迁出主仓，单独维护
  - 主仓文档改成“接受任意已经会讲 ACP 的外部 command”，不再默认自带这些 wrapper
- 已完成主仓收口：
  - `wrappers/acp/codex/`、`wrappers/acp/claude/`、`wrappers/acp/shared/` 已迁到独立目录 `mistermorph-acp-adapters/`
  - `wrappers/acp/cursor/` 已从主仓删除
  - `docs/acp.md`、VitePress 指南和 `assets/config/config.example.yaml` 已改成外部 adapter / 直接 `agent acp` 口径
  - 主仓集成测试已去掉对仓库内 wrapper 路径的硬依赖

## 待确认的实现细节

当前没有新的阻塞项。

后续如果发现某个 wrapper 需要更严格的 config typing 或更细的 terminal 沙箱，再单独记录并回写设计文档。
