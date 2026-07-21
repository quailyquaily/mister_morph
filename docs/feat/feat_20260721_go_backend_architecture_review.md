---
date: 2026-07-21
title: Go 后端架构审查
status: completed
---

# Go 后端架构审查

## 1. 审查范围

本次审查基于提交 `6c8a631e`，只检查 Go 后端及其架构文档，没有修改运行逻辑。第二轮补充检查了 package 归属、装配重复、接口契约、资源所有权和代码风格。

重点阅读了这些文档：

- `docs/arch.md`
- `docs/runtime_layers.md`
- `docs/integration.md`
- `docs/security.md`
- `docs/configuration.md`
- `docs/feat/feat_20260315_task_persistence.md`
- `docs/feat/feat_20260320_streaming_breakdown.md`
- `docs/feat/feat_20260418_console_config_runtime_snapshots.md`
- `docs/feat/feat_20260511_cron_tasks.md`
- `docs/feat/feat_20260614_unified_domain_journal.md`
- `docs/feat/feat_20260622_human_approval_flow.md`
- `docs/feat/feat_20260715_context_checkpoint_compaction.md`
- `docs/feat/feat_20260721_console_composer_file_references.md`

代码审查覆盖：

- `agent/`、`guard/`
- `llm/`、`providers/`、`internal/llmutil/`
- `tools/`、`skills/`
- `internal/channelruntime/`
- `internal/daemonruntime/`、`internal/domainjournal/`
- `cmd/mistermorph/`
- `integration/`

本文不评价前端视觉，也不建议重写整个后端。

## 2. 结论

当前后端的主方向是合理的：channel adapter、`runtimecore`、`taskruntime`、Agent Engine、LLM client 和 provider 已经有清楚的基本分层；journal 作为事实源、projection 作为可重建视图的方向也正确。

现在的问题主要不是“缺少更多抽象”，而是几条已经写进文档的边界没有贯穿实现：

1. 审批应只授权一个确定 action，但实现使用了批次级布尔状态。
2. journal 应是唯一事实提交点，但部分 API 无法返回写入错误，另一些 API 又会在事实已提交后报告整个操作失败。
3. runtime generation 应是一次任务内不变的配置快照，但部分路径、store、approval 和 logger 仍读取进程全局状态。
4. 有副作用的工具需要明确执行顺序和所有者，但当前所有多工具调用都会并发执行。

这些问题可以在现有架构内修复，不需要数据库、分布式队列、微服务或依赖注入框架。

第二轮代码组织审查也确认：当前维护成本不是由“小 package 太多”或“文件不够抽象”造成的。真正的问题是同一条安全规则、同一份配置和同一次运行准备存在多个实现。重复已经导致缓存路径校验、Bash rewrite、Lark 联系人、Todo prompt、channel 默认端口和 Integration feature 开关出现差异。修复重点应是让这些稳定语义只有一个所有者，同时删除只改名或只转调的包装。

### 2.1 严重度摘要

| ID | 严重度 | 问题 | 主要后果 |
| --- | --- | --- | --- |
| A-01 | Critical | 一次审批会放行同批次的其他高风险工具 | 未审批命令可以执行；恢复时还会丢失前置工具调用 |
| A-02 | High | durable task/journal/projection 没有一致的提交语义 | 磁盘错误被吞掉，或已提交操作被报告为失败 |
| A-03 | High | runtime snapshot 和 generation 没有贯穿所有状态路径 | 两个 runtime 可互相污染；reload 后任务混用新旧配置 |
| A-04 | High | final output 没有唯一、完整的 Guard 出口 | 结构化输出和强制收尾可以绕过脱敏 |
| A-05 | High | 显式配置读取失败后仍可能按默认权限运行 | 原 guard、工具限制、profile 和目录配置静默失效 |
| A-06 | High | 所有 tool-call 批次都并发执行 | shell、写文件、通知等副作用顺序不确定 |
| A-07 | High | `cron.yaml` 缺少读改写锁，也缺少唯一 scheduler 所有权 | 并发写会丢任务；多个 runtime 会重复执行任务 |
| A-08 | High | approval 的过期和 generation 生命周期不完整 | pending task 永久残留；reload 后审批可能无法处理 |
| A-09 | Medium | weighted route 选定实际候选的时机太晚 | 图片能力、上下文窗口和 TaskInfo model 可能与实际模型不符 |
| A-10 | Medium | Console 把流式 plan/activity 写入 durable store | journal 和 projection 出现高频写放大 |
| A-11 | Medium | conversation worker 永不回收，worker 边界不隔离 panic | goroutine 随历史会话增长；handler panic 可结束进程 |
| A-12 | Medium | Integration 与 CLI 的能力和资源生命周期不一致 | MCP、图片工具、动态工具缺失；宿主 logger 被覆盖 |
| A-13 | Medium | Console 上传缓存和 HTTP client/server 生命周期不完整 | cache 无界增长；部分上传失败会留 orphan 文件；请求可长期占用连接 |
| A-14 | Low | package 边界和文档已出现漂移 | 修改范围变大，重复 schema 容易分叉 |

## 3. 当前实现的实际结构

```text
CLI / Integration / Console HTTP
        |
        v
channel adapter / Console local runtime
        |
        v
runtimecore: queue、task lifecycle、conversation serialization
        |
        v
taskruntime: route、prompt、skills、tools、memory、context checkpoint
        |
        v
Agent Engine -> Guard -> Tool Registry
        |
        v
llm.Client -> route/fallback/stats -> provider

durable state:
domain journal -> in-memory projection -> projection snapshot / HTTP view
```

这张结构图与 `docs/runtime_layers.md` 大体一致。问题集中在横跨多层的状态：approval、模型身份、runtime paths、task persistence 和资源生命周期。

## 4. 详细发现

### A-01：审批授权范围错误

严重度：Critical

#### 证据

- `agent/engine_loop.go:399-456` 先串行预检一个 LLM 返回的全部 tool calls。
- 遇到 `require_approval` 时，resume state 只保存当前调用和它后面的调用：`agent/engine_loop.go:765-770`。
- `agent/engine_resume.go:67-78` 只校验当前 pending 调用的 action hash。
- 恢复时却把 `approvedPendingTool` 设成批次级 `true`：`agent/engine_resume.go:122-150`。
- 后续任何同样要求审批的调用都会因为这个布尔值被直接允许：`agent/engine_loop.go:742-745`。

#### 可发生的行为

对于批次 `[bash A, bash B]`：

1. A 生成审批请求。
2. 用户只批准 A。
3. 恢复后 A 和 B 都会执行。
4. B 没有独立 approval id，也没有自己的 action-hash 授权。

对于批次 `[read_file, bash A]`：

1. phase 1 尚未执行 `read_file` 就在 A 处暂停。
2. resume state 没保存 A 前面的 `read_file`。
3. 恢复后 `read_file` 永久丢失。
4. assistant message 仍包含原批次的全部 tool calls，下一次 provider 请求可能收到不完整的 tool-call/result 对。

这违反 `docs/feat/feat_20260622_human_approval_flow.md` 的两个约束：审批只授权一个确定 action hash，approve 只能生效一次。

#### 最小改法

- 把批次级 `approvedPendingTool bool` 改成当前待审批调用的精确身份，例如 action hash 或稳定 call id。
- 授权只消费一次；当前调用恢复执行后立即清除。
- 后续高风险调用重新执行 Guard，并生成独立审批。
- approval 路径按原顺序处理工具。暂停前已执行成功的工具必须先写入 step 和 tool result，再保存当前位置。

不需要引入工作流引擎。

#### 必须补的测试

- `[approval tool, approval tool]` 需要两次审批。
- `[normal tool, approval tool]` 恢复后前置 result 不丢失。
- `[approval tool, normal tool]` 保持原顺序。
- 批准 A 的 action hash 不能执行参数不同的 B。

### A-02：durable state 没有一致的提交语义

严重度：High

这个问题由三个相互关联的实现组成。

#### 1. lifecycle API 无法返回错误

- `internal/daemonruntime/store.go:29-35` 的 `TaskUpdater.Update` 和 `TaskWriter.Upsert` 没有 error 返回值。
- `internal/daemonruntime/console_store.go:182-184,222-224` 丢弃 `UpsertWithTrigger`、`UpdateWithTrigger` 的错误。
- `internal/daemonruntime/file_store.go:99-101,129-131` 同样丢弃错误。
- `internal/channelruntime/core/taskinfo.go:44-93` 的 running、failed、done 更新全部走无 error API。

磁盘满、权限变化或 journal 损坏时，任务仍可执行并向 UI 发布 `running`、`done`，但事实源没有相应事件。重启 replay 后，用户看到的历史会倒退或变成另一种状态。

#### 2. journal event 已写入后，index 错误仍让 Append 返回失败

- `internal/domainjournal/journal.go:116-133` 先调用 `appendLineLocked` 写事实事件，再写 index。
- `internal/domainjournal/journal.go:450-482` 已经完成 event write 和可选 sync。
- 随后的 `appendIndexRecordsLocked` 失败时，`Append` 返回空 cursor 和 error。

index 是派生数据，不是第二份事实。此时调用方收到“失败”，但重启 replay 又会看到已经写入的事件。重试可能产生重复事件。

#### 3. projection snapshot 失败会把已提交操作报告为失败

- `ConsoleFileStore.CreateTopic`、`UpsertWithTrigger`、`UpdateWithTrigger`、`DeleteTopic`、`SetTopicTitle` 都是“append journal -> 修改内存 -> 保存 snapshot”：`internal/daemonruntime/console_store.go:156-260,384-471`。
- `FileTaskStore` 使用同一顺序：`internal/daemonruntime/file_store.go:103-164`。
- snapshot 只是可重建 projection，但 snapshot 写失败会作为整个 mutation 的 error 返回。
- `TopicDeleter.DeleteTopic(id) bool` 又把 not found 和 I/O error 合并成一个 `false`：`internal/daemonruntime/store.go:70-72`。

这与 `docs/feat/feat_20260614_unified_domain_journal.md` 的定义冲突：journal 是事实源，snapshot 是可删除、可重建的缓存。

#### 最小改法

- 规定唯一 commit point：事实 event 成功 append。
- authoritative mutation API 必须返回 error；persistent store 不再使用 void updater。
- index 和 snapshot 失败进入明确的 degraded 状态并记录错误，允许后续重建或重试，不能把已提交事实报告为“整个操作未发生”。
- `DeleteTopic` 改成能区分 `(found, error)` 的接口。
- 一个业务动作若现在要 append 两个 event，应改成一个能完整 replay 的领域 event，或增加幂等键；不要假装两次 append 是事务。

这仍然是文件 journal，不需要换数据库。

#### 必须补的测试

- journal 成功、index 失败时，调用结果和 replay 语义一致。
- journal 成功、snapshot 失败时，重试不会创建 ghost topic 或重复 task。
- lifecycle journal 写失败时，不得先向 UI 发布成功状态。
- delete 的 not found 与持久化错误分别映射为 404 和 5xx。

### A-03：runtime snapshot 和 generation 不是端到端边界

严重度：High

#### 证据

Integration 确实创建了独立 Viper snapshot：`integration/runtime_snapshot_loader.go:24-121`。但状态路径仍有多处回到进程全局 Viper：

- persona：`integration/runtime.go:287-296`、`internal/promptprofile/identity.go:22-53`
- contacts/workspace：四个 channel 的 `runtime.go`
- memory/journal：`internal/channelruntime/core/memory.go:37-49`
- task store：`internal/daemonruntime/file_store.go:40-64`
- `statepaths` 本身：`internal/statepaths/statepaths.go:21-119`

Console generation 也存在相同问题：

- upload 使用 `RoutesOptions.AgentSettingsReader`，但 download、preview、diagnostics、state、memory、todo、audit 和 log 路径仍调用全局 `viper`/`statepaths`：`internal/daemonruntime/server.go:253-334,893-935,1891-1902,2267-2303`。
- `applyPreparedGeneration` 在切换 generation 之前先修改 process-owned store 配置，之后分三个临界区切换 generation、workspace store 和 handler：`cmd/mistermorph/consolecmd/local_runtime.go:607-636`。
- pending approval 保留了旧 generation 引用，但 approve/deny 使用当前 generation 的 Guard：`cmd/mistermorph/consolecmd/local_runtime.go:1314-1459,1543-1562`。
- Integration 和 channel runtime 会调用 `slog.SetDefault`，修改宿主进程的全局 logger。

#### 后果

- 同一宿主内两个 `integration.Runtime` 使用不同 `file_state_dir` 时，persona、memory、contacts、workspace 和 task 仍可能互相污染。
- Console reload `file_cache_dir` 后，upload 可能写入新目录，而 preview 仍从旧的全局目录读取。
- 任务可绑定旧模型、旧 tools、旧 memory，但把 task state 或附件写进新路径。
- reload 修改 Guard 或 approval store 路径后，旧 pending task 可能永远无法 approve/deny，并一直持有旧 generation。

#### 最小改法

- 在 composition root 解析一次小型 `RuntimePaths` 值，包含 state、cache、journal、memory、contacts、workspace、tasks 路径；把它作为不可变值传入现有 dependency/options。
- Console admission 一次 capture 完整 runtime view：generation、task store view、workspace store 和 route settings 必须属于同一版本。
- approval 必须先找到 pending job，再使用 `job.Generation` 中的 Guard 和 store。
- 如果 persistence root 无法安全热切换，最简单的做法是把它标为 boot-only，reload 时明确拒绝变化。
- reusable package 只使用传入 logger，不调用 `slog.SetDefault`。

不需要增加通用 DI 容器。

#### 必须补的测试

- 同一进程创建两个 state dir 不同的 Integration runtime，并故意修改全局 Viper，确认状态完全隔离。
- Console reload cache/state dir 时，upload、preview、task store 和 memory 使用同一代路径。
- 旧 generation 的 pending approval 在 reload 后仍可批准、拒绝和释放资源。

### A-04：final output 没有唯一的 Guard 出口

严重度：High

#### 证据

- 正常 final 只在 `Final.Output` 是 string 时执行 `OutputPublish`：`agent/engine_loop.go:333-344`。
- `Final.Output` 的类型是 `any`：`agent/types.go:69-75`。对象和数组中的字符串不会脱敏。
- `forceConclusion` 直接返回 final，没有经过同一个 Guard：`agent/engine_helpers.go:61-101`。
- `Context.RawFinalAnswer` 在脱敏前保存：`agent/engine_loop.go:278`、`agent/engine_helpers.go:95`。
- `guard.emitAudit` 丢弃 `AuditSink.Emit` error：`guard/guard.go:296-324`；Engine 也忽略 `Evaluate` error。

这与 `docs/feat/feat_20260201_guard.md` 的约束冲突：final output 应在存储、展示和发布前脱敏，每个 Guard decision 应有 audit record。

#### 最小改法

- 建立一个真正的 final egress 函数，正常 final、force conclusion 和 fallback 都调用它。
- 对 JSON object/array 的字符串叶子递归应用脱敏，保持原结构。
- `RawFinalAnswer` 要么不保留，要么保存脱敏后的等价结构；不能通过 public Context 返回未处理内容。
- 高风险 precheck、approval 和 output publish 的 audit 写失败不能静默。至少要 fail closed 或返回明确错误。

#### 必须补的测试

- string、object、array 三种 output。
- force-conclusion 和 fallback 路径。
- RawFinalAnswer 不包含原始 secret。
- AuditSink 失败时调用方能看到错误。

### A-05：显式配置读取失败会按默认值继续

严重度：High

#### 证据

- `cmd/mistermorph/root.go:138-163` 的 `initConfig` 读取失败后只打印错误并返回。
- runtime file preflight 只覆盖 `run`、Telegram、Slack、LINE、Lark：`cmd/mistermorph/runtime_preflight.go:24-33`。
- 显式配置文件不存在不一定被视为 broken。
- 默认值会开启 `write_file`、`spawn`，并在 Unix-like 系统开启 `bash`：`internal/configdefaults/defaults.go:175-190`。
- Console repair mode 允许 defaults-only snapshot 是文档明确的特殊情况，不应扩散到其他命令。

#### 后果

当 `--config` 路径写错或 YAML 损坏时，原配置中的 Guard 规则、工具限制、LLM profile 和 state dir 可能全部失效，而进程仍尝试运行。

#### 最小改法

- 让配置加载产生结构化 error，并在所有消费配置的命令进入业务逻辑前 fail fast。
- 只有 `console serve` 的 repair mode 保留明确、可见的 defaults-only 行为。
- 显式路径不存在必须是 error。

#### 必须补的测试

- 显式配置不存在。
- `chat` 使用损坏配置。
- `run` 使用损坏配置。
- Console repair mode 仍能启动，但明确标记 degraded/config error。

### A-06：多工具并发没有副作用契约

严重度：High

#### 证据

- `agent/engine_loop.go:477-511` 对所有未跳过的 tool call 使用 `errgroup.Go`。
- `tools.Tool` 只有 name、description、schema、execute，没有并发安全或副作用声明：`tools/tool.go:5-10`。
- `write_file`、`bash`、`powershell`、`todo_update`、`contacts_send` 都可能修改状态或依赖顺序。
- `StopAfterSuccess` 只能取消已经并行启动的其他工具，不能撤销已发生的副作用。

#### 后果

- 两次写同一个文件时，结果由调度时序决定。
- shell B 依赖 shell A 的输出时可能先执行。
- 通知、联系人修改和 TODO 更新的顺序与模型给出的顺序不同。

#### 最小改法

- 未声明的工具默认串行执行。
- 增加一个可选的并发安全能力，例如 `ParallelSafe() bool`；只有整个批次都明确为只读且并发安全时才并发。
- approval 批次始终按顺序执行。

#### 必须补的测试

- 两个修改共享状态的工具保持 provider 顺序。
- 只读、明确 parallel-safe 的工具仍可并发。
- `StopAfterSuccess` 不会让另一个有副作用工具提前启动。

### A-07：cron 缺少文件事务锁和唯一执行者

严重度：High

#### 并发写丢失

- `internal/cron/store.go:111-194` 的 Add/Delete 是独立 `Read -> mutate -> Write`。
- `internal/fsstore/atomic.go:23-60` 只保证单次 rename 原子，不保护整个读改写过程。
- 多个 conversation 可以同时调用 `todo_update`。

两个调用都读取旧文件后，最后一次 rename 会覆盖另一次修改。

#### 多 runtime 重复执行

- `cron.enabled` 默认开启：`internal/configdefaults/defaults.go`。
- Telegram、Slack、Console 可以分别启动 scheduler。
- `internal/channelruntime/awareness/cron.go:70-115,154-220` 的 `inFlight` 只在当前 runner 内有效。
- `docs/feat/feat_20260511_cron_tasks.md` 也明确说明第一版只做进程内去重，并假定一个 runtime 负责同一个文件。

当多个 runtime 共享 `file_state_dir` 时，同一 cron task 会被重复执行。通知、外部 API 和文件写入都会产生重复副作用。

#### 最小改法

- 用现有 `fsstore.WithLock` 包住完整的 `Read -> mutate -> Write`，Add/Delete 使用同一 lock file。
- 每个 cron path 只允许一个 scheduler 持有进程生命周期文件锁；未获得锁的 runtime 不执行 tick。

这是单机文件锁问题，不需要分布式调度系统。

#### 必须补的测试

- 两个 goroutine 并发 Add 不丢失。
- Add 与一次性任务 Delete 并发不丢失。
- 两个 scheduler 竞争同一路径时只有一个执行 task。

### A-08：approval 的过期和 generation 生命周期不完整

严重度：High

#### 过期不终结 task

- approval 固定五分钟过期：`guard/guard.go:131-148`。
- Console、Telegram、Slack 只保存 pending handle，没有 timer 或 sweeper。
- 列表和按钮校验会过滤过期记录，但 task 仍是 `pending`，内存 job 仍存在，approval store 仍可能保持 pending。

结果是过期任务从 UI 消失，却不会进入 canceled/expired 终态。

#### reload 使用了错误 generation

- pending job 会额外 acquire 它所属 generation。
- Console approve/deny 先使用 `currentApprovalGuard()`，之后才读取 pending job。
- reload 改变 Guard 开关或 approval store 路径后，新 Guard 查不到旧记录，旧 generation 引用无法释放。

#### 最小改法

- 注册 pending handle 时按 `ExpiresAt` 安排到期处理，或使用一个轻量 sweeper。
- 到期时原子地把 approval 标为 expired、删除 handle、把 task 标为 canceled，并写 audit。
- approve/deny 先读取 pending job，再使用 job generation 的 Guard。

#### 必须补的测试

- 短 TTL 到期后 task 进入终态、handle 被删除、generation ref 归零。
- reload 前创建的 pending approval 在 reload 后仍能处理。

### A-09：weighted route 在模型相关准备完成后才选实际候选

严重度：Medium

这里需要区分两件事：实际 provider 请求的 model 没有发错；`weightedRouteClient.Chat` 会按 run id 选择 candidate，并把 request model 改成该 candidate 的 model：`internal/llmutil/route_client.go:95-122`。

真正的问题是候选选得太晚：

- `ResolveRoute` 把 default 或第一个 candidate 作为展示候选：`internal/llmutil/routes.go:166-185,701-710`。
- `taskruntime` 在 `Chat` 前已经用展示候选决定 cache、runtime tool model、prompt patch 和 context window：`internal/channelruntime/taskruntime/runtime.go:341-491`。
- Telegram、Slack、LINE、Lark 和 Console 也用展示 model 决定是否把图片转成 multimodal parts。
- `internal/channelruntime/imageinput/message.go:24-28` 调用 `llm.ModelSupportsImageParts(model)`，但传入的不是本次实际 weighted candidate。
- TaskInfo 和 inspector 记录的 model 也可能只是展示候选。

当 candidates 的图片能力或 context window 不同，系统会静默丢弃本可发送的图片、把图片发给不支持的候选，或使用错误的 compaction 阈值。这违反文件引用文档中的“本次实际选择的模型支持图片输入”。

此外，`llm/model_capabilities.go` 只按模型名前缀猜图片能力，自定义 alias 和新模型会出现误判。

#### 最小改法

- 在 run preparation 开始时，使用 run id 选出 concrete candidate。
- message、context window、cache、prompt patch、TaskInfo 和 client 全部使用同一个 selected route。
- fallback 机制可以保留。
- 图片能力只增加 profile 级可选 `supports_image_parts` 覆盖；未配置时保留当前 heuristic。不要设计通用 capability negotiation 系统。
- 删除 `internal/channelruntime/telegram/runtime_task.go:91-95` 中每 task 创建但从未使用的 main client。

#### 必须补的测试

- 同一 run id 在 preparation 和 Chat 得到同一个 candidate。
- 文本 candidate 与视觉 candidate 混合时，图片 parts 与实际候选一致。
- context window 和 TaskInfo.Model 与实际候选一致。
- 自定义 model alias 可通过显式 capability override 启用图片。

### A-10：Console 把流式状态写入 durable store

严重度：Medium

#### 证据

- `cmd/mistermorph/consolecmd/local_runtime.go:1813-1858` 在每次 activity 和 plan 更新时调用 `storeProgress`。
- `storeProgress` 调用 `ConsoleFileStore.Update`。
- 每次 Update 都 append journal，并在 store mutex 内重写包含全部 task/topic/trigger 的 projection snapshot：`internal/daemonruntime/console_store.go:230-260,723-756`。
- `docs/feat/feat_20260320_streaming_breakdown.md` 明确要求 partial snapshot 不进入 `TaskInfo.Result` 和 durable store。

随着历史 task 增长，一次运行会产生“更新次数 × 全量 projection 大小”的写放大，还会让其他 task mutation 等待同一个锁。

#### 最小改法

- 实时 plan/activity 只发 `StreamHub`。
- pending、done、failed 时最多保存一次最终汇总。
- 先删除高频 durable update，再判断是否需要 snapshot 合并；不要先增加复杂缓存层。

### A-11：conversation worker 生命周期没有结束条件

严重度：Medium

#### 证据

- `internal/channelruntime/core/runner.go:87-103` 为每个新 conversation key 创建 channel、map entry 和 goroutine。
- `internal/channelruntime/worker/worker.go:12-34` 只在整个 runtime context 结束或 channel close 时退出。
- 当前没有删除 map entry 或 close 单个 worker 的路径。
- worker 直接调用 handler，没有 panic recovery。

长期运行的 Console 和 channel bot 会随历史 conversation 数量单调增加 goroutine。任一 handler、tool 或 provider wrapper 的未恢复 panic 还可能结束整个进程。

#### 最小改法

- worker 队列为空且空闲超时后，在锁内确认自身仍是当前 entry，再安全删除并退出。
- 不要 close 仍可能被 Enqueue 持有的 channel。
- 在 worker job 边界 recovery，记录 stack，并把对应 task 标为 failed。

### A-12：Integration 能力和资源所有权与 CLI 不一致

严重度：Medium

#### 证据

- CLI channel dependency 提供 `CreateImageClient`、`RegisterTriggeredStaticTools` 和 MCP tools。
- Integration snapshot 已读取 MCP 配置，但 `integration/channel_bots.go` 没有把 MCP、图片 client 和动态静态工具完整传给 Telegram/Slack。
- Integration bot 因此不支持 CLI 已有的图片工具、`$bash` 等单次工具触发和 MCP tools。
- `integration/runtime.go`、Telegram、Slack 会调用 `slog.SetDefault`，覆盖宿主 logger。
- CLI MCP 使用 `context.Background()` 初始化，resolver 不拥有明确的 `Close`。
- 根命令使用 `Execute()`，没有 `signal.NotifyContext` + `ExecuteContext`；SIGTERM 不会可靠运行 runtime cleanup。

#### 最小改法

- 补齐现有 dependency 字段，不增加第二套工具系统。
- Bot `Run` 使用自己的 run context 初始化 MCP，退出时 Close。
- Integration 只传 logger，不改进程全局 logger。
- CLI 根入口使用 signal context，并让 resolver 持有和关闭 MCP host。
- 暂未支持的能力先在 `docs/integration.md` 写出能力矩阵。

### A-13：上传缓存和 HTTP 生命周期不完整

严重度：Medium

#### 证据

- Console `/files/upload` 把文件写进 cache root，但没有调用现有 max-age/max-files/max-total-bytes cleanup。
- multipart 中第 N 个文件失败时，前 N-1 个已保存文件不会删除。
- Telegram cache 有 cleanup，Console cache 没有对应 owner。
- daemon 和 Console `http.Server` 主要只设置 `ReadHeaderTimeout`。
- `internal/daemonruntime/daemon_client.go` 的 download client 没有 timeout。

#### 最小改法

- Console 使用 `file_cache_dir/console`，复用现有有界 cleanup。
- 单个 multipart 请求失败时，只删除本次请求已经创建的文件。
- server 增加合理的 `IdleTimeout`；普通 body route 增加 body read deadline，stream/download route 单独放宽。
- download transport 至少设置 response-header timeout，并继续使用 request context 取消 body。

### A-14：package 边界和文档漂移

严重度：Low

#### 代码边界

- `internal/daemonruntime/server.go` 同时处理 task/topic、文件、workspace、settings、contacts、memory、todo/cron、logs、audit 和 observations，约 3757 行。
- `cmd/mistermorph/consolecmd/local_runtime.go` 同时承担 generation、queue、approval、streaming、topic、workspace 和 task execution，约 2544 行。
- Telegram、Slack 的 `runtime.go` 分别约 1730、1555 行。
- `agent/subtask.go` 反向依赖 `daemonruntime` 只为文本截断工具。
- `integration/run_task_options.go` 重复定义 task journal payload，因为 canonical codec 留在 `daemonruntime` 私有实现中。

行数本身不是错误。实际问题是 transport、domain type 和 state composition 混在一起，已经造成 global path、context、cron 和 Integration 能力分叉。

#### 最小改法

- 先把 HTTP route 按 domain 拆成同 package 的注册文件，不急着建立新 framework。
- 让中立 task domain 拥有 `TaskInfo`、status、error-aware store port 和 journal codec。
- channel 只抽取已经稳定的生命周期装配；trigger、平台 API 和回复渲染继续留在各自 adapter。
- 不要建立泛型化的“大一统 channel framework”。

#### 文档漂移

- `docs/arch.md` 仍引用已删除的 `cmd/mistermorph/daemoncmd/serve.go` 和旧 `TaskStore`。
- `docs/modes.md` 已说明 standalone `serve` 被删除，两处文档互相冲突。
- 项目说明中的“currently no `*_test.go` files”已经不符合仓库现状。

## 5. 代码组织与风格补充审查

### 5.1 判断标准

本节不按文件长度、package 数量或重复行数机械判断。一个抽象至少应满足下面一项：

- 拥有一条必须一致的业务或安全规则。
- 维护明确的不变量、默认值或资源生命周期。
- 为多个真实调用方提供同一语义，而不是只替函数改名。
- 作为稳定依赖方向的类型边界，避免上层 package 反向进入 transport package。

反过来，下面情况不值得抽象：

- 只调用另一个函数，没有增加校验、转换、默认值或错误语义。
- 只是两段代码长得相似，但平台协议、身份键或回复语义不同。
- 只有一个调用方，拆 package 后反而把同一个生命周期分散到两处。
- 为了统一两三个局部 helper 新建 `common`、`misc`、通用 builder 或策略框架。

### 5.2 补充发现摘要

| ID | 严重度 | 问题 | 主要后果 |
| --- | --- | --- | --- |
| O-01 | High | channel 文件缓存边界有三份实现，符号链接校验不一致 | Telegram 和 Lark 可从 cache 内的符号链接读取目录外文件 |
| O-02 | High | Guard 与 `url_fetch` 各自实现网络和脱敏规则 | 安全规则会随修改分叉，初始请求与重定向可能采用不同判断 |
| O-03 | Medium | 静态工具、Guard 和单次 Engine 各有三到四套装配 | 不同入口缺少字段、工具或 prompt policy |
| O-04 | Medium | channel 配置和依赖经过多层同形复制 | 默认值与能力字段已经分叉，新增字段容易漏传 |
| O-05 | Medium | Registry 静默覆盖同名工具，clone 又复制五次 | 工具实现取决于注册顺序，碰撞不可见 |
| O-06 | Medium | `llmutil.ConfigReader` 契约小于实现实际要求 | 合法 reader 仍会静默丢失 profile、route、headers 和图片配置 |
| O-07 | Medium | Agent settings、HTTP routes 和 channel loop 缺少清楚的状态所有者 | 初始化依赖 late binding，修改跨越大段闭包和多组锁 |
| O-08 | Medium | Integration 的 feature、构建失败回滚和 cleanup 契约不完整 | 显式全关功能失效；MCP/client 等资源可能泄漏 |
| O-09 | Low | 中立逻辑放在 transport package，另有多处薄包装 | 依赖方向反转，调用链变长但没有增加语义 |
| O-10 | Low | CLI flag、Cobra 初始化和测试 seam 使用多份或全局状态 | help 与实际值不同，多实例和并行测试互相影响 |

### O-01：文件缓存边界重复并出现安全差异

#### 证据

- `tools/telegram/cache_file.go:12-47` 和 `tools/lark/cache_file.go:12-47` 只比较清理后的词法路径。
- `tools/slack/cache_file.go:12-65` 在 containment check 前调用 `filepath.EvalSymlinks`。
- Slack 有符号链接逃逸测试：`tools/slack/cache_file_test.go:78-98`；Telegram 和 Lark 没有同类测试。
- `sanitizeFilename` 也分别存在于三个 channel tool package。

因此，cache 目录内若存在指向目录外文件的符号链接，Slack 会拒绝，Telegram 和 Lark 会跟随链接并发送目标文件。这里的重复不是普通代码风格问题，而是同一安全边界存在三个所有者。

#### 最小改法

- 在中立 package 中保留一个窄职责函数：解析 cache root 内的可读普通文件。
- 统一处理空 root、绝对路径、符号链接、目录、文件大小和 containment。
- 三个平台继续拥有各自的发送 API、参数 schema、reply 标识和错误文案。
- 把 Slack 的逃逸测试做成共享契约测试，并覆盖三个调用方。

不要为此建立通用 channel file tool。

### O-02：网络策略与脱敏规则有两个所有者

#### 证据

- Guard 在 `guard/guard.go:227-247,496-529` 实现 URL prefix 和 private host 判断。
- `url_fetch` 在 `tools/builtin/url_fetch.go:166-176,396-415,976-1008` 再实现一次，并在重定向阶段单独调用。
- 敏感键判断分别存在于 `guard/redact.go:214-232` 和 `tools/builtin/url_fetch.go:726-745`。
- `url_fetch` 又在 `tools/builtin/url_fetch.go:747-775` 维护独立的响应正文脱敏正则。

Guard precheck、真实 HTTP 请求和重定向必须遵守同一目标策略。敏感内容的定义也不应因为数据来自 final output 还是 HTTP response 而变化。

#### 最小改法

- 让现有 `guard.NetworkPolicy` 提供实际 URL 校验行为，Guard 和 `url_fetch` 调用同一个实现。
- 让响应正文复用 `guard.Redactor`；`url_fetch` 只保留 URL query 等确有不同输入结构的处理。
- 先用测试固定 scheme、prefix、localhost、private IP、redirect 和 proxy 语义。

不需要通用 policy engine。

### O-03：装配路径重复，已经产生能力漂移

#### 静态工具有三套配置

- CLI：`cmd/mistermorph/registry.go:20-241`
- Console：`cmd/mistermorph/consolecmd/runtime_support.go:25-255`
- Integration：`integration/runtime_snapshot.go:35-84`、`integration/runtime_snapshot_loader.go:80-129`、`integration/registry.go:14-116`

三处都读取 auth profile、复制字段、校验 profile、创建 profile store，再组装已有的 `toolsutil.StaticRegistryConfig`。

已经出现两处明确分叉：

- 只有 CLI 读取并传入 `tools.bash.rewrite.*`；Console 和 Integration 没有这些字段。
- CLI 和 Console 会给 `contacts_send` 传入 Lark 凭据；Integration snapshot 和 registry 只传到 LINE。

#### Guard 有三套构造

- `cmd/mistermorph/guard_config.go:20-110`
- `cmd/mistermorph/consolecmd/runtime_support.go:262-347`
- `integration/guard.go:13-71`

三处重复解析 redaction、network、audit、approval，重复创建目录、audit sink 和 approval store。

#### 单次 Engine 至少有四套准备逻辑

- `cmd/mistermorph/runcmd/run.go:105-377`
- `cmd/mistermorph/chatcmd/session.go:243-345,456-780`
- `integration/runtime.go:132-343`
- `internal/channelruntime/taskruntime/runtime.go:309-492`

它们都处理 route/client、plan/image tools、skills、prompt blocks、Guard 和 `agent.New`。Integration 在 `integration/runtime.go:296-299` 添加 persona、plan 和 model patch，却没有像其他三处一样调用 `AppendTodoWorkflowBlock`；默认注册 `todo_update` 时 prompt policy 因入口而异。

#### 最小改法

- 让现有 `toolsutil.StaticRegistryConfig` 成为唯一 typed value，增加一个真正负责读取、复制和校验的 `FromReader`；三个入口只补充 selected tools、triggers 和 awareness 这类入口语义。
- Guard 提供一个共享 snapshot parser 和 factory；Integration feature 开关仍留在 Integration。
- 以现有 `taskruntime` 为单次任务的 prepare/run 核心。Integration 若必须返回 Engine，就让 prepare result 明确拥有 Engine、选定 route 和 cleanup。
- 删除被替代的 snapshot 和逐字段 adapter，不要再增加第四套 builder。

### O-04：channel options 和 dependency bag 是同形复制

#### 证据

以 LINE 为例：

- `internal/channelopts/options.go` 先构造 channel config 和 input。
- `internal/channelruntime/line/run.go:24-51` 定义 `RunOptions`。
- `internal/channelruntime/line/runtime_options.go:10-68` 再定义完全同形的 `runtimeLoopOptions` 并逐字段复制。

Telegram、Slack 和 Lark 使用相同模式。task timeout、concurrency、bus capacity、queue、request timeout、memory limits 和阈值又在四个 runtime package 中重复补默认值。

默认值已经分叉：`internal/channelopts/options.go:28-31` 为四个平台分配 `8787`、`8788`、`8789`、`8790`，但四个 runtime 的空值 fallback 都是 `127.0.0.1:8787`。LINE 和 Lark 的空 `Hooks struct{}` 只在两层 options 之间搬运，没有任何读取者。

依赖也有同样问题：

- `depsutil.CommonDependencies` 是四个 channel 已在用的稳定边界。
- `integration/channel_bots.go:260-373` 又定义其子集 `runtimeSharedDependencies`，再逐字段复制回 Telegram 和 Slack 的 `CommonDependencies`。
- 这层复制漏掉 `CreateImageClient`、`RegisterTriggeredStaticTools` 和 awareness/MCP 相关能力。
- `internal/channelruntime/depsutil/depsutil.go:35-155` 先用 `Logger`、`CreateClient` 等包装函数做 nil check，再用 `LoggerFromCommon`、`CreateClientFromCommon` 做第二层转调。

#### 最小改法

- 每个平台保留一个公开、显式的 `RunOptions`，直接按值 normalize；删除同形 private struct。
- 默认值只在 config owner 或 normalize owner 出现一次。平台凭据、webhook、listen 和 API 行为继续留在各自 options。
- `sharedDependencies` 直接返回 `depsutil.CommonDependencies`；channel 特有 command handler 放在外层。
- 在 bootstrap 边界一次校验 required dependencies。内部直接调用字段；optional capability 就地判断 nil。
- 删除空 Hooks，等真实 hook contract 出现再增加。

不要引入 DI 容器或通用 channel config schema。

### O-05：Tool Registry 缺少明确的所有权契约

#### 证据

- `tools/registry.go:17-19` 的 `Register` 无条件覆盖同名 key，也没有拒绝 nil tool。
- MCP 和 Agent 内置工具都向同一个 registry 注册：`internal/mcphost/host.go:198-215`、`agent/engine_tools.go:46-60`。
- `agent.New` 在 `agent/engine.go:207-257` 直接修改调用方传入的 registry。
- 相同 clone 循环至少存在五份：`taskruntime.CloneRegistry`、Integration `cloneRegistry`、Chat `cloneToolRegistry`、Console `cloneConsoleRegistry` 和 Awareness `cloneRegistry`。

当 MCP、内置工具或宿主自定义工具同名时，最终实现由注册顺序决定，调用方得不到任何错误。

#### 最小改法

- `Register` 对 nil、空名称和重复名称返回 error。
- 只有明确替换场景使用命名清楚的 `Replace`。
- 给 `tools.Registry` 增加浅复制 `Clone()`，并规定它只复制注册表；tool instance 默认不可变。
- 有运行期状态的工具继续在 composition root 新建，不增加 `CloneableTool` 协议。
- `agent.New` 应 clone 后再注册 Engine 私有工具，或明确接收一个已归 Engine 所有的 registry。

### O-06：配置接口没有表达真实要求

#### `llmutil.ConfigReader`

`internal/llmutil/llmutil.go:22-24` 的接口只声明 `GetString`，但实现又通过动态断言寻找 `GetStringSlice`、`GetStringMapString`、`GetStringMap` 和 `UnmarshalKey`：`internal/llmutil/llmutil.go:550-584`、`internal/llmutil/routes.go:341-354`。

因此，一个完全符合公开接口的 reader 仍会静默丢失 headers、profiles、routes 和 image options。profile/route 反序列化错误也会在 `internal/llmutil/routes.go:308-312,329-333` 被当作“没有配置”。

最小改法是让配置入口一次解码并验证 `RuntimeValues`，返回 error。若仍保留 reader，接口必须直接声明真实依赖；不要继续增加一组 optional micro-interface。

#### Integration features

`integration/runtime.go:52-56` 用 `cfg.Features != (Features{})` 判断调用方是否提供 features。调用方从 `DefaultConfig()` 开始，把 PlanTool、Guard 和 Skills 全部显式设为 false 后，结构体恰好成为零值，`New` 会把它恢复成全 true。

最简单的契约是 `Config` 精确生效，想要默认值的调用方显式使用已有 `DefaultConfig()`。不需要把每个 bool 改成 pointer 或 optional 类型。

### O-07：状态所有权仍集中在巨型组合函数中

#### HTTP routes

- `internal/daemonruntime/server.go:532-559` 的 `RoutesOptions` 有二十多个 callback/interface 字段。
- `RegisterRoutes` 从 `internal/daemonruntime/server.go:618` 延续到约 2100 行，覆盖 task、approval、workspace、files、settings、state、memory、todo、contacts、logs 和 audit。
- 函数开头又把 options 字段逐个复制成局部变量。

最小改法是在同一个 `daemonruntime` package 内按 endpoint domain 拆注册文件。只把天然成组的 Task、Approval、Workspace 能力定义成小接口；不要每个 handler 建一个 interface，也不要增加 controller/repository framework。

#### Channel loop

Telegram 和 Slack 的主 loop 分别是千行级闭包。它们先启动 HTTP server，再用可变函数槽晚绑定 approval handler；同一函数还捕获 history、skills、identity、warning、pending approval 和 runner 状态。LINE/Lark 结构较小，但也采用相同的闭包式所有权。

先在各平台 package 内建立具体 runtime state struct，把 bootstrap、run job、inbound、outbound 和 serve 拆成方法，并在依赖完整构造后启动 server。平台 runtime state 是实际所有者，不需要再建一层通用 channel loop。

#### Console 与 Agent settings

- `consoleLocalRuntime` 在 `cmd/mistermorph/consolecmd/local_runtime.go:115-143` 同时拥有 generation、pending jobs、pending approvals、managed runtime、awareness、handler、runner、store 和多组锁。
- Agent settings 又分别实现在 `cmd/mistermorph/consolecmd/agent_settings.go` 与 `internal/daemonruntime/server_agent_settings.go`。
- 两套连接测试已有差异：Console 手工拼 LLM values，并在 `cmd/mistermorph/consolecmd/agent_settings.go:1657` 读取全局 Viper；daemon 版本从传入 reader 构造较完整的 `RuntimeValues`。

Agent settings 中真正共享的配置解析、env-managed 字段、连接测试和 model listing 应移到已有 `internal/agentsettings`。HTTP handler 和 YAML 写入继续留在原入口。Console runtime 只在状态与生命周期确实成组时抽 owner，例如 generation manager、pending approval registry 和 execution controller；不要抽一行转调 service。

### O-08：资源生命周期和全局状态没有随构造边界闭合

#### Integration 构建

- `integration.New(cfg) *Runtime` 不能返回错误；logger 初始化错误保存在 snapshot，到 `NewRunEngine` 才返回。
- `integration/runtime.go:229-236` 创建 MCP host 后，plan route/client 或 skills 初始化失败时只关闭 inspectors，没有关闭 MCP host。
- `PreparedRun.Cleanup` 只处理 inspectors 和 MCP，不明确拥有自己创建的 main、plan 和 image clients。
- inspect client wrapper 没有转发 `Close`，基于 `io.Closer` 的清理检测会失效。

构造函数应遵守一个简单规则：要么返回可用对象，要么返回 error。`New` 的签名若受兼容性约束，就在下一次允许 breaking change 时调整；当前版本至少应在发布 runtime 前完成可执行的验证。构建过程使用局部 `defer` 和 success flag 回滚已取得资源，成功后把幂等 cleanup 所有权交给 prepared result。无需通用 cleanup stack package。

#### 进程全局状态

- `integration/runtime.go:149` 和部分 channel runtime 调用 `slog.SetDefault`，可覆盖宿主 logger。
- `cmd/mistermorph/root.go:54` 每次构造 root command 都调用包级 `cobra.OnInitialize`；多次构造会累积 initializer，而且 initializer 无法返回配置错误。
- `integrationBaseClientBuilder` 和 `consoleRouteRegistrars` 是测试通过改写的 package 全局变量，限制并行测试。

reusable package 只使用传入 logger。配置加载放入当前 root command 的 `PersistentPreRunE` 并返回 error。测试 seam 放入 Runtime 或 server 的私有构造依赖，不需要公开插件机制。

### O-09：中立语义放在了 transport package

明确的例子包括：

- `agent/subtask.go` 只为 UTF-8 截断反向依赖 `internal/daemonruntime`。
- `internal/awarenessutil` 和 `internal/promptprofile` 为 `PokeInput` 依赖 `daemonruntime`。
- `tools/builtin/image_tools.go` 为 MIME normalization 依赖 `internal/channelruntime/imageinput`。
- `internal/configbootstrap` 为默认值依赖公开高层 package `integration`，而 `integration.ApplyViperDefaults` 只是调用 `internal/configdefaults.Apply`。
- `internal/channelruntime/worker` 只有 `runtimecore.ConversationRunner` 一个调用方，却把同一个 worker 生命周期拆到两个 package。

另有两处重复算法应回到已有 owner：

- `cmd/mistermorph/chatcmd/session.go:828-912` 明确复制 `tools/builtin/write_file.go:125-225` 的路径规则，但漏掉 base symlink、目录 mode 和部分 error 检查。
- `agent/parser.go:20-84` 自己做 JSON candidate 提取，而 `internal/jsonutil/jsonutil.go:16-59` 已有相近实现。candidate 提取可以共享，Agent response schema 校验仍应留在 Agent。

最小改法：

- 把 rune-safe 截断放入中立 `internal/textutil`；有意按 payload bytes 截断的代码不要混进来。
- 让 awareness domain 拥有 `PokeInput`，HTTP 只负责解析和适配。
- MIME/path MIME helper 放入中立 image/media package。
- internal caller 直接使用 `configdefaults.Apply`；`integration.ApplyViperDefaults` 可作为外部 API 保留。
- worker loop 合回 `runtimecore`，使 worker map、回收、panic boundary 和 enqueue 契约由同一个 owner 管理。
- 让 `write_file` 暴露唯一的 path resolution 行为；Chat 不再维护安全规则副本。
- 让 `jsonutil` 只负责 candidate 提取和修复，调用方负责自己的结构验证。

这些移动是为了修正所有权，不要建立 `common` 或 `misc` package。

### O-10：低成本风格问题

#### CLI 默认值有两份真相

- Viper 默认 `tool_repeat_limit=64`、`timeout=10m`：`internal/configdefaults/defaults.go:38-45`。
- run/chat flag help 写的是 repeat 3；chat timeout 又写 30m：`cmd/mistermorph/runcmd/run.go:430-435`、`cmd/mistermorph/chatcmd/chat.go:42-46`。
- `configutil.FlagOrViper*` 使用 `viper.IsSet`，而 Viper default 也会被视为 set，所以实际运行采用 64 和 10m，help 展示的默认值没有生效。
- run 的 provider、endpoint、model flag 还展示非空默认值，但代码只在 flag changed 时应用 override。

配置支持的 flag 应引用同一组默认常量；只作为 override 的 flag 默认应为空。不要生成一套通用 config schema。

#### 可以删除的薄包装

代表性例子：

- `depsutil` 的两层 `*FromCommon` helper 和只改名的 `FormatFinalOutput`。
- `agent/engine.go:138-159` 中只给 `WithEngineToolsConfig` 单字段赋值的别名 options。
- `tools/builtin/local_path_roots.go:13-15` 对 `pathroots.Resolve` 的改名。
- `internal/llmutil/routes.go:417-419` 只转调 `routeTargetForPurpose` 的 `resolveRoutePolicy`。
- `configbootstrap.loadDocument` 只转调 `LoadDocumentBytes`。
- LINE/Lark 的空 Hooks 和只给测试传空 image cache 的 prompt wrapper。

删除标准很简单：没有新增约束、转换、默认行为或错误语义，就让调用方直接调用真实函数。

#### 大文件与长参数

- `providers/uniai/client.go` 约 1747 行，包含 chat、image、usage、cache、debug 和 conversion。可在同 package 内按这些概念拆文件，不应增加 provider strategy/factory 层。
- `buildChatOptions` 有十个参数，其中大部分来自 `Client` 字段；改为 receiver method 能表达真实所有权，不需要 options wrapper。
- `runTelegramTask` 和 Console task admission 有十多个位置参数。只把已经稳定成组的 task request/dependencies 组成结构体，不要为每个三参数函数都创建 options。
- `agent/engine_loop.go` 是有顺序约束的状态机。审批和 tool batch 测试完备前，不应按行数拆散。
- Go error 文案应使用小写开头；目前 Agent、depsutil、taskruntime、cron 和少量 CLI 路径不一致。这是低优先级机械修正，不应与架构修复混在同一提交。

### 5.3 应保留和应复用的边界

#### 应保留

- `llm.Client` 与 `llm.ImageClient`：两个接口都小而稳定，不需要共同的 BaseClient。
- `tools.Tool`：保持中立；若修复并发执行，只增加可选的 parallel-safe 能力，不扩展成元数据框架。
- `tools/builtin/shell_runner.go`：Bash 和 PowerShell 确实共享进程、输出限制、环境和路径语义；两者 schema 与平台策略继续分开。
- `fallbackClient` 与 `weightedRouteClient`：前者表达故障切换，后者表达确定性主路由，不应合并。
- `runtimecore.BootstrapChannelRuntime`、`taskruntime.Runtime` 和 `ConversationRunner`：已经承担稳定的共享生命周期，修复其缺口，不再建第二套 channel framework。
- Telegram polling/forum/HTML、Slack Socket Mode/blocks、LINE webhook/reply token、Lark websocket/card/reaction 等 transport 细节。
- Agent 的 `ToolCall` 与 `llm.ToolCall`：一个是 Agent JSON 协议，一个是 provider 请求类型，显式 adapter 是合理边界。

#### 小 package 不应因行数被合并

- `internal/channels` 提供跨层稳定 channel vocabulary。
- `internal/llmconfig` 避免 route/provider 直接互相依赖。
- `internal/prompttmpl` 强制 `missingkey=error`，不是单纯改名。
- `internal/runtimeclock` 维护统一的运行时钟 metadata，并有独立测试。
- `internal/pathroots` 是带 normalize、alias、workspace override 和 containment root 语义的值对象。

#### 值得统一的真实语义

- `tools.Registry.Clone()` 和重复注册规则。
- static registry config 与 Guard config/factory。
- cache file containment、write path resolution、URL network policy 和 redaction。
- Agent settings 的解析与连接测试。
- channel history 裁剪、稳定的 approval 状态转换和 pending handle 生命周期。

其中 pending approval 只有在 A-08 的到期、take-once、generation release 测试先固定后再抽取。现在先复制修复也比抽出一个错误的通用 registry 更安全。

#### 不值得统一

- `firstNonEmpty`、两三行 map clone 等局部 helper。
- 只因 LINE/Lark command handler 行数相似而抽通用 transport callback；已有 `chatcommands` 应只拥有 dispatch result，发送仍由 adapter 负责。
- 平台 identity key、conversation key、reply token、thread id、reaction 和上传 API。
- 为了消除字段复制而引入反射、泛型 config mapper 或依赖注入框架。

## 6. 已有设计中应保留的部分

下面这些实现不需要为了修复上述问题而重写：

- `runtimecore` 管队列和 task lifecycle，`taskruntime` 管单次 Agent 执行，基本符合 `docs/runtime_layers.md`。
- 每次任务 clone registry，避免动态工具直接污染共享 registry。
- Engine、provider 和大部分 tool 调用持续传递 context，正常取消链清楚。
- context checkpoint 有 revision 校验，compaction 会保护完整 tool exchange。
- journal envelope 不理解领域 payload，projection 可从事实 replay，方向正确。
- file reference 对绝对路径、`..` 和 symlink escape 有二次校验。
- upload 使用唯一命名和 `O_EXCL`，不会覆盖已有文件。
- Console task generation 对正常 queued/running job 已有 ref count；缺口集中在 process-owned store/path 和 approval。
- URL fetch、web search、read file 的不可信输出在回送模型前有明确包装。

## 7. 建议修复顺序

### Phase 0：安全边界

1. 修复 A-01 的批次审批绕过。
2. 补齐多审批和前置工具 result 的回归测试。
3. 修复 A-04 的统一 final egress。
4. 修复 O-01 的 cache symlink containment，并覆盖 Telegram、Slack、Lark。
5. 统一 O-02 的 URL policy 和 redaction owner。

### Phase 1：事实与副作用

1. 统一 A-02 的 durable mutation/commit 语义。
2. 修复 A-05 的显式配置 fail-fast。
3. 修复 A-06 的有副作用工具执行顺序。
4. 给 cron 的读改写和 scheduler 加单机文件锁。
5. 让 Registry 拒绝静默碰撞，并统一 `Clone()`。

### Phase 2：runtime 一致性

1. 让 RuntimePaths 和 generation 贯穿 admission、task、HTTP 和 approval。
2. 处理 approval 到期和旧 generation。
3. 把 weighted candidate 的选择移到 run preparation。
4. 统一 static registry、Guard 和单次 Engine 的装配入口。
5. 删除 channel 的同形 private options 和重复默认值。

### Phase 3：长期运行稳定性

1. 删除流式状态的高频 durable write。
2. 回收 idle conversation worker。
3. 补齐 Integration/MCP/signal 生命周期。
4. 给 Console cache 和 HTTP 连接增加明确 owner 与上限。
5. 去掉 reusable package 对进程全局 logger、Viper 和 Cobra initializer 的修改。

### Phase 4：维护性

1. 按真实 domain 拆分 `server.go` 和 Console runtime 文件。
2. 统一 Agent settings 的解析、连接测试和 model listing。
3. 统一 task domain/journal codec。
4. 把 text、awareness、media 等中立语义移出 transport package。
5. 删除已被 canonical owner 替代的薄包装和测试专用全局 seam。
6. 修正文档中的旧入口和旧 package 路径。

每个 Phase 都可以独立提交。Phase 0 和 Phase 1 不应等待 package 重构。

## 8. 不建议采用的方案

- 不要因为 journal 的错误语义直接换数据库。先修 commit point 和 error contract。
- 不要引入分布式 scheduler。当前需要的是同机同路径文件锁。
- 不要增加依赖注入框架。一个显式 `RuntimePaths` 值和现有 options 已经够用。
- 不要建立通用工作流引擎处理 approval。精确 action identity 和有序 resume 已经足够。
- 不要把四个 channel 合成一个泛型大循环。只共享稳定的 lifecycle 代码。
- 不要只按文件行数拆 package；应按事实所有权和资源生命周期拆。
- 不要用反射或泛型 mapper 消除 options 字段复制；直接删除没有独立语义的镜像结构。
- 不要为局部字符串、map 和 slice helper 建通用 util package。
- 不要为资源回滚引入 cleanup framework；局部 `defer` 和单一 owner 足够。

## 9. 验证结果

### 9.1 审查时

已运行：

```text
go test ./...
go vet ./...
```

两者通过。

还运行了完整 race test：

```text
go test -race ./...
```

没有发现 data race，但完整命令中 `internal/acpclient.TestRunPrompt_ReturnsBeforeConnectionCloseAfterPrompt` 因 1 秒 context deadline 失败。该测试单独在 race 模式运行时通过。原因是并行测试在获取全局 fake ACP mutex 前就创建了 1 秒 deadline；这更像测试夹具的时序问题，本文没有把它列为产品架构缺陷。

本次审查发现多数是跨层组合缺口。现有单包测试通过，并不能覆盖“两次审批”“journal 成功但 snapshot 失败”“两个 runtime 使用不同 state dir”“weighted candidate 能力不同”等场景。

### 9.2 修复后

修复分支补充了审批作用域、durable commit、runtime generation、Guard 输出、weighted route、cron 排他执行、pending approval、worker panic、消息总线和 HTTP/WebSocket 关闭顺序等回归测试。

最终运行：

```text
go test ./... -count=1 -timeout=5m
go test -race ./... -count=1 -timeout=10m
go vet ./...
gofmt -l .
git diff --check
```

全部通过。`gofmt -l .` 和 `git diff --check` 没有输出。审查时偶发超时的 ACP 测试也已修正：测试现在先取得共享 fake connection，再启动本用例的 deadline。
