# MisterMorph Architecture

## 1. System Architecture

```text
                           +-------------------------+
                           |      User Surface       |
                           | CLI / Console / Channels|
                           +------------+------------+
                                        |
                   +--------------------+--------------------+
                   |                                         |
         +---------v---------+                     +---------v---------+
         | CLI bootstrap     |                     | integration API   |
         | cmd/mistermorph/* |                     | integration/*     |
         +---------+---------+                     +---------+---------+
                   |                                         |
                   +--------------------+--------------------+
                                        |
                           +------------v------------+
                           | Runtime Assembly Layer  |
                           | config snapshot + deps  |
                           +------------+------------+
                                        |
      +-----------------+---------------+--------------------+------------------+
      |                 |                                    |                  |
 +----v-----+   +-------v--------+                    +------v------+   +------v-------+
 | One-shot |   | Channels       |                    | Console     |   | Heartbeat    |
 | runtime  |   | event workers  |                    | local runtime|  | scheduler    |
 | run/serve|   | (bus-driven)   |                    | + runtime API|  | periodic chk |
 +----+-----+   +-------+--------+                    +------+-------+   +------+-------+
      |                 |                                      |                  |
      +-----------------+-------------------+------------------+------------------+
                                        |
                          +-------------v--------------+
                          | channelruntime/core        |
                          | runner + task lifecycle +  |
                          | memory runtime wiring      |
                          +-------------+--------------+
                                        |
               +------------------------+------------------------+
               |                                                 |
      +--------v--------+                              +---------v----------+
      | agent.Engine    |                              | daemonruntime      |
      | prompt/tools/LLM|                              | API + TaskView     |
      +---+---------+---+                              | memory/file-backed |
          |         |                                  +---------+----------+
 +--------v--+   +--v--------+                                   |
 | llm.Client|   | tools.Reg |                                   v
 +-----+-----+   +-----+-----+                        +----------+-----------+
       |               |                              | file_state_dir/tasks |
 +-----v-----+   +-----v------------------+           | topic.json / JSONL   |
 | providers |   | builtin/tools/adapters |           +----------------------+
 +-----------+   +------------------------+
Cross-cutting: guard, skills/prompt blocks, inspect dump, bus idempotency, HEARTBEAT.md
```

## 2. Execution Flows

### 2.1 Main Agent Run Flow (Registry Path)

```text
task/event
  -> build prompt/messages/meta
  -> build tool registry + llm tools
  -> agent.Engine.Run
     -> step loop
        -> LLM call (with tools)
        -> parse (plan | tool_call | final)
        -> tool execute (when tool_call)
        -> update plan/history/metrics
     -> if limits reached: force finalization request
     -> final output (guard redact if needed)
```

This is the primary execution path shared across entrypoints; independent non-registry requests are listed in 2.2.

Tools in this flow:

- Main step-loop LLM requests use runtime registry tools (`buildLLMTools(registry)`), including static and runtime-injected tools available in that runtime.
- The force-finalization fallback request uses no tools.

### 2.2 Independent LLM Requests (Non-Registry Path)

These requests are executed outside the normal agent step loop tool-registration path.

Agent / Plan tools:

- Forced finalization request when step/token limits are reached.
  tools: `none`
  files: `agent/engine_helpers.go`
- Plan generation request inside the `plan_create` tool implementation.
  tools: `none`
  files: `builtin/plan_create.go`

Telegram:

- Group-addressing decision request (this path injects local `message_react` for the addressing loop when context allows, and does not expose runtime registry tools).
  tools: `message_react` only when context allows; otherwise `none`
  files: `telegram/runtime.go`, `telegram/trigger.go`, `telegram/trigger_addressing.go`
- Init flow requests (question generation, profile fill, post-init greeting, SOUL polish, `/humanize`).
  tools: `none`
  files: `telegram/init_flow.go`, `telegram/runtime.go`
- Memory flow requests (session draft and semantic dedupe/merge support).
  tools: `none`
  files: `telegram/memory_flow.go`, `entryutil/semantic_llm.go`

Slack:

- Group-addressing decision request (this path can inject local `message_react` for the addressing loop when context allows, and does not expose runtime registry tools).
  tools: `message_react` only when context allows; otherwise `none`
  files: `slack/trigger.go`

LINE:

- Group-addressing decision request (this path can inject local `message_react` for the addressing loop when context allows, and does not expose runtime registry tools).
  tools: `message_react` only when context allows; otherwise `none`
  files: `line/trigger.go`

TODO semantics / references:

- Reference-id rewrite, complete-target semantic match, and WIP dedupe keep-index selection.
  tools: `none`
  files: `builtin/todo_update.go`, `todo/reference_llm.go`, `todo/semantic_llm.go`, `todo/ops.go`

Skills / installer:

- Remote skill installation safety-review request (extract download files and risks from untrusted SKILL content).
  tools: `none`
  files: `skillscmd/skills_install_builtin.go`

## 3. Two Runtime Families

### 3.1 One-shot (`run` / `serve`)

```text
CLI command -> config/registry/guard setup -> agent.Engine.Run -> output/json
```

- Entrypoints: `cmd/mistermorph/runcmd/run.go`, `cmd/mistermorph/daemoncmd/serve.go`
- Characteristics: single task execution or queued execution; no platform event consumer loop. `serve` additionally exposes `daemonruntime` task APIs backed by a runtime-owned queue plus a separate `TaskView`.

### 3.2 Channels Runtime Family

```text
platform event
  -> inbound adapter
  -> inproc bus
  -> per-conversation worker (serial)
  -> run*Task -> agent.Engine
  -> outbound publish
  -> delivery adapter
  -> platform send
```

- Channels runtime: `internal/channelruntime/{telegram,slack,line,lark}/*`
- Shared channels core: `internal/channelruntime/core/*`

## 4. Existing Topic Docs (Links Only)

The following areas already have formal docs, so this file only links them:

- Prompt system: [`./prompt.md`](./prompt.md)
- Tools system: [`./tools.md`](./tools.md)
- Security / Guard: [`./security.md`](./security.md)
- Skills system: [`./skills.md`](./skills.md)
- Heartbeat feature notes: [`./feat/feat_20260204_heartbeat.md`](./feat/feat_20260204_heartbeat.md)
- Telegram runtime behavior: [`./telegram.md`](./telegram.md)
- Slack Socket Mode: [`./slack.md`](./slack.md)
- LINE webhook runtime: [`./line.md`](./line.md)
- Bus design and implementation: [`./bus.md`](./bus.md), [`./bus_impl.md`](./bus_impl.md)

## 5. Key Areas Without Standalone Docs

### 5.1 Integration Embedding Layer

```text
host app
  -> integration.DefaultConfig + Set(...)
  -> rt := integration.New(cfg)      // snapshot at init
  -> rt.RunTask(...)                 // one-shot
  -> rt.NewTelegramBot/NewSlackBot   // long-running
```

Notes:

- `integration` is the third-party reuse entrypoint; host apps do not need to depend on CLI command wiring.
- `integration.New(cfg)` builds a snapshot of effective runtime config at initialization time.
- Code: `integration/runtime.go`, `integration/runtime_snapshot*.go`, `integration/channel_bots.go`

### 5.2 Memory Status

```text
runtime task/event
  -> runtime memory adapter (channels/console/heartbeat)
  -> injection (when enabled)
  -> record/update memory artifacts
```

Notes:

- Runtime-level memory integration is wired in Channels, Console local runtime, and Heartbeat.
- Shared orchestrator wiring (`internal/memoryruntime/*`) is reached through `internal/channelruntime/core/memory.go` from:
  - `internal/channelruntime/telegram/runtime.go`
  - `internal/channelruntime/slack/runtime.go`
  - `internal/channelruntime/line/runtime.go`
  - `internal/channelruntime/lark/runtime.go`
  - `cmd/mistermorph/consolecmd/local_runtime.go`
  - `internal/channelruntime/heartbeat/run.go`
- Storage model lives in `memory/*`.

### 5.3 Heartbeat Runtime Path

```text
heartbeat ticker OR authenticated POST /poke
  -> heartbeatutil.Tick(state, buildTask, enqueueTask)
  -> BuildHeartbeatTask(HEARTBEAT.md)
  -> heartbeat meta envelope (trigger=heartbeat, optional poke payload preview)
  -> agent.Engine.Run (normal tools/skills enabled)
  -> optional [[ Wake Signal ]] prompt block
  -> summary output (runtime-defined sink, e.g. logs/chat)
```

Notes:

- Heartbeat shares the same agent execution core; it differs mainly by scheduler path and metadata envelope.
- `/poke` is a wake trigger, not a task submission API. Its body is not schema-validated; at most a small textual preview is forwarded as untrusted context.
- If `/poke` arrives while heartbeat is already running, the admin server returns `409 Conflict` and the caller must retry.
- Scheduler-side skip reasons include `already_running`, `worker_busy`, `worker_queue_full`, and `empty_task`.
- Consecutive failures are tracked by `heartbeatutil.State`; alert escalation is emitted after threshold.
- Code:
  - shared helpers: `internal/heartbeatutil/heartbeat.go`, `internal/heartbeatutil/scheduler.go`
  - runtime integrations: `cmd/mistermorph/daemoncmd/serve.go`, `internal/channelruntime/heartbeat/run.go`, `cmd/mistermorph/telegramcmd/command.go`, `cmd/mistermorph/slackcmd/command.go`
  - admin server surface: `internal/daemonruntime/server.go`, `internal/daemonruntime/poke.go`

### 5.4 Task View and Persistence

```text
runtime submit / inbound accept
  -> runtime-owned queue/worker
     - serve: cmd/.../daemoncmd.TaskStore
     - channels/console: ConversationRunner + local state
  -> daemonruntime.TaskView update
     - queued / running / pending / done / failed / canceled
  -> optional file append
     - ConsoleFileStore: topic.json + daily topic logs
     - FileTaskStore: tasks/<target>/log/tasks.jsonl(.N)
  -> admin/console APIs read from TaskView
```

Notes:

- Execution queues stay in the runtime layer; `TaskView` is read/write state for task metadata, not worker orchestration.
- `TaskView` can be pure memory (`MemoryStore`) or file-backed (`ConsoleFileStore`, `FileTaskStore`) depending on `tasks.persistence_targets`.
- Topic list/delete APIs require `TopicReader` / `TopicDeleter`; currently Console Local provides them.

### 5.5 Plan Creation and Progress Lifecycle

```text
runtime setup
  -> RegisterPlanTool(...) (if enabled)
  -> AppendPlanCreateGuidanceBlock(...)
  -> run loop
     -> plan_create -> normalize -> validate -> cap
     -> each successful non-plan_create tool call: advance plan status
     -> final: mark remaining steps completed
```

- `plan_create` registration happens during runtime assembly when the runtime enables plan tooling (`PlanTool` feature) and calls `RegisterPlanTool(...)`.
- `RegisterPlanTool(...)` is effective only when registry is non-nil and `tools.plan_create.enabled=true` (default true).
- `tools.plan_create.max_steps` controls default plan step cap (fallback default is 6).
- Prompt guidance is injected only if `plan_create` exists in registry.
- Primary creation path is `plan_create`; direct `type="plan"` remains compatibility path.

- `agent.NormalizePlanSteps`:
  - trims step text,
  - drops empty steps,
  - normalizes status to `pending|in_progress|completed`,
  - enforces at most one `in_progress` (promotes first pending when needed).
- `plan_create.Execute(...)` calls `NormalizePlanSteps(...)` once, then:
  - rejects empty normalized steps (`invalid plan_create response: empty steps`),
  - applies `max_steps` cap.
- The tool no longer auto-fills placeholder names for empty steps.
- `AdvancePlanOnSuccess(...)` only changes status (`in_progress -> completed`, next `pending -> in_progress`) and returns `ok=false` when no valid in-progress step exists.
- `CompleteAllPlanSteps(...)` is status-only completion.

Telegram progress updates:

- Telegram runtime installs `WithPlanStepUpdate(...)` during task-run engine construction.
- On each completed plan step, it renders a short progress message via `generateTelegramPlanProgressMessage(...)` and publishes outbound with correlation id `telegram:plan:<chat_id>:<message_id>`.
- Progress send is skipped when plan is missing/empty or `CompletedIndex < 0`.

## 6. State Directory and Naming Baseline

```text
file_state_dir (default ~/.morph)
├── HEARTBEAT.md
├── contacts/
│   ├── ACTIVE.md
│   ├── INACTIVE.md
│   ├── bus_inbox.json
│   └── bus_outbox.json
├── memory/
│   ├── index.md
│   └── YYYY-MM-DD/<sanitized-session-id>.md
├── guard/
│   ├── audit/guard_audit.jsonl
│   └── approvals/guard_approvals.json
└── skills/<skill>/SKILL.md
```

Additional notes:

- `HEARTBEAT.md` is the default heartbeat checklist input (`statepaths.HeartbeatChecklistPath()`).
- Memory short-term filenames come from sanitized `session_id` values (letters, digits, `-`, `_`).
- Contacts bus dedupe keys:
  - inbox: `(channel, platform_message_id)`
  - outbox: `(channel, idempotency_key)`

## 7. Code Navigation

Recommended reading order:

1. `cmd/mistermorph/root.go` (entrypoint assembly)
2. `integration/runtime.go` (embedding entrypoint)
3. `agent/engine.go` + `agent/engine_loop.go` (execution core)
4. `internal/channelruntime/core/*` (shared channels core: runner, lifecycle, memory wiring)
5. `internal/channelruntime/{telegram,slack,line,lark}/runtime*.go` and `cmd/mistermorph/consolecmd/local_runtime.go` (runtime flows)
6. `internal/bus/*` and `internal/bus/adapters/*` (message bus and adapters)
