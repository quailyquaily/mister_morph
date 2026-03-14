---
date: 2026-03-14
title: Channel Agent Runtime Core Abstraction Plan
status: proposed
---

# Channel Agent Runtime Core Abstraction Plan

## 1) Goal

- Evaluate whether current channel runtime implementations already provide enough information and stable shape to extract one shared runtime core.
- If yes, define a minimal core abstraction so Slack/Telegram/LINE/Lark become channel adapters that invoke this core.

This document is explicitly about:

- shared execution kernel for inbound task handling
- channel adapter boundaries
- migration plan with low regression risk

This document is not about:

- replacing channel-specific transport/webhook/polling code
- introducing a new message bus backend
- redesigning `agent.Engine`
- changing public integration API surfaces

## 2) Current State (Code-Verified)

The codebase already shares low-level primitives:

- `agent` engine, prompt assembly model, guard, llm types
- `depsutil` for logger/route/client/prompt spec resolution
- `runtimeworker` generic worker start/enqueue
- bus abstraction and per-channel inbound/outbound adapters
- `chathistory` rendering (`history` + `current_message`)
- shared memory orchestration primitives in `internal/memoryruntime` (`PrepareInjection*`, `Record*`)

But execution is still duplicated per channel runtime:

- each channel defines its own `job` struct and worker map
- each channel keeps its own in-memory history/sticky-skills store
- each channel performs the same task lifecycle transitions (`queued -> running -> done/failed`)
- each channel wires similar `run*Task` flow with per-channel prompt/runtime blocks
- each channel decides memory injection/recording strategy itself

Memory status today:

- Slack/Telegram runtime loops construct memory orchestrator + projection worker and wire task-level injection/recording.
- LINE/Lark already expose memory runtime options, but their `run*Task` paths are not yet wired to memory orchestrator calls.

In short, current shape is:

- multiple runtimes reusing common libraries
- not multiple runtimes calling one runtime core

## 3) Similarities and Differences

## Shared mainline shape

Across Slack/Telegram/LINE/Lark, the mainline flow is materially the same:

1. inbound bus message
2. normalize + group-trigger decision (for group chat)
3. enqueue per-conversation job (versioned)
4. load history + sticky skills snapshot
5. run `run*Task(...)` with timeout
6. publish outbound text (and errors) through bus
7. update daemon task store
8. append inbound/outbound events back to history

## Existing differences that must remain adapter-owned

- inbound transport details:
  - Telegram polling
  - Slack socket events
  - LINE/Lark webhook verification and payload details
- channel-specific ids and reply strategy:
  - Slack thread scope
  - Telegram reply-to message id
  - LINE reply token fallback
  - Lark reply-by-message id
- capability differences:
  - Telegram/LINE image paths and multimodal prompt parts
  - Slack/Telegram reaction tool behavior
  - memory integration maturity differs by channel today

Conclusion:

- the execution skeleton is shared enough for one core
- channel edge behavior must stay in adapters

## 4) Decision: Is There Enough Information to Extract a Core?

Yes.

Reason:

- The loop invariants are already stable and repeated in all channel runtimes.
- The repeated code is mostly lifecycle orchestration and state transitions, not channel business logic.
- Channel-specific differences can be expressed as hooks/callbacks at clear boundaries.

## 5) Proposed Architecture

## 5.0 ASCII Architecture Graph

```text
               (Slack / Telegram / LINE / Lark)
                           Platform Event
                                  |
                                  v
                    +-----------------------------+
                    | Inbound Adapter             |
                    | (channel-specific transport)|
                    +-------------+---------------+
                                  |
                                  v
                         Bus Direction: Inbound
                                  |
                                  v
                    +-----------------------------+
                    | Channel Adapter             |
                    | - ParseInbound              |
                    | - ShouldAccept              |
                    | - BuildJob                  |
                    +-------------+---------------+
                                  |
                                  v
                    +-----------------------------+
                    | Runtime Core                |
                    | ConversationRunner          |
                    | - worker queue + version    |
                    | - task lifecycle/status     |
                    | - history + sticky skills   |
                    +------+------+---------------+
                           |      |
                           |      +---------------------------+
                           |                                  |
                           v                                  v
                  +-------------------+            +----------------------+
                  | run*Task          |<---------->| Memory Runtime       |
                  | (channel-owned)   |            | Prepare/Record       |
                  +---------+---------+            +----------------------+
                            |
                            v
                         Outbound Text / Error
                            |
                            v
                         Bus Direction: Outbound
                            |
                            v
                    +-----------------------------+
                    | Delivery Adapter            |
                    | (channel-specific send/reply)|
                    +-------------+---------------+
                                  |
                                  v
                               Platform API
```

## 5.1 Runtime Core Package

Add a new package:

- `internal/channelruntime/core`

Core owns:

- per-conversation worker registry
- versioned job execution gate
- shared task lifecycle transitions
- history and sticky-skill state management primitives
- common error/outbound publish flow
- common memory hook call points in the task lifecycle

Core does not own:

- channel transport ingestion (webhook/polling/socket)
- channel prompt policy details
- channel-specific tool registration details
- channel-specific memory identity and participant mapping policy

## 5.2 Channel Adapter Contract (Minimal)

```go
type InboundEnvelope struct {
  ConversationKey string
  TaskID          string
  Text            string
  SentAt          time.Time
  Channel         string
  Meta            map[string]any
}

type TaskResult struct {
  FinalText      string
  LoadedSkills   []string
  HistoryAppends []chathistory.ChatHistoryItem
  Lightweight    bool
}

type MemoryIntegration struct {
  Enabled bool
  Hooks   MemoryHooks
}

type MemoryHooks struct {
  // Resolve channel-specific subject/session/participant context from inbound envelope and job.
  BuildInjectionRequest func(env InboundEnvelope, job any, maxItems int) (memoryruntime.PrepareInjectionRequest, bool, error)
  // Apply prepared memory snapshot into channel task input (usually prompt spec augmentation).
  ApplyInjectionSnapshot func(job any, snapshot string) (any, error)
  // Build channel-specific record request from run result and source history.
  BuildRecordRequest func(env InboundEnvelope, job any, run TaskResult, sourceHistory []chathistory.ChatHistoryItem) (memoryruntime.RecordRequest, bool, error)
}

type Adapter interface {
  ParseInbound(msg busruntime.BusMessage) (InboundEnvelope, error)
  ShouldAccept(ctx context.Context, env InboundEnvelope, history []chathistory.ChatHistoryItem) (bool, []chathistory.ChatHistoryItem, error)
  BuildJob(env InboundEnvelope, version uint64) (any, error)
  RunTask(ctx context.Context, job any, history []chathistory.ChatHistoryItem, stickySkills []string) (TaskResult, error)
  PublishOutbound(ctx context.Context, env InboundEnvelope, text string, kind string) error
  TaskInfoQueued(env InboundEnvelope, model string, timeout time.Duration) daemonruntime.TaskInfo
  MemoryIntegration() MemoryIntegration
}
```

Notes:

- `ShouldAccept` covers group-trigger decision and optional ignored-message history append in one place.
- `RunTask` remains channel-owned and can keep current `run*Task` implementation first.
- `HistoryAppends` allows channel-specific history events (reaction notes, agent output, etc.) without core knowing channel semantics.
- memory is a first-class adapter contract; all channels implement memory hooks when memory is enabled.

## 5.3 Core Loop Responsibility Split

Core-level algorithm:

1. parse inbound envelope
2. fetch/create worker by `ConversationKey`
3. snapshot history + sticky skills + worker version
4. call adapter `ShouldAccept(...)`
5. enqueue typed job
6. on worker execution:
   - run pre-run memory hook and apply injection snapshot (when memory enabled)
   - mark running
   - call adapter `RunTask(...)` with timeout
   - on error: fail task + publish error via adapter
   - on success: run post-run memory hook and record memory event (when memory enabled)
   - on success: publish output via adapter (if any)
   - apply history appends + sticky skills updates
   - mark done

Adapter-level algorithm:

- convert channel bus message to envelope/job
- perform group trigger, image download, mention handling, etc.
- construct prompt/runtime tool specifics
- map channel identity to memory subject/session/participants
- create outbound publish semantics for that channel

## 6) Data Model Choices

## Keep canonical key in core as string

- core only sees `ConversationKey string`
- adapters convert native ids:
  - Telegram `int64 chat_id` <-> `tg:<chat_id>`
  - Slack thread-scoped history key handled in adapter history appends/policy
  - LINE/Lark already string-based

## Keep history store in core, but key strategy adapter-controlled

- core provides map and trimming behavior
- adapter provides:
  - history scope key for write/read
  - history cap by mode/policy

This preserves Slack thread-specific behavior without leaking thread semantics into core.

## 6.1 Cross-Channel Memory Event Contract

When `memory.enabled=true`, all channels must be able to emit memory records through the same contract:

- `TaskRunID`
- `SessionID`
- `SubjectID`
- `Channel`
- `Participants`
- `TaskText`
- `FinalOutput`
- `SourceHistory`
- `SessionContext`

Channel adapters are responsible for deriving these fields from native channel ids and message metadata.

## 6.2 Memory Capability Rule

- Memory is not Slack/Telegram-only.
- Memory capability is required for Slack, Telegram, LINE, and Lark.
- If a channel cannot resolve a valid memory subject/context for a specific message, it should skip that message with explicit logs, but the channel still implements the memory hooks contract.

## 7) Migration Plan (Incremental)

## Phase 1: Extract shared worker/lifecycle kernel

- implement `core.Runtime` with generic conversation worker + queue + task state updates
- keep each channel `run*Task` unchanged
- adapt one channel first (recommend LINE or Lark for lower feature complexity)

Acceptance:

- functional behavior unchanged for migrated channel
- no prompt/tool behavior drift

## Phase 2: Move shared history/sticky-skills handling into core

- unify reset/version invalidation behavior
- unify history append ordering contract
- keep adapter-provided append items

Acceptance:

- history snapshots and post-run history are equivalent to pre-migration behavior

## Phase 3: Migrate all channels to core execution path

- migrate LINE and Lark first (simpler channel behavior)
- migrate Slack and Telegram second (thread/reaction/stream complexity)
- port Slack thread-scope and reaction history append via adapter hooks
- port Telegram lightweight publish policy and stream/reaction behavior via adapter hooks

Acceptance:

- `go test ./internal/channelruntime/...` remains green
- regression checks for group trigger and outbound reply paths pass

## Phase 4: Cross-channel memory integration (required)

- add core memory hook call points:
  - pre-run memory injection
  - post-run memory recording
- wire memory orchestrator/projection worker bootstrap in all channel loops (Slack/Telegram/LINE/Lark)
- implement LINE memory adapters (`InjectionAdapter` + `RecordAdapter`) and task-level prompt injection/recording
- implement Lark memory adapters (`InjectionAdapter` + `RecordAdapter`) and task-level prompt injection/recording
- align channel memory subject/session conventions and participant mapping rules in docs/tests

Acceptance:

- Slack/Telegram memory behavior unchanged
- LINE/Lark memory injection and recording work under `memory.enabled=true`
- all 4 channels honor `memory.injection.enabled` and `memory.injection.max_items` consistently

## Phase 5: Integration hardening

- add cross-channel integration tests for memory-enabled runtime flows
- validate memory event/projector compatibility and no schema drift
- add runtime observability checks for memory injection/record errors by channel

Acceptance:

- memory events from all channels are projector-compatible
- no channel silently drops memory path when memory is enabled

## 8) Non-Goals / Constraints

- Do not merge all channel `job` structs into one mega struct.
- Do not move webhook/polling/socket logic into core.
- Do not force one universal outbound semantics.
- Do not redesign `agent.Engine` and prompt profile system in this effort.
- Do not create channel-specific memory schemas; keep one memory event contract.
- Do not change integration API contracts used by embedders (`integration/*` and existing runtime-facing call shapes).

## 8.1 Integration API Compatibility Rule

- Runtime core extraction must be internal-only refactor at first.
- Existing integration entrypoints and behavior contract remain stable.
- Any required integration API expansion must be additive and backward compatible, and is out of scope for this plan.

## 8.2 Console Runtime Positioning (Agreed)

The console runtime is positioned as a first-class channel adapter with its own runtime loop and lifecycle semantics.

1. API parity target:
   - Console runtime must implement the same runtime API contract used by other channels (`/health`, `/overview`, `/tasks`, `/tasks/{id}`, state/todo/contacts/persona/memory/audit APIs, and approvals parity where applicable).
   - For `console serve`, external console backend surface remains `/console/api*`; runtime APIs are consumed through the console adapter path.
2. Adapter role:
   - Console is treated as another channel adapter invoking the shared runtime core.
   - It has channel-owned `job` shape and adapter-owned edge behavior, same as Slack/Telegram/LINE/Lark.
3. Topology:
   - Same-process topology is accepted: console backend + console runtime core + runtime API adapter in one process.
4. Serve listen configuration split:
   - Replace single shared `server.listen` coupling with channel-scoped settings:
     - `telegram.serve_listen`
     - `slack.serve_listen`
     - `line.serve_listen`
     - `lark.serve_listen`
     - `console.serve_listen`
   - Give each channel a different default port to avoid collisions.
   - Keep backward compatibility by allowing fallback from `<channel>.serve_listen` to existing `server.listen` during migration.
5. Core lifecycle:
   - Console inbound submit must follow the same adapter->core->run->lifecycle flow as other channels.
6. Memory identity:
   - Console memory `subject_id` / `session_id` must use `console:*` convention.
7. Compatibility meaning (clarified):
   - Existing clients and scripts should keep working during migration without forced breakage.
   - Any API/config changes required by this repositioning should be additive first, with clear fallback and deprecation path.
8. Verification:
   - Add parity and integration tests for console runtime against the shared runtime API contract and auth boundaries.

## 9) Risks and Mitigations

- Risk: accidental behavior drift in history ordering.
  - Mitigation: snapshot-based regression tests for history before/after run.
- Risk: adapter API too abstract and hides bugs.
  - Mitigation: keep adapter interface small and concrete; avoid framework-style over-generalization.
- Risk: Slack/Telegram advanced features overfit core.
  - Mitigation: migrate simple channels first, then expand hook set only when strictly required.
- Risk: memory integration parity breaks across channels.
  - Mitigation: make memory hooks mandatory in adapter contract and gate with integration tests on all channels.

## 10) Recommendation

- Proceed with extraction.
- Start from the minimum core that removes duplicated orchestration only.
- Keep `run*Task` logic channel-owned in first iteration.
- Expand shared surface only after proving no behavior regressions.

This is the smallest path from:

- "shared low-level libs"

to:

- "channel runtimes calling one runtime core."
