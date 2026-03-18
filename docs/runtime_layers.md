# Runtime Layers

This document explains the runtime layering used by channel runtimes such as Telegram, Slack, LINE, Lark, and Console.

The short version:

- adapter layer decides whether a message should become a task
- `runtimecore` decides when and where that task runs, and drives task status transitions
- `daemonruntime.TaskView` holds the task metadata view in memory or file-backed storage
- `taskruntime` decides how that accepted task is executed

## Three Layers

### 1. Adapter Layer

This is channel-owned logic.

It includes:

- inbound transport handling
- event parsing
- group trigger / addressing
- image download
- reply strategy
- reaction policy
- channel-specific memory identity mapping

The key question at this layer is:

> Should this inbound event become a task at all?

If the answer is no, execution stops here.

### 2. `runtimecore`

This is the worker and lifecycle layer.

It includes:

- per-conversation queue
- per-conversation worker
- task status transitions
- writing queued/running/done/failed snapshots into the runtime task view
- version/reset handling

The key question at this layer is:

> This task was accepted. When does it run, in which conversation lane, and what is its status?

`runtimecore` does **not** know prompt details, tool registration, or LLM wiring.
It also does **not** own the storage format; it only updates the injected task view.

### 3. `taskruntime`

This is the run-one-task execution layer.

It includes:

- main / plan client wiring
- runtime tool registration
- prompt assembly with sticky skills
- shared prompt blocks
- guard wiring
- `agent.Engine` construction
- optional memory injection / record hooks

The key question at this layer is:

> This task is already accepted and scheduled. How do we actually run it and get a final result?

`taskruntime` does **not** decide whether a group message should be accepted.

## Sequence

```text
Inbound message
    |
    v
Adapter layer
    - parse event
    - group trigger / addressing
    - channel-specific preprocessing
    |
    +--> rejected
    |      - ignore / append lightweight history / maybe react
    |      - stop
    |
    +--> accepted
           |
           v
runtimecore
    - enqueue by conversation key
    - select worker
    - mark queued/running/done/failed
    - handle reset/version
           |
           +--> daemonruntime.TaskView
           |      - MemoryStore, ConsoleFileStore, or FileTaskStore
           |      - powers /tasks and optional /topics APIs
           |
           v
channel run*Task wrapper
    - build channel-specific registry additions
    - build channel-specific current message
    - build memory identity / callbacks
           |
           v
taskruntime.Run(...)
    - register runtime tools
    - build prompt
    - apply shared prompt blocks
    - apply optional channel prompt block
    - run agent.Engine
    - apply memory hooks
           |
           v
result
    - final output
    - loaded skills
    - agent context
```

## One Concrete Example

Using Telegram as the example:

1. Telegram adapter logic receives a message.
2. Group trigger logic decides whether the bot should respond.
3. If accepted, a `telegramJob` is enqueued into the conversation worker.
4. `runtimecore` runs that job in order for that chat.
5. `runTelegramTask(...)` prepares Telegram-specific data.
6. `taskruntime.Run(...)` executes the shared prompt/tool/engine path.
7. Telegram runtime publishes output back to Telegram and updates history/state.

This means:

- group trigger is **before** `runtimecore`
- worker/lifecycle is **inside** `runtimecore`
- task metadata view is a sidecar updated during lifecycle changes
- actual shared task execution is **inside** `taskruntime`

## Why This Split Exists

Without the split, each channel runtime had to duplicate two different categories of logic:

- scheduling/lifecycle logic
- task execution wiring

Those two categories change for different reasons:

- worker/lifecycle changes are about concurrency, ordering, reset semantics, and daemon task state
- task execution changes are about prompt/tool/LLM/guard/memory wiring
- task view/storage changes are about query semantics, persistence, replay, and admin API reads

So they should not live in the same abstraction.

## Rule of Thumb

When reading or changing code, use this rule:

- if the question is "should we respond?" or "how do we talk to this platform?", it belongs to the adapter layer
- if the question is "when does this job run?" or "what task status should it have?", it belongs to `runtimecore`
- if the question is "how is the accepted task exposed to `/tasks` or persisted to disk?", it belongs to the `daemonruntime.TaskView` layer
- if the question is "how do we run the accepted task through the agent?", it belongs to `taskruntime`

## Code Pointers

- `runtimecore`: `internal/channelruntime/core/`
- `daemonruntime.TaskView`: `internal/daemonruntime/{store,console_store,file_store}.go`
- `taskruntime`: `internal/channelruntime/taskruntime/`
- Telegram group trigger decision: `internal/channelruntime/telegram/runtime.go`
- Telegram task execution wrapper: `internal/channelruntime/telegram/runtime_task.go`
