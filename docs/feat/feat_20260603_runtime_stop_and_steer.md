---
date: 2026-06-03
title: Runtime /stop 与 steer 方案
status: draft
---

# Runtime /stop 与 steer 方案

## 1) 范围

这份方案只处理三个能力：

1. `/stop`：用户可以停止当前 runtime 中当前 conversation 正在运行的任务，并收到可读的进展摘要和停止确认。
2. steer：用户在任务运行中继续输入时，这段输入可以进入当前 agent turn，而不是只能排成下一个独立任务。
3. 窄版 turn event：为 `/stop` 和 steer 提供明确的状态事件，让 UI 和 audit 不靠文本猜状态。

第一阶段优先做 agent loop 和 Console Local，因为它们已经有 task store、stream hub、chat command registry 和 per-conversation runner。后续再扩到 Telegram、Slack、Lark、LINE、managed runtime 和 CLI chat。

不在本方案内做：

- 完整照搬 Codex 的 Session/Turn/History 架构。
- fork、rollback、resume tree。
- 后台 shell 进程管理。
- 跨 runtime 的全局停止。
- 对已经发给模型的 tool call 做半途改写。
- 完整 app-server protocol、可 replay 的 turn item stream、turn diff 和 compaction event。

## 2) 当前事实

从现有代码看，底层已有一部分停止能力：

- `agent.Engine.Run(...)` 接收 `context.Context`，每个 step 开始会检查 `ctx.Err()`。
- LLM client、tool 执行、subtask runner 都沿用同一个 ctx。
- `bash` 通过 `exec.CommandContext(...)` 执行，ctx 取消会终止命令进程。
- ACP client 在 ctx 取消时会发送 `session/cancel`。
- `agent.Context` 已经记录 `Plan`、`Steps`、`Metrics`，可以生成停止时的进展摘要。
- `agent.Event` 已有 `tool_start`、`tool_output`、`tool_done`、`subtask_start`、`subtask_done`。
- Console 已经有 `streamHub` 和 event preview sink，可以显示工具活动。

缺的是 runtime 层的控制句柄：

- `internal/channelruntime/core.ConversationRunner` 只负责按 conversation 排队和串行执行，不保存当前 run 的 cancel func。
- Console Local 的 `pendingJobs` 只覆盖任务进入 bus 到 enqueue 前的短暂阶段，不代表正在运行的任务。
- 实现前，`daemonruntime` 只有 `POST /tasks`、`GET /tasks`、`GET /tasks/{id}`，没有停止或 steer 的控制端点。
- `chatcommands.NewRuntimeRegistry(...)` 还没有 `/stop`。
- `agent.Event` 还没有 turn 生命周期、stop 和 steer 事件。
- CLI chat 在 thinking 状态下不接收普通输入，Ctrl+C 当前更接近退出整个 chat，而不是停止当前 turn 后留在会话里。

所以 `/stop` 不应该写成一个普通 agent prompt，也不应该只更新 task 状态。它需要 runtime 拿到当前 run 的 cancel func，并让 agent/tool/LLM 都沿 ctx 退出。

## 3) 第一性原则

`/stop` 的本质是用户撤回当前任务的继续执行许可。正确行为是：

- 找到同一 runtime、同一 conversation 的当前 active run。
- 取消该 run 的 ctx。
- 不再调用下一轮 LLM。
- 尽力终止正在运行的工具。
- 把任务状态标为 `canceled`，原因是用户停止，不是超时。
- 返回当前进展和“已停止”或“正在停止”的明确反馈。

steer 的本质是用户在当前任务仍在执行时修改约束或补充信息。正确行为是：

- 运行中输入先进入一个有序队列。
- agent 在安全检查点把这些输入作为新的 user message 注入当前 turn。
- 注入后继续同一个 run，而不是新建一个 task。
- 如果用户需要立刻打断当前 LLM 请求或工具，应使用 `/stop`。

这里的安全检查点指：

- 每次 LLM 调用前。
- 工具批次结束后、下一次 LLM 调用前。
- 模型返回 final 之后、真正结束 run 之前。

不在工具调用半途插入 steer。原因很简单：tool call 已经进入 provider history，如果跳过对应 tool output，历史结构会损坏。要打断工具，用 `/stop`。

## 4) 核心设计

### 4.1 RunControl

新增一个 runtime 控制组件，例如 `internal/runtimecontrol`。它不是已有函数的薄封装，它真正拥有两类状态，并提供一个可选的进展文本回调：

- 当前 active run：runtime、conversation key、task id、run id、cancel func。
- 当前 steer queue：运行中用户输入，按收到顺序保存。
- 当前进展文本：由具体 runtime 本地生成，`RunControl` 不理解 plan、metrics、stream activity 的结构。

当前实现采用窄结构：

```go
type ActiveRun struct {
    Runtime         string
    ConversationKey string
    TopicID         string
    TaskID          string
    RunID           string
    Cancel          context.CancelCauseFunc
    Snapshot        func() string
    SteerQueue      *SteerQueue
}

type StopResult struct {
    Found    bool
    Progress string
}
```

`RunControl` 提供这些操作：

- `Start(run ActiveRun)`：run 开始前注册。相同 conversation 已有 active run 时返回错误。
- `StartLease(parent, timeout, run)`：为 worker 创建 cancel-cause ctx、timeout ctx、steer queue，并注册 active run。Console 和 channel runtime 都走这条路径。
- `Finish(runtime, conversationKey, taskID)`：run 结束后移除，返回是否真的清理了 active run。
- `Stop(runtime, conversationKey, reason)`：查找 active run，记录 stop request，调用 cancel。
- `StopTask(runtime, taskID, reason)`：按 task id 精确查找 active run，记录 stop request，调用 cancel。
- `Steer(runtime, conversationKey, input)`：查找 active run，把 input 放进 queue。

`Stop` 必须幂等。用户重复输入 `/stop` 时，不应报错；应返回同一个任务的当前停止进展。

### 4.2 Runtime 支持范围

`/stop` 的语义在所有 runtime 中保持一致：

- 只停止同一 runtime、同一 conversation/topic/thread 的当前 active run。
- 不停止其他 conversation，也不做跨 runtime 的全局停止。
- 没有 active run 时，返回“当前没有正在运行的任务”。
- 已经在停止中的 run，重复 `/stop` 返回当前停止进展。

各 runtime 的入口不同，但语义相同：

- Console Local：`/stop` command、`POST /topics/{topic_id}/stop`、`POST /tasks/{id}/stop`。
- Telegram/Slack/Lark/LINE standalone runtime：同一 chat/thread 内的 `/stop`。
- Console managed runtime：被 Console 托管启动的 Telegram/Slack/Lark runtime 也按 standalone runtime 的同一语义执行 `/stop`；managed 只改变进程托管方式，不改变用户语义。
- CLI chat：运行中输入 `/stop` 取消当前 turn，不退出 chat；空闲时 `/exit` 或 `/quit` 仍退出 chat。

### 4.3 停止原因

现在很多路径只看到 `context.Canceled`，无法区分用户停止、runtime 关闭和超时。Go 版本已经支持 cancel cause，建议在 run 外层用：

```go
runCtx, cancel := context.WithCancelCause(workerCtx)
```

停止时使用明确原因：

```go
cancel(runtimecontrol.ErrStoppedByUser)
```

任务收尾时读取 `context.Cause(runCtx)`：

- `ErrStoppedByUser`：状态写 `canceled`，错误文本写 `stopped by user`。
- `context.DeadlineExceeded`：状态写 `canceled`，错误文本写超时。
- runtime 关闭导致的取消：状态写 `canceled`，错误文本写 runtime stopped。

这样 UI 和日志不会把用户主动停止误报成普通错误。

### 4.4 停止进展

停止反馈不需要完整 dump。当前最小实现不定义通用 `ProgressSnapshot`，因为不同 runtime 能拿到的进展来源不同。`RunControl` 只接受：

```go
Snapshot func() string
```

Console 已经在 `handleTaskJob(...)` 里维护 `latestPlan` 和 `latestActivity`，所以 Console 本地把它们渲染成短文本，例如：

```text
计划 1/3，当前步骤 run tests，工具调用 2
```

如果后续 HTTP stop endpoint 需要结构化 JSON，再在 daemonruntime 层定义响应结构，不提前放进 `RunControl`。

反馈文本建议：

- 没有 active run：`当前没有正在运行的任务。`
- 收到停止请求但 run 还没退出：`已请求停止当前任务。当前进展：...`
- run 已退出：`已停止当前任务。当前进展：...`

### 4.5 窄版 turn event

这次只补 `/stop` 和 steer 需要的事件，不做完整 turn item 协议。

agent loop 发：

- `turn_start`：一次 `Engine.Run(...)` 开始。
- `turn_done`：正常 final 返回。
- `turn_canceled`：ctx 取消导致 run 结束。
- `steer_applied`：pending steer 已经注入 prompt history，下一次 LLM 请求会看到。

runtime/control 发：

- `run_stop_requested`：用户或 HTTP control endpoint 请求停止 active run。
- `run_stopped`：active run 已按用户停止原因收尾。
- `steer_queued`：运行中输入已进入 active run 的 steer queue。

事件字段沿用现有 `agent.Event`，只按需补少量字段：

```go
type Event struct {
    Kind            string
    RunID           string
    TaskID          string
    ConversationKey string
    TopicID         string
    Status          string
    Reason          string
    Text            string
    Summary         string
    Args            map[string]any
}
```

这些字段不要求一次全部填满。事件的基本规则：

- `turn_*` 事件由 agent loop 产生，表示模型任务生命周期。
- `run_*` 事件由 runtime/control 产生，表示用户控制动作和结果。
- `steer_queued` 和 `steer_applied` 成对出现；如果 run 被 stop，queued 但未 applied 的 steer 应留在 task result 中。
- task store 仍是任务最终状态的来源，event stream 只负责实时观察和 audit。

## 5) `/stop` 流程

### 5.1 Console Local

`POST /tasks` 收到 task 为 `/stop` 时，不创建普通 queued task。

流程：

1. 解析 topic id，得到 `conversationKey`。
2. 调用 `RunControl.Stop("console", conversationKey, "/stop")`。
3. 如果没有 active run，返回一个 synthetic done task，输出“当前没有正在运行的任务。”
4. 如果有 active run，立即返回一个 synthetic done task，输出“已请求停止...”和当前进展。
5. 被停止的 active task 自己在 `handleTaskJob(...)` 收尾时写成 `canceled`。
6. stream hub 发布 active task 的 `canceled` 状态，让 UI 能更新原任务。

这样 `/stop` 本身仍然是用户可见的一条 command 回复，但真正被停止的是原 active task。

### 5.2 HTTP 控制端点

为了让 Console UI 不必伪造 slash command，当前实现同时增加：

- `POST /tasks/{id}/stop`
- `POST /topics/{topic_id}/stop`

`/tasks/{id}/stop` 用 task id 精确停止。`/topics/{topic_id}/stop` 停止当前 topic 的 active run，适合聊天输入框和移动端 UI。

返回 JSON：

```json
{
  "status": "stopping",
  "found": true,
  "task_id": "console_...",
  "topic_id": "topic_...",
  "progress": "计划 1/3，当前步骤 run tests，工具调用 2",
  "message": "已请求停止当前任务。\n当前进展：计划 1/3，当前步骤 run tests，工具调用 2"
}
```

### 5.3 Channel runtimes 和 managed runtime

Telegram、Slack、Lark、LINE 的 `/stop` 语义应和 Console 一致：

- command 必须在同一 conversation 生效。
- 群聊里仍遵循现有显式触发规则。未明确发给 agent 的 `/stop` 不应误停。
- 停止成功后发送短回复。
- 如果没有 active run，回复没有正在运行的任务。

Channel runtime 当前也用 `ConversationRunner` 串行任务。接入时同样在 run 开始前注册 active run，结束后移除。

Console managed runtime 启动的是同一类 Telegram/Slack/Lark runtime。它们必须复用同一套 `/stop` 行为：

- 用户仍在原聊天软件里发送 `/stop`。
- 停止的是该 managed child runtime 中同 conversation 的 active run。
- Console supervisor 只负责进程生命周期，不改变 `/stop` 语义。

### 5.4 CLI chat

CLI chat 的 `/stop` 语义也应一致：

- 运行中输入 `/stop` 取消当前 turn，并打印停止反馈。
- 取消后保留 chat session 和历史，不退出进程。
- 空闲时 `/stop` 返回没有正在运行的任务。
- Ctrl+C 的语义和 `/stop` 对齐：运行中取消当前 turn，空闲时退出 chat。

### 5.5 Pending approval

approval pending 也是当前任务的一种暂停状态。最小处理：

- 如果 active run 已经返回 pending approval，`RunControl` 里没有 active run，此时 `/stop` 要能按 task id 或 topic 查到 pending task。
- pending task 被 `/stop` 后写成 `canceled`。
- resume 时如果 task 已经 `canceled`，拒绝继续执行。

如果 guard store 已支持拒绝 approval request，可以同步把 approval 标成 rejected；如果没有，先用 task 状态阻止 resume。

## 6) steer 流程

### 6.1 Runtime 接收

运行中普通输入的处理规则：

- 如果同一 conversation 没有 active run：按现有逻辑创建新 task。
- 如果有 active run，且输入不是 runtime command：作为 steer 输入进入 active run。
- 被接受为 steer 时，runtime 必须立刻给用户一条短反馈，说明输入已收到并会加入当前任务。
- 如果输入是 `/stop`：走停止流程。
- 其他 slash command 保持现有 command 语义，不注入 agent turn。

推荐反馈文本：

- Console：`已收到，已加入当前运行中的任务。`
- Channel runtime：`已收到，会在当前任务的下一步处理。`
- CLI chat：`已收到，会加入当前 turn。`

Console `POST /tasks` 可以在 response 上增加兼容字段：

```go
type SubmitTaskResponse struct {
    ID           string     `json:"id"`
    Status       TaskStatus `json:"status"`
    TopicID      string     `json:"topic_id,omitempty"`
    AcceptedAs   string     `json:"accepted_as,omitempty"`    // task|steer|command
    TargetTaskID string     `json:"target_task_id,omitempty"` // steer/command target
    SteerID      string     `json:"steer_id,omitempty"`
    Message      string     `json:"message,omitempty"`
}
```

旧客户端忽略新增字段即可。

### 6.2 SteerQueue

steer queue 的第一阶段只保证顺序和有界。去重依赖 channel message id 或 console submit id，需要和 `steer_events` 持久化一起做，先不提前放进内存队列。

当前结构只存文本：

```go
type SteerSource interface {
    Drain() []string
}
```

规则：

- 队列按收到顺序 drain。
- 队列设置最大条数。超限时拒绝新 steer，并提示用户当前任务输入过多。
- 如果后续做 `steer_events`，再给 steer 增加 id、source、created_at、applied_at。

### 6.3 注入 agent turn

在 `agent.RunOptions` 增加 steer source：

```go
type SteerSource interface {
    Drain() []string
}
```

`engineLoopState` 持有该 source。loop 在安全检查点调用 drain，并把 steer 合并成 user message：

```text
[[ Runtime Steer ]]
The user sent this while the current task was running. Apply it to this same turn:

...
```

注入点：

1. 每次 LLM 调用前：先 drain，再调用 hooks，再请求模型。
2. 工具批次结束后：进入下一轮 loop 时自然 drain。
3. 模型返回 final 后：如果此时 drain 到 steer，不返回 final；追加 assistant 文本，再追加 steer message，继续下一轮。

不在 tool call 执行前抢占当前 tool call。原因是 provider history 需要 tool call 和 tool output 成对出现。用户要阻止当前工具继续执行，应使用 `/stop`。

### 6.4 持久化

steer 输入后续必须可追踪，否则 topic history 会丢失用户补充的信息。当前第一阶段先让 steer 进入 active turn；持久化放到下一阶段。

Console 后续最小做法：

- active task 的 `Result` 增加 `steer_events`。
- 每个 steer 记录 id、source、text、created_at、applied_at。
- `buildConsoleTopicHistory(...)` 后续渲染历史时，把已完成 task 的 steer events 作为该 task 内的 inbound user 补充消息。

这不要求立刻引入完整 History Manager。等后续做结构化 history 时，再把 steer events 迁过去。

## 7) UI 和 audit 反馈

Console UI 至少需要显示四类状态：

- turn running：agent turn 已开始。
- steer queued：用户输入已进入当前任务。
- steer acknowledged：runtime 已向用户确认收到 steer。
- steer applied：agent 已在下一轮模型请求中看到这段输入。
- stopped/canceled：当前任务已停止。

最小事件流：

- `turn_start`
- `turn_done`
- `turn_canceled`
- `run_stop_requested`
- `run_stopped`
- `steer_queued`
- `steer_applied`

这些事件用于实时 UI 和 audit，不替代 task store。task store 仍然决定任务最终状态，event stream 只说明状态变化发生过。

## 8) 实施阶段

### Phase 1：最小 turn event schema

先写测试：

- agent run 正常 final 时发出 `turn_start` 和 `turn_done`。
- agent run 被 ctx 取消时发出 `turn_start` 和 `turn_canceled`。
- 事件里带 run id、task id 或 conversation key 中至少一个可关联字段。
- 现有 tool/subtask event 行为不变。

实现：

- 扩展 `agent.Event` 常量和必要字段。
- `engine_loop.go` 在 run 开始、正常结束、取消结束时发事件。
- Console event sink 可以接收新事件；暂时不要求完整 UI 改版。

验收：

- task audit 或 stream 里能看到 turn 生命周期事件。
- 没有引入 History Manager。

### Phase 2：Console Local `/stop`

先写测试：

- `RunControl.Stop` 对 active run 调用 cancel，重复 stop 幂等。
- 没有 active run 时返回 not found。
- Console `/stop` 不创建普通 queued task。
- 被停止的 active task 最终状态是 `canceled`，错误原因是 `stopped by user`。
- 停止反馈包含 plan/metrics/steps 中至少一类进展信息。
- 停止请求发出 `run_stop_requested`，任务收尾发出 `run_stopped` 或 `turn_canceled`。

实现：

- 增加 `internal/runtimecontrol`。
- `consoleLocalRuntime` 持有一个 control registry。
- `handleTaskJob(...)` 创建 run ctx 时使用 cancel cause。
- run 开始注册 active run，defer finish。
- `handleConsoleRuntimeCommand(...)` 注册 `/stop`。
- `routesOptions(...)` 给 daemon handler 注入 stop handler。
- stop path 发 `run_stop_requested`，active task 收尾时发 `run_stopped`。

验收：

- Console 输入 `/stop` 能停止同 topic 正在运行的任务。
- 长 shell 命令能随着 ctx 取消退出。
- UI 任务状态从 `running` 变为 `canceled`。

### Phase 3：agent steer source

先写测试：

- mock LLM 第一轮调用工具，工具结束后 drain 到 steer，第二轮请求包含 steer message。
- mock LLM 返回 final 时，如果 pending steer 存在，不结束 run，而是继续下一轮。
- 多条 steer 按顺序合并。
- 没有 steer 时现有 agent 测试行为不变。
- steer 注入 prompt history 时发 `steer_applied`。

实现：

- 增加窄版 `agent.SteerSource`，接口为 `Drain() []string`。
- `RunOptions` 增加 `SteerSource`。
- `engine_loop.go` 在安全检查点注入 steer。
- 发出 `steer_applied` event。

验收：

- steer 能进入当前 run 的下一次模型请求。
- 不破坏 tool call/output 配对。

### Phase 4：Console steer

先写测试：

- 同 topic 有 active run 时，普通 `POST /tasks` 被接受为 steer，不创建新 queued task。
- response 带 `accepted_as=steer`、`target_task_id`、`steer_id`。
- response 或 synthetic reply 包含已收到 steer 的短反馈。
- active task 的 result 记录 `steer_events`。
- 下一次 LLM 请求可以看到 steer。
- 收到运行中输入时发 `steer_queued`。

实现：

- Console submit path 在创建 task 前调用 `RunControl.Steer(...)`。
- 如果返回 found，说明 active run 存在，当前输入作为 steer 处理。
- stream hub 发布 `steer_queued`。
- `taskruntime.Run(...)` 把 steer source 传给 agent。

验收：

- 用户在 Console 中连续输入时，第二条输入进入第一条任务的 turn。
- UI 能显示输入已加入当前任务。
- 用户能立刻看到 steer 已收到的反馈。

### Phase 5：Channel runtimes 和 managed runtime

先写测试：

- Telegram/Slack/Lark/LINE 的 command registry 或现有 command 分支识别 `/stop`。
- 同 conversation 有 active run 时，新 inbound message 进入 steer。
- 新 inbound message 作为 steer 被接受时，channel 回复已收到。
- 群聊未显式触发的消息不 steer。
- 重复 message id 不重复注入。

实现：

- 给各 channel runtime 接入同一个 `RunControl`。
- 在 enqueue 前判断 active run。
- `/stop` 走 control registry。

验收：

- 各 channel 的长任务可以被同 conversation 的 `/stop` 停止。
- 运行中补充输入会进入当前任务。
- managed runtime 与 standalone runtime 的 `/stop` 和 steer 用户语义一致。

### Phase 6：CLI chat

CLI chat 当前 thinking 时禁用输入。它要支持 steer，需要让 TUI 在 thinking 状态仍能提交文本。

先写测试：

- thinking 状态下输入 `/stop` 只取消当前 turn，不退出 chat。
- thinking 状态下普通输入进入 steer queue。
- thinking 状态下普通输入被接受为 steer 时，CLI 打印已收到。
- Ctrl+C 的语义重新明确：一次取消当前 turn，空闲时退出 chat。

实现：

- chat model thinking 状态保留输入框。
- turn goroutine 不再独占 command dispatch。
- 当前 turn 的 cancel func 和 steer queue 注册到 chat session。

验收：

- CLI chat 能在任务运行中 stop/steer，并保持会话继续可用。

## 9) 风险与处理

工具不响应 ctx：

- Go 代码无法强杀任意不合作的 tool goroutine。
- 已有 shell 工具使用 `exec.CommandContext`，可以先覆盖主要长任务场景。
- 后续对长生命周期工具逐个补 ctx 响应测试。

steer 和 tool call 冲突：

- MVP 不抢占已开始的 tool batch。
- 用户要阻止工具继续执行，使用 `/stop`。

历史记录丢 steer：

- MVP 先把 steer 写入 active task result。
- 后续 History Manager 可用后，再改成结构化 history item。

用户其实想排新任务：

- Console 运行中普通输入默认 steer。
- 需要新任务队列时，可以后续增加显式 `/queue <task>` 或 UI 按钮。本方案先不做。

## 10) 最小结论与 Checklist

这三个能力应先以 runtime 控制和窄版事件为中心实现，而不是靠 prompt 约定。

`/stop` 的最小正确实现是：runtime 记录 active run，slash command 或 HTTP endpoint 调用 cancel cause，active task 收尾为 `canceled`，同时输出进展摘要。

steer 的最小正确实现是：runtime 把运行中输入放入 active run 的有序队列，agent loop 在安全检查点把它作为 user message 注入当前 turn，并记录 queued/applied 事件。

turn event 的最小正确实现是：agent loop 发 `turn_start`、`turn_done`、`turn_canceled`，runtime/control 发 `run_stop_requested`、`run_stopped`、`steer_queued`，让 UI 和 audit 能看到状态变化。

这条路径不要求先重写 history，也不要求先做完整 Codex session。它补的是当前长任务最直接的控制和观察缺口。

### 10.1 代码检查后的拆解

当前代码里的关键接入点：

- `agent/events.go`：已有 event sink 和 tool/subtask event，适合扩展 turn/stop/steer event。
- `agent/engine_loop.go`：run loop 已检查 ctx，工具批次后会回到下一轮 LLM 调用，适合加 steer 安全检查点。
- `agent/types.go`：`RunOptions` 还没有 steer source。
- `cmd/mistermorph/consolecmd/local_runtime.go`：`handleTaskJob(...)` 是 Console active run 注册、cancel cause、进展文本和收尾状态的主接入点。
- `cmd/mistermorph/consolecmd/local_runtime_bus.go`：Console submit 现在会直接创建 task，steer 判断必须放在创建 task 前。
- `internal/daemonruntime/server.go`：HTTP 只有 task submit/read，没有 stop control handler。
- `internal/channelruntime/core/runner.go`：runner 只排队，不保存 active run；不要把 cancel 状态塞进 runner。
- `internal/channelruntime/{telegram,slack,lark,line}/runtime.go`：每个 channel 都有自己的 worker runCtx 和 command 分支，需要分别接入 `/stop` 和 steer。
- `cmd/mistermorph/chatcmd/bubble.go`：thinking 状态下当前忽略普通输入，CLI steer 需要改 TUI 输入行为。
- `cmd/mistermorph/chatcmd/repl.go`：当前 turn 的 cancel func 和 steer queue 需要由 chat session 持有。

### 10.2 实施 Checklist

Phase 1：最小 turn event schema

- [x] 先补 `agent` 测试：正常 final 发 `turn_start`、`turn_done`。
- [x] 先补 `agent` 测试：ctx 取消发 `turn_start`、`turn_canceled`。
- [ ] 先补 Console event sink 测试：未知新 event 不影响现有 tool/subtask preview。
- [x] 在 `agent/events.go` 增加 `turn_start`、`turn_done`、`turn_canceled`、`run_stop_requested`、`run_stopped`、`steer_queued`、`steer_applied`。
- [x] 按需给 `agent.Event` 增加 `ConversationKey`、`TopicID`、`Reason` 字段。
- [x] 在 `agent/engine_loop.go` 的 run 开始、正常结束、取消结束发 turn event。
- [x] 取消结束事件使用 detached delivery context，避免 sink 因 run ctx 已取消而丢事件。
- [ ] 调整 Console activity/preview 处理，让新 event 能进 audit/stream，但不要求完整 UI 改版。
- [x] 跑 `go test ./agent ./cmd/mistermorph/consolecmd`。

Phase 2：共用 `RunControl` 和 `SteerQueue`

- [x] 先补 `internal/runtimecontrol` 测试：`Start`/`Finish` 注册和清理 active run。
- [x] 先补测试：`Stop` 调用 cancel cause，重复 stop 幂等。
- [x] 先补测试：`Steer` 按顺序进入队列并按顺序 drain。
- [ ] 先补测试：`Steer` 有队列上限。
- [ ] 先补测试：`Steer` 按 message id 去重。
- [x] 实现 `internal/runtimecontrol`，包含 active run registry、进展文本回调、stop result、steer queue。
- [x] 实现 `RunControl.StartLease(...)`，收拢 worker 的 cancel-cause ctx、timeout ctx、steer queue、active run 清理。
- [x] 定义 `ErrStoppedByUser`，并提供 stop/steer 的短反馈文案常量或 helper。
- [x] 跑 `go test ./internal/runtimecontrol`。

Phase 3：Console Local `/stop`

- [x] 先补 Console 测试：`/stop` 不创建普通 queued task。
- [x] 先补 Console 测试：active run 被 `/stop` 后最终状态为 `canceled`，错误原因为 `stopped by user`。
- [x] 先补 daemonruntime 路由测试：`POST /tasks/{id}/stop`、`POST /topics/{topic_id}/stop` 调用 stop handler。
- [x] 在 `consoleLocalRuntime` 持有 `RunControl`。
- [x] 在 `handleTaskJob(...)` 使用 `context.WithCancelCause`，注册 active run，defer finish。
- [x] 用 `latestPlan`、`latestActivity` 组装停止进展文本。
- [x] 在 `/stop` command 中调用 `RunControl.Stop(...)`。
- [x] 在 HTTP stop handler 中调用 `RunControl.Stop(...)` / `RunControl.StopTask(...)`。
- [x] `/stop` 返回 synthetic done task，输出停止请求和进展摘要。
- [x] active task 收尾时按 cancel cause 写 `canceled`，并发 `run_stopped`。
- [x] 跑 `go test ./cmd/mistermorph/consolecmd ./internal/daemonruntime`。

Phase 4：agent steer source

- [ ] 先补 `agent` 测试：工具批次结束后 drain steer，下一轮 LLM 请求包含 steer message。
- [x] 先补 `agent` 测试：max steps 后、`forceConclusion` 前 drain steer。
- [x] 先补 `agent` 测试：模型返回 final 但有 pending steer 时，不结束 run，继续下一轮。
- [x] 先补 `agent` 测试：steer 注入时发 `steer_applied`。
- [x] 在 `agent/types.go` 增加窄版 `SteerSource` 和 `RunOptions.SteerSource`。
- [x] 在 `engineLoopState` 保存 steer source。
- [x] 在每次 LLM 调用前 drain steer，追加 runtime steer user message。
- [x] 在 final 返回前再检查一次 steer；有 steer 时追加 assistant 文本和 steer message 后继续。
- [x] 确保不在 tool call 半途插入 steer，不破坏 tool call/output 配对。
- [x] 跑 `go test ./agent`。

Phase 5：Console steer

- [x] 先补 Console 测试：同 topic active run 存在时，普通 submit 被接受为 steer，不创建新 queued task。
- [ ] 先补 Console 测试：submit response 带 `accepted_as=steer`、`target_task_id`、`steer_id`、反馈文本。
- [ ] 先补 Console 测试：active task result 记录 `steer_events`。
- [ ] 扩展 `daemonruntime.SubmitTaskResponse` 兼容字段。
- [x] 在 Console submit 创建 task 前查询 active run；命中则调用 `RunControl.Steer(...)`。
- [x] 立即向用户返回“已收到”反馈，并发 `steer_queued`。
- [x] 将 `SteerSource` 通过 `taskruntime.RunRequest` 传到 `agent.RunOptions`。
- [ ] 在 Console task result 中保存 queued/applied steer events。
- [x] 跑 `go test ./cmd/mistermorph/consolecmd ./internal/channelruntime/taskruntime ./internal/daemonruntime`。

Phase 6：Channel runtimes 和 managed runtime

- [ ] 先补 Telegram 测试：同 chat active run 时 `/stop` 停止当前 run。
- [ ] 先补 Slack/Lark/LINE command 测试：`/stop` 被识别并返回一致反馈。
- [ ] 先补 channel 测试：运行中普通 inbound message 被接受为 steer，并回复已收到。
- [ ] 先补 channel 测试：群聊未显式触发的消息不 stop、不 steer。
- [x] 先补 Slack 测试：active run control key 使用 thread scope，避免同频道不同 thread 互相 stop/steer。
- [x] 给 channel runtime dependencies 或 run options 接入 `RunControl`。
- [x] 在各 channel worker runCtx 创建处通过 `RunLease` 注册 active run，收尾时 finish。
- [x] 在各 channel command 分支加入 `/stop`。
- [x] 在 enqueue 前判断 active run；命中则写 steer queue，不创建新 task。
- [x] managed runtime 复用 channel runtime 的同一 `RunControl` 语义，不单独定义一套行为。
- [x] 跑 `go test ./internal/channelruntime/telegram ./internal/channelruntime/slack ./internal/channelruntime/lark ./internal/channelruntime/line ./cmd/mistermorph/consolecmd`。

Phase 7：CLI chat

- [x] 先补 bubble 测试：thinking 状态可以输入并提交文本。
- [ ] 先补 REPL 测试：thinking 状态 `/stop` 取消当前 turn，不退出 chat。
- [ ] 先补 REPL 测试：thinking 状态普通输入进入 steer queue，并打印已收到。
- [x] 修改 `chatModel.Update(...)`，thinking 状态保留输入框，Ctrl+C 只请求取消当前 turn。
- [x] 在 `runREPL(...)` 中保存当前 turn cancel func 和 steer queue。
- [x] CLI `/stop` 使用同一 stop reason，取消后保留 history 和 session。
- [x] 将 CLI steer source 传给 `agent.RunOptions`。
- [x] 跑 `go test ./cmd/mistermorph/chatcmd`。

Phase 8：全量验证

- [x] 跑 `go test ./...`。
- [ ] 手动验证 Console：长 shell 任务运行中输入 `/stop`，原 task 变 `canceled`。
- [ ] 手动验证 Console：长任务运行中输入普通文本，收到 steer 反馈，下一轮 LLM 看到 steer。
- [ ] 手动验证 CLI chat：运行中 `/stop` 取消 turn，chat 继续可用。
- [ ] 手动验证至少一个 managed runtime：原聊天软件里 `/stop` 能停止 child runtime 的当前 run。
