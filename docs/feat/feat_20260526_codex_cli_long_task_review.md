---
date: 2026-05-26
title: Codex CLI 长任务能力审阅
status: draft
---

# Codex CLI 长任务能力审阅

## 1) 范围

这份文档审阅 OpenAI Codex CLI 的公开代码，并和本仓库当前实现对比，找出值得学习的部分。

上游仓库：

- https://github.com/openai/codex
- GitHub API 元数据：默认分支 `main`，license 为 `Apache-2.0`，描述为本地终端 coding agent。

主要审阅的上游文件：

- `README.md`
- `codex-rs/core/src/session/turn.rs`
- `codex-rs/core/src/session/session.rs`
- `codex-rs/core/src/session/input_queue.rs`
- `codex-rs/core/src/context_manager/history.rs`
- `codex-rs/core/src/compact.rs`
- `codex-rs/core/src/thread_rollout_truncation.rs`
- `codex-rs/core/src/tools/orchestrator.rs`
- `codex-rs/core/src/tools/registry.rs`
- `codex-rs/core/src/tools/parallel.rs`
- `codex-rs/core/src/tools/runtimes/unified_exec.rs`
- `codex-rs/core/src/unified_exec/process_manager.rs`
- `codex-rs/core/src/unified_exec/head_tail_buffer.rs`
- `codex-rs/core/src/exec_policy.rs`
- `codex-rs/core/src/turn_diff_tracker.rs`
- `codex-rs/app-server-protocol/src/protocol/v2/turn.rs`
- `codex-rs/app-server-protocol/src/protocol/v2/item.rs`
- `codex-rs/app-server-protocol/src/protocol/event_mapping.rs`
- `codex-rs/tui/src/app/event_dispatch.rs`
- `codex-rs/tui/src/app/session_lifecycle.rs`
- `codex-rs/tui/src/bottom_pane/mod.rs`

主要对照的本仓库文件：

- `agent/engine.go`
- `agent/engine_loop.go`
- `agent/context.go`
- `agent/events.go`
- `agent/engine_resume.go`
- `agent/engine_runstate.go`
- `agent/subtask.go`
- `tools/registry.go`
- `tools/builtin/bash.go`
- `tools/builtin/shell_runner.go`
- `internal/channelruntime/taskruntime/runtime.go`
- `internal/chatcommands/runtime.go`
- `cmd/mistermorph/chatcmd/commands.go`
- `cmd/mistermorph/consolecmd/local_runtime.go`
- `cmd/mistermorph/consolecmd/stream_events.go`
- `internal/chathistory/prompt.go`
- `internal/topiccontext/store.go`
- `internal/llmstats/client.go`

## 2) 第一性原理

编程类长期任务容易失败，主要不是因为少了某个 prompt，而是因为运行过程会不断破坏模型可用的信息结构：

- 历史变长以后，模型会忘掉早期约束，或者把工具调用和工具返回关系搞乱。
- 命令输出会很大，保留全量会浪费上下文，只保留开头又会丢掉最终错误。
- shell 命令可能长时间运行，需要输出流、取消、继续读取和后台进程管理。
- 用户会在任务运行时补充新信息，系统需要能接住，而不是等当前请求完全结束。
- 写文件、删文件、跑命令都有权限风险，需要明确的审批和可复用的批准规则。
- UI 不能只拿一段最终文本，它需要知道当前是在思考、跑命令、等审批、应用 patch，还是压缩上下文。
- resume、fork、rollback 不能按原始消息条数切，需要按语义 turn 切。

Codex CLI 做得好的地方，是把这些问题都建成了明确的运行状态，而不是把所有东西塞进一条 prompt 循环。

## 3) Codex CLI 做得好的地方

### 3.1 Session 和 Turn 是一等对象

`session/session.rs` 把线程级状态集中到 `Session`，包括模型、provider、cwd、workspace roots、权限、hook、MCP、skill/plugin、unified exec、环境和持久 rollout。

`session/turn.rs` 负责一次 turn 的生命周期：

- turn 开始前执行 pre-sampling compaction。
- 注入当前上下文、skill、plugin、hook 结果。
- 构造给模型的 prompt history。
- 读取 Responses API 流式事件。
- 处理工具调用。
- 等工具结果回到 history。
- 检查 pending input。
- 必要时在 turn 中途自动 compaction，再继续下一轮模型请求。
- turn 停止时发出事件和统计。

这比一个 `for step := 0; step < max; step++` 循环复杂，但复杂度来自真实问题：长期任务需要可恢复、可中断、可压缩、可观察。

### 3.2 History 有不变量，不只是 `[]Message`

`context_manager/history.rs` 不只是保存消息。它维护这些规则：

- tool call 必须有对应 output。
- tool output 必须能找到对应 call。
- 不支持图片的模型会去掉图片输入。
- 工具输出会按策略截断。
- 会估算 token，记录服务端 token 信息。
- 可以删除最近若干 user turn，并修复 call/output 对。
- 有 `reference_context_item`，用于 compaction 后重新注入初始上下文。

这是 Codex 能长时间工作的关键。上下文不是 append-only 文本，而是有结构和约束的运行记录。

### 3.3 Compaction 是运行机制，不是临时总结

`compact.rs` 把上下文压缩做成一个正式任务：

- pre-turn 可以压缩。
- mid-turn 遇到 token 压力也可以压缩。
- 压缩时会生成 `ContextCompaction` item。
- 失败时有重试和上下文裁剪。
- 压缩后会按策略重新注入初始上下文。
- 保留最近用户输入，避免 summary 覆盖最新目标。
- 记录压缩前后 token 和耗时。

这点值得直接学习。我们现在有 token 统计和 `/ctx`，但没有真正的历史压缩机制。

### 3.4 工具执行有统一编排层

`tools/orchestrator.rs` 把工具运行拆成固定步骤：

1. 判断是否需要审批。
2. 选择 sandbox。
3. 执行第一次尝试。
4. 如果被 sandbox 拒绝，按策略请求升级。
5. 在批准后重试。
6. 处理网络审批。

`exec_policy.rs` 再把命令批准规则、prefix rule、危险命令判断、网络规则、持久化规则放到一个地方。这样 UI 和 runtime 都不需要自己猜“这个命令能不能跑”。

我们已有 `guard` 的 pre/post 检查和 resume state，这是正确方向；但它更像 agent loop 的附属逻辑，还没有独立的工具运行策略层。

### 3.5 Unified exec 按进程生命周期处理 shell

Codex 的 shell 不是一次性 `exec.Command`：

- `unified_exec/process_manager.rs` 分配 process id。
- 支持 PTY 和非 TTY。
- 支持后台进程保存和后续 `write_stdin`。
- 支持 stdout/stderr 流式事件。
- 支持取消。
- 支持网络拒绝时终止进程。
- 有 LRU 清理。
- 进程结束有统一事件。
- `head_tail_buffer.rs` 保留输出开头和结尾，中间截断。

我们当前 `tools/builtin/shell_runner.go` 也有 timeout、流式 chunk、输出上限和 cwd/env 处理，但它是一次命令一次响应。`limitedBuffer` 只保留前缀，长输出末尾的错误容易丢。

### 3.6 UI 协议是 typed event，不是文本约定

Codex 的 `app-server-protocol` 定义了 `ThreadItem`、`TurnStatus`、`ItemStarted`、`ItemCompleted`、`CommandExecutionOutputDelta`、`TurnDiffUpdated`、approval request 等结构。`event_mapping.rs` 把 core event 映射成 UI notification。

这让 TUI 能精确显示：

- assistant message delta
- reasoning delta
- plan delta
- command begin/output/end
- patch begin/update/end
- approval request
- context compaction
- turn diff
- subagent 状态

我们的 `agent.Event` 已经有 `tool_start`、`tool_done`、`tool_output`、`subtask_start`、`subtask_done`，console 也有 preview sink。但事件类型还偏少，UI 主要靠摘要和文本 tail，不能还原完整 turn item。

### 3.7 Pending input 和 interrupt 是核心语义

`session/input_queue.rs` 说明 Codex 支持运行中的输入队列、mailbox、下一 turn 输入和当前 turn 注入。`turn.rs` 在一次采样结束后会检查 pending input，必要时继续处理。

这对长任务很重要。用户在 agent 运行期间补充信息时，系统不应只把它当成下一次独立任务。它可能是当前任务的约束变更。

我们的 console/channel runtime 有任务队列和 topic，但是 agent loop 本身没有 turn 内 pending input 语义。

### 3.8 Diff 是 turn 的产物

`turn_diff_tracker.rs` 不重新扫工作区，而是从 apply_patch 的精确 delta 维护当前 turn 的 unified diff。UI 可以在 turn 中显示当前改了什么。

我们现在更多依赖最终文字、日志和 git diff。对于编程 agent，turn 级 diff 是一个很实用的反馈面。

### 3.9 Resume、fork、rollback 按语义边界做

`thread_rollout_truncation.rs` 支持按 user turn 和 fork turn 截断，且会写入 rollback marker。TUI 的 `session_lifecycle.rs` 也有 resume、fork、subagent thread 选择和 replay。

这说明 Codex 把“对话历史”当成可操作的数据，而不是只能追加的日志。

## 4) 我们当前实现的优点

我们的实现并不是缺少所有这些能力。已有几个方向是正确的：

- `agent/engine_loop.go` 简洁，容易测试，工具批次支持并发执行，并且结果按原始顺序写回。
- `guardPreCheck` 能暂停并保存 resume state，`engine_resume.go` 能从批准请求恢复。
- `tools/builtin/bash.go` 和 `shell_runner.go` 已有 cwd、timeout、env、deny path/token、流式输出和输出截断。
- `internal/channelruntime/taskruntime/runtime.go` 把 runtime 的 LLM route、registry、skills、prompt、memory、image tool、guard 串起来，复用度不错。
- `internal/chatcommands/runtime.go` 已经把 `/models`、`/skills`、`/ctx`、`/workspace` 注册成 runtime 共用命令。
- `cmd/mistermorph/consolecmd/stream_events.go` 已经能把工具输出转成 console preview，并做 tail 限制和语义观察。
- `internal/llmstats` 和 `internal/topiccontext` 已经记录 token、cost 和上下文窗口信息。

这些能力可以保留，不需要为了学习 Codex 而整体重写。

## 5) 主要差距

### 5.1 缺少结构化 History Manager

现在 `agent.Run` 构造 `[]llm.Message`，`runLoop` 直接 append assistant/tool/user message。`agent.Context` 记录 steps 和 metrics，但它不是 prompt history 的所有者。

结果是：

- 没有统一地方维护 tool call/output 配对不变量。
- 没有统一地方做历史归一化。
- 没有统一地方做 token-aware 截断。
- resume state 保存的是消息快照，不是更细的 turn item。
- UI 不能从同一份结构化 history 还原完整任务过程。

### 5.2 token budget 是事后停止，不是上下文管理

`agent.Context.AddUsage` 会累加 token，`runLoop` 超过 `MaxTokenBudget` 后 break 并 force conclusion。这个机制能防止无限烧 token，但不能解决上下文过长。

Codex 的做法是：在模型调用前和调用过程中都可以压缩历史，并把压缩作为事件记录。

### 5.3 shell 输出截断策略偏弱

我们的 `limitedBuffer` 保留前缀。长命令失败时，最有价值的信息常在结尾，例如 test failure summary、panic、linker error。

Codex 的 `HeadTailBuffer` 更适合编程任务。最小改动不是引入完整 unified exec，而是先把 shell 输出缓存改为 head/tail。

### 5.4 工具运行策略分散

我们有：

- tool registry
- bash 自己的限制
- guard pre/post
- approval resume
- channel/console 的事件展示

这些都可用，但没有一个类似 Codex `ToolOrchestrator` 的统一入口来表达“审批、权限、执行、重试、事件、结果写回”的顺序。

短期不必照搬 sandbox 体系，但可以先把工具执行生命周期显式化。

### 5.5 事件模型不够表达 turn

`agent.Event` 能表达工具和子任务，但还不能表达：

- assistant item started/completed
- plan delta
- command begin/end 的结构化字段
- file change item
- approval request item
- compaction item
- turn diff
- turn completed/interrupted/failed

这限制了 Console Web UI。UI 只能从任务列表、audit、preview 文本中拼状态，无法直接订阅一条完整 turn event stream。

### 5.6 运行中用户输入没有进入 agent turn

channel 和 console 有任务队列，但 agent loop 没有 pending input queue。用户在任务中途发来的信息，通常会变成另一个任务或下一次上下文，而不是当前 turn 的 steer。

这对长期目标任务影响很大，因为用户经常会补充“别做 X”“改用 Y”“刚才那个测试不用管”。

## 6) 值得学习的改进

### P0：先做 History Manager

先在 `agent/` 内增加一个小的 history manager，不改外部 UI。

最小形态：

- 定义 `HistoryItem`：user message、assistant message、tool call、tool output、context injection、compaction。
- 提供 `Append...` 方法。
- 提供 `ForPrompt(modelCaps)`，输出现有 `[]llm.Message`。
- 在 append 时保证 tool call/output 配对。
- 对工具输出统一走截断策略。
- 保留 `Context.Steps`，但不再让它承担历史职责。

这一步收益最大，因为后面的 compaction、event stream、resume 都依赖它。

### P1：把 shell 输出改成 head/tail

不需要先实现完整 unified exec。

先做：

- 把 `limitedBuffer` 替换成 head/tail buffer。
- observation 里标明 omitted bytes。
- stdout/stderr 分别保留 head/tail。
- 保持现有 `bash` 参数、timeout、流式输出不变。

这能立刻提高测试失败分析质量。

### P1：把 turn 事件补齐

在现有 `agent.Event` 上扩展，不需要先引入 app-server protocol：

- `turn_start`
- `assistant_delta`
- `plan_update`
- `command_start`
- `command_output`
- `command_done`
- `file_change`
- `approval_required`
- `compaction_start`
- `compaction_done`
- `turn_done`

Console 可以继续保留当前 preview sink，但 audit 和任务流应逐步读取结构化事件。

### P1：实现简单 auto-compaction

等 History Manager 有了以后，再做最小 compaction：

- 当估算输入 token 超过窗口阈值时触发。
- summary 由当前主模型或指定模型生成。
- 保留最近 N 条 user turn。
- 保留当前未完成的 tool call/output 对。
- 把 compaction 作为 history item 和 event。

不要先追求 Codex 那套完整 remote compaction、analytics 和多版本策略。

### P2：工具执行生命周期集中化

引入一个轻量的 `ToolRunner` 或 `ToolOrchestrator`，但要避免只包一层名字。它必须真的拥有顺序：

- tool lookup
- repeat limit
- guard pre-check
- approval pause
- execution
- guard post-redact
- lifecycle events
- result normalization
- history append

当前这些逻辑在 `engine_loop.go` 里能工作，但后续要支持更多工具状态和 UI event，会越来越挤。

### P2：支持 pending input

先在 console chat topic 内做，不必所有 runtime 一次完成：

- 当前 turn 运行时，用户新输入进入 pending queue。
- 模型一轮输出或工具批次结束后检查 queue。
- 如果有输入，把它作为 steer 注入当前 turn。
- UI 明确显示 queued input。

这个能力比多开任务更符合“长期目标任务”的实际使用方式。

### P2：turn diff

如果我们继续以 shell 和 write_file/apply_patch 混合修改文件，turn diff 可以先做简化版：

- 对明确的 `write_file` 和未来 `apply_patch` 工具记录 delta。
- shell 修改无法精确追踪时标记 diff unknown。
- UI 显示“本 turn 已知改动”。

不要为了 diff 去扫全仓库或强行解析所有 shell 命令。

## 7) 不建议照搬的部分

不要为了“像 Codex”而直接复制这些复杂度：

- 不需要改写成 Rust。
- 不需要马上引入 app-server v2 协议。
- 不需要马上实现完整 plugin marketplace。
- 不需要先做跨平台 sandbox。
- 不需要完整照搬 Codex 的 TUI。
- 不需要让每个 runtime 都支持 fork、rollback、multi-agent navigation。

先做能改善长期编程任务稳定性的最小结构：history、head/tail、compaction、event。

## 8) 建议的实施顺序

### 第一阶段：History Manager

目标：

- agent loop 不再直接维护原始 `[]llm.Message`。
- 所有 prompt message 都从 history manager 生成。
- tool call/output 不变量有测试。

验收：

- 现有 agent 测试通过。
- 新增测试覆盖 tool output 缺失、call 缺失、空消息过滤、历史转 prompt。

### 第二阶段：shell head/tail

目标：

- 长输出保留开头和结尾。
- observation 明确说明截断。

验收：

- bash/powershell 相关测试通过。
- 新增测试覆盖 stdout/stderr 超限时保留 tail。

### 第三阶段：turn event

目标：

- agent 对外发出 turn 级结构化事件。
- console 可以继续渲染现有 preview，也能把结构化事件写进 audit。

验收：

- console audit 能看到命令开始、输出、结束、错误。
- 事件 JSON 有稳定字段测试。

### 第四阶段：auto-compaction

目标：

- 超过阈值时自动压缩历史。
- 压缩不破坏 tool call/output 配对。
- `/ctx` 可以展示压缩后的输入占用。

验收：

- 构造长历史测试，确认 compaction 后能继续调用工具。
- 压缩失败时不会破坏原 history。

### 第五阶段：pending input

目标：

- console topic 中运行任务可接收用户追加输入。
- 新输入可以进入当前 turn。

验收：

- 运行中输入被记录为 queued。
- 当前工具批次结束后，下一次模型请求能看到 queued 输入。

## 9) 当前最小结论

Codex CLI 的优秀点不是某个单独模块，而是它把长任务视为一个有状态系统：

- session 管配置和服务。
- turn 管一次任务生命周期。
- history 管上下文不变量。
- compaction 管上下文压力。
- tool orchestrator 管权限和执行。
- unified exec 管进程生命周期。
- typed event 管 UI。
- rollout/truncation 管 resume/fork/rollback。

我们当前实现胜在简单，很多 channel/runtime 能力已经可用。下一步不应大改外壳，而应先补一个结构化 history manager，再把 shell 输出和 event stream 做扎实。这样能保留现有代码的简单性，同时解决长期编程任务最核心的失败点。
