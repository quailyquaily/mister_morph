---
date: 2026-03-14
title: Channel Agent Runtime Core Abstraction Plan (V2)
status: revised
---

# Channel Agent Runtime Core Abstraction Plan (V2)

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

## 1.1) V2 Clarification: What Was Actually Finished vs. What Was Not

This V2 update exists because V1 could reasonably be misread as "runtime abstraction is already basically done."

That is not the current reality.

What was already extracted before V2:

- low-level worker primitives in `internal/channelruntime/core`
- memory orchestration primitives and hooks
- `depsutil` for logger / route / client / prompt-spec dependency passing
- bus abstractions and channel-specific transport adapters

What was **not** actually extracted before V2:

- one shared internal task-execution runtime that all channels call
- one shared place that wires:
  - main / plan / addressing clients
  - runtime tools
  - prompt spec + skills
  - persona / local tool notes / todo workflow / memory prompt blocks
  - guard
  - MCP
  - `agent.Engine` construction

The main source of confusion was `integration.Runtime`:

- it already looked like a shared task-execution runtime
- `console` had even started delegating to it at one point
- but `integration.Runtime` is an embedding-facing facade, not the intended internal runtime core
- using it internally proved the common execution layer exists, but it did **not** mean the internal abstraction had been completed at the right boundary

V2 therefore makes one distinction explicit:

- `runtimecore` already exists, but it is only the lower-level orchestration primitive layer
- the actual missing layer is a shared **task-execution runtime** above `runtimecore`

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
- until V2, `console` also proved the gap from the other side: it temporarily reused `integration.Runtime` to get a shared execution stack, which showed the shared stack is real, but also showed it does not belong in `integration`

Memory status today:

- Slack/Telegram runtime loops construct memory orchestrator + projection worker and wire task-level injection/recording.
- LINE/Lark already expose memory runtime options, but their `run*Task` paths are not yet wired to memory orchestrator calls.

In short, current shape before V2 correction was:

- multiple runtimes reusing common libraries
- one embedding runtime facade (`integration.Runtime`) that partially overlaps with the missing internal runtime layer
- not multiple runtimes calling one shared internal task runtime

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

## 5.1 Revised Package Split (V2)

Keep the existing package:

- `internal/channelruntime/core`

`runtimecore` owns:

- per-conversation worker registry
- versioned job execution gate
- shared task lifecycle transitions
- history and sticky-skill state management primitives
- common error/outbound publish flow
- common memory hook call points in the task lifecycle

Add a new package above it:

- `internal/channelruntime/taskruntime`

`taskruntime` owns:

- LLM route / client selection for runtime purposes
- runtime tool registration
- prompt-spec assembly with skills
- prompt profile augmentation:
  - persona identity
  - local tool notes
  - plan-create guidance
  - todo workflow
  - memory summaries
  - channel/runtime-specific blocks when needed
- guard injection
- MCP tool wiring
- `agent.Engine` construction
- one canonical "run one task" path used by:
  - console
  - daemon submit path
  - Slack / Telegram / LINE / Lark adapters

`runtimecore` still does not own:

- channel transport ingestion (webhook/polling/socket)
- channel prompt policy details
- channel-specific tool registration details
- channel-specific memory identity and participant mapping policy

`integration` stays outside this split:

- `integration.Runtime` becomes a consumer or thin facade over `taskruntime`
- it is no longer treated as the internal abstraction target itself

## 5.2 Concrete Boundary After V2

After the actual V2 extraction, the intended boundary is simpler than the earlier adapter-contract sketch.

`taskruntime` owns only two things:

1. bootstrap of shared execution dependencies
   - logger / log options
   - main-loop route + client
   - plan-create route + client
   - shared base registry
   - shared guard
2. run-one-task execution wiring
   - runtime tool registration
   - prompt spec loading with sticky skills
   - shared prompt blocks
   - optional channel prompt augmentation callback
   - `agent.Engine` construction
   - optional memory injection / record callbacks
   - optional plan-progress / stream callbacks

Everything else stays outside `taskruntime`:

- inbound transport and event parsing
- addressing / group-trigger decisions
- per-channel `job` structs
- worker lifecycle and versioning
- history storage and trimming
- outbound publish semantics
- reaction / attachment / image-download behavior
- channel-specific memory identity derivation

The key simplification rule is:

- do not define a universal channel adapter framework unless runtimecore migration later proves it is actually needed
- do not force all channels through one common `InboundEnvelope` / `Adapter` interface up front
- keep the shared layer at "run one task" and nothing more

## 5.3 Current Concrete Shape

The concrete shape after this extraction is:

- `runtimecore`
  - conversation worker queue
  - task lifecycle/status bookkeeping
  - shared memory runtime bootstrap helpers
- `taskruntime`
  - shared task execution wiring
- channel packages
  - transport + inbound parsing
  - addressing / group policy
  - channel-specific registry additions
  - channel-specific prompt blocks
  - channel-specific memory identity and record details
  - outbound send / reaction behavior

This is intentionally less ambitious than a full adapter framework.

## 6) Data and Memory Constraints

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

## Phase 0: Correct boundary assumptions (V2)

- document explicitly that `runtimecore` is not the full abstraction target
- stop using `integration.Runtime` as the implicit internal core for console
- align console task execution wiring with Slack/Telegram-style internal runtime wiring first

Acceptance:

- no internal runtime path depends on `integration.Runtime` as its execution core
- document clearly states which layer already exists and which layer is still missing

## Phase 1: Extract shared task-execution runtime above `runtimecore`

- implement `internal/channelruntime/taskruntime`
- move shared single-task execution wiring there
- keep channel transport / adapter code untouched
- keep `run*Task` behavior equivalent during the move

Acceptance:

- console and daemon can call the new shared task-execution layer
- no prompt/tool/guard/MCP behavior drift

## 7.1 Phase 1 Work Breakdown (Execution Plan)

The implementation order for Phase 1 should be explicit and narrow:

1. Add `internal/channelruntime/taskruntime` bootstrap layer
   - own logger / log-options capture from `depsutil.CommonDependencies`
   - own main-loop and plan-create route resolution
   - own main client / plan client construction
   - own shared base registry lookup
   - own shared guard lookup
   - expose stable runtime struct fields needed by adapters:
     - main route / client / model
     - plan route / client / model
     - logger / log options
     - agent config
     - base registry / shared guard
2. Add shared per-task execution entrypoint
   - clone or accept adapter-prepared registry
   - register runtime tools in one place
   - build prompt spec with sticky skills
   - always apply shared prompt augmentations in one place:
     - persona identity
     - local tool notes
     - plan-create guidance
     - todo workflow
   - allow adapter-owned hooks for:
     - channel prompt blocks
     - plan progress callbacks
     - stream callbacks
     - memory injection / record callbacks
     - per-run meta / scene / current-message inputs
3. Migrate `console` first
   - replace local duplicated main/plan client wiring with `taskruntime`
   - replace local duplicated agent/prompt/runtime-tool execution path with shared per-task execution entrypoint
   - keep console-specific registry bootstrap, MCP bootstrap, and runtime API adapter unchanged
4. Migrate LINE and Lark second
   - move their `run*Task` shared execution flow onto `taskruntime`
   - keep image/history builders and memory identity helpers adapter-owned
5. Migrate Slack and Telegram last
   - preserve reaction tools, plan-progress hooks, lightweight publish policy, and stream handling
   - only move shared execution wiring; do not absorb Slack thread policy or Telegram media handling into `taskruntime`
6. Add focused regression tests
   - bootstrap route/client reuse behavior
   - prompt augmentation ordering
   - memory injection / record hook execution
   - console task execution path
   - one migrated channel sanity test per channel family

Acceptance for the work breakdown:

- no internal runtime path constructs prompt/tool/guard wiring ad hoc once migrated
- channel `run*Task` wrappers become adapter-focused and materially smaller
- `console` no longer carries a private copy of task execution wiring
- `integration.Runtime` remains untouched in this phase unless needed as a thin consumer later

## Phase 2: Keep shared worker/lifecycle kernel in `runtimecore`

- keep `runtimecore` as the conversation worker + queue + task state layer
- keep each channel `run*Task` unchanged at first
- migrate one simpler channel adapter first (recommend LINE or Lark)

Acceptance:

- functional behavior unchanged for migrated channel
- no prompt/tool behavior drift

## Phase 3: Move shared history/sticky-skills handling into `runtimecore`

- unify reset/version invalidation behavior
- unify history append ordering contract
- keep adapter-provided append items

Acceptance:

- history snapshots and post-run history are equivalent to pre-migration behavior

## Phase 4: Migrate all channels to `taskruntime` + `runtimecore`

- migrate LINE and Lark first (simpler channel behavior)
- migrate Slack and Telegram second (thread/reaction/stream complexity)
- port Slack thread-scope and reaction history append via adapter hooks
- port Telegram lightweight publish policy and stream/reaction behavior via adapter hooks

Acceptance:

- `go test ./internal/channelruntime/...` remains green
- regression checks for group trigger and outbound reply paths pass

## Phase 5: Cross-channel memory integration (required)

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

## Phase 6: Integration hardening

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
- If `integration.Runtime` is later reimplemented on top of `taskruntime`, this remains an internal compatibility-preserving refactor.
- Any outward integration API expansion must be additive and backward compatible, and is out of scope for this plan.

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
   - Give each channel a different default port to avoid collisions.
   - Keep backward compatibility by allowing fallback from `<channel>.serve_listen` to existing `server.listen` during migration.
   - Console was later simplified again to use an in-process local runtime transport, so it no longer needs a dedicated `console.serve_listen`.
5. Core lifecycle:
   - Console inbound submit must follow the same adapter->taskruntime->runtimecore->run->lifecycle flow as other channels.
6. Memory identity:
   - Console memory `subject_id` / `session_id` must use `console:*` convention.
7. Compatibility meaning (clarified):
   - Existing clients and scripts should keep working during migration without forced breakage.
   - Any API/config changes required by this repositioning should be additive first, with clear fallback and deprecation path.
8. Verification:
   - Add parity and integration tests for console runtime against the shared runtime API contract and auth boundaries.

## 8.3 Console Status After V2 Clarification

- Console is no longer treated as evidence that abstraction is already finished just because it once used `integration.Runtime`.
- The correct reading is:
  - Console had temporarily crossed a package boundary to reuse a shared execution facade.
  - That proved the missing shared layer was real.
  - It did not prove the internal runtime abstraction had already been completed.
- In V2, console should first be aligned with channel-internal wiring, then migrated onto `taskruntime` once that layer exists.

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
- Treat V1 as "primitives extracted, core runtime not finished" rather than "abstraction complete."
- Build the missing shared task-execution layer above `runtimecore`, not inside `integration`.
- Keep `run*Task` logic channel-owned in first iteration.
- Expand shared surface only after proving no behavior regressions.

This is the smallest path from:

- "shared low-level libs"
- "one embedding-facing runtime facade that partially overlaps"

to:

- "channel runtimes calling one internal taskruntime + runtimecore stack."
