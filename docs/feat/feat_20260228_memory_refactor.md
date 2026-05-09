# Memory Implementation Plan (WAL-first, Minimal)

Note: this plan doc is historical. The current event shape and projector behavior are documented in `docs/feat/memory-project-time-draft.md` and `docs/memory.md`.

## 1. Principles

- `memory/log/*.jsonl` is the single source of truth.
- `memory/*.md` is a projection (rebuildable from logs).
- Write path rule: `append + fsync` first, projection second.
- Channel runtime should only provide an adapter, not duplicate memory flow logic.
- No overdesign in v1.

## 2. Non-goals (v1)

- No distributed consensus.
- No multi-node writer arbitration.
- No remote replication.
- No background compaction service.
- No event bus dependency for memory pipeline.

## 3. Deliverables

- `internal/memoryruntime` package: shared orchestration.
- `memory/log/*.jsonl` journal with rotate + replay + checkpoint.
- Async markdown projector.
- Telegram adapter migration (final orchestrator path).
- Follow-up adapters for heartbeat and slack.

## 4. Work Breakdown

### Phase A: Data Contract

- [x] Define `MemoryEvent` schema with minimal fields:
  - `schema_version`
  - `event_id`
  - `task_run_id`
  - `ts_utc`
  - `session_id`
  - `subject_id`
  - `channel`
  - `participants[]` with:
    - `id`
    - `nickname`
    - `protocol`
  - `task_text`
  - `final_output`
  - `source_history`
  - `session_context`
- [x] Define id rules:
  - `event_id` unique per event.
  - `task_run_id` links events to one run.
- [x] Add schema validation helpers.
  - `participants` may be empty.
  - if `participants` contains items, each entry must satisfy one of:
    - normal participant: non-empty `id`, non-empty `nickname`, non-empty `protocol`
    - agent self marker: `id == 0`, non-empty `nickname`, `protocol == ""`

Acceptance:

- [x] Invalid events are rejected before journal append.

### Phase B: Journal (WAL)

- [x] Implement journal append API:
  - `Append(event) -> offset`
  - `Append` includes flush/sync.
- [x] Implement file rotation:
  - Rotate on size threshold only.
- [x] Implement monotonic naming:
  - `since-YYYY-MM-DD-0001.jsonl`, `since-YYYY-MM-DD-0002.jsonl`.
  - `YYYY-MM-DD` means the first record date in that segment.
- [x] Implement replay iterator (ordered by file + line).
- [x] Implement checkpoint store:
  - last applied file
  - last applied offset/line
- [x] Segment reuse on restart:
  - reopen latest segment and continue appending to the same file
  - do not create a new segment just because process restarted
- [x] Line offset rule:
  - count existing lines only when opening an existing segment
  - normal append path must be O(1): write + sync + line++
- [x] Checkpoint semantics:
  - checkpoint is projector/replay consumer progress, not writer progress
  - checkpoint tracks last applied `file + line`
  - on restart, replay resumes from checkpoint instead of replaying all logs
- [x] Keep active segment as plain `.jsonl`.
- [x] Optional: compress only closed old segments to `.jsonl.gz`.
- [x] Do not use `tar.gz` packaging for WAL segments.

Acceptance:

- [x] Crash after append but before projection keeps event durable.
- [x] Replay from checkpoint re-processes missing projections only.

Phase B public interfaces (current):

- `NewJournal(root string, opts JournalOptions) *Journal`
  - Construct journal writer/replayer under `<root>/log`.
- `(*Manager) NewJournal(opts JournalOptions) *Journal`
  - Convenience wrapper using manager memory root.
- `(*Journal) Append(event MemoryEvent) (JournalOffset, error)`
  - Validate + append one event, sync to disk, return `file + line`.
- `(*Journal) ReplayFrom(offset JournalOffset, limit int, fn func(JournalRecord) error) (JournalOffset, bool, error)`
  - Ordered replay with explicit max record count.
  - Returns `nextOffset` (last delivered record position) and `exhausted` (`true` when replay reached end).
- `(*Journal) LoadCheckpoint() (JournalCheckpoint, bool, error)`
  - Read projector progress from `checkpoint.json`.
- `(*Journal) SaveCheckpoint(cp JournalCheckpoint) error`
  - Atomically persist projector progress.
- `(*Journal) Close() error`
  - Close current active file handle.

### Phase C: Projector (Async)

- [x] Implement externally triggered projection entrypoint:
  - `(*Projector) ProjectOnce(ctx, limit)` replays from checkpoint and projects one bounded window
  - single-worker execution via projector-internal mutex
- [x] Implement runtime trigger policy (not per-event immediate):
  - projection is triggered by an external worker/scheduler
  - auto trigger when either condition is met:
    - timer interval reached (`N` minutes)
    - newly appended WAL events reached threshold (`M` records)
  - skip trigger when:
    - no new WAL records since last projection checkpoint
    - previous projection round is still running
  - manual trigger is not provided for now
  - per-round projection worker parallelism:
    - `X` goroutines
    - `X` derived from current UTC-day short-memory file count (clamped to >=1)
  - implementation note:
    - do not run multiple `ProjectOnce` calls concurrently (projector has global mutex today)
    - apply `X` parallelism inside one projection round (bucket-level projection workers)
- [x] Define projection windows:
  - read journal in batches (window applies to WAL event count)
  - group events by projection target (subject-derived target file)
  - run projection per target file; number of projections per pass equals number of touched files
- [x] Projection writes:
  - project to `memory/YYYY-MM-DD/{sanitize(subject_id)}.md` targets based on current subject organization
  - examples: `heartbeat.md`, `tg_-1003824466118.md`
  - when projecting one target, summary merge uses all current summary items in that target file (not a truncated in-file window)
- [x] Replay semantics:
  - duplicate replay processing is allowed (at-least-once style)
  - merge path handles duplicate impact
- [x] Use the single projection-flow semantic dedupe step for short-term merge (no separate dedupe pipeline).
- [x] Checkpoint advance policy:
  - checkpoint is flushed by batch (for example every 10 processed events)
  - on projection error, checkpoint still advances; caller gets returned error (no separate projector error log file)

Phase C public interfaces (current):

- `NewProjector(manager *Manager, journal *Journal, opts ProjectorOptions) *Projector`
  - Build one projector instance; trigger policy is controlled by caller.
- `(*Projector) ProjectOnce(ctx context.Context, limit int) (ProjectOnceResult, error)`
  - Replay one bounded window from checkpoint and project to markdown files.
  - Returns progress (`processed`, `next_offset`, `exhausted`).
- `memoryruntime.NewProjectionWorker(journal, projector, opts)`
  - Build runtime projection trigger worker.
- `(*ProjectionWorker) Start(ctx)`
  - Start timer/count trigger loop.
- `(*ProjectionWorker) NotifyRecordAppended()`
  - Notify worker that a new WAL event was appended (count-trigger hint).

Acceptance:

- [x] Append path does not block on markdown projection.
- [x] Markdown files can be rebuilt from journal + checkpoint via `ProjectOnce`.
- [x] Backlog larger than one window is drained incrementally across passes.

Phase C implementation breakdown (next):

1. [x] Add projection worker loop (external trigger owner):
   - keep one trigger coordinator goroutine
   - maintain `running` guard to prevent overlapping rounds
2. [x] Implement trigger decision:
   - run when `N`-minute interval fires
   - or when unprojected WAL count reaches `M`
   - skip when no new WAL events
3. [x] Implement `M`-threshold detection helper:
   - read checkpoint
   - replay up to `M` events from checkpoint (no-op callback)
   - treat `count >= M` as count-trigger
4. [x] Implement round execution with bounded work:
   - each round runs bounded replay windows (`limit`, `max_rounds`)
   - stop when exhausted or no progress
5. [x] Implement `X` worker parallelism in projection application:
   - compute `X` from current UTC-day short-memory file count (clamped to >=1)
   - parallelize bucket projection within one round
   - keep checkpoint writes deterministic
6. [x] Add tests for trigger policy:
   - interval-trigger and count-trigger
   - skip on no-new-events
   - skip on already-running
   - bounded round behavior (`limit`/`max_rounds`)

### Phase D: Shared Runtime Orchestrator

- [x] Create `internal/memoryruntime` with shared flow:
  - `PrepareInjection(...)`
  - `Record(...)`
  - `ProjectOnce(...)` (deprecated compatibility wrapper)
- [x] Define adapter interface for runtime-specific mapping:
  - `InjectionAdapter` for identity + request-context mapping
  - `RecordAdapter` for runtime-to-record request mapping
- [x] Remove duplicated inline memory flow from channel runtime code paths.

Acceptance:

- [x] Core flow logic exists in one place only.

Current orchestrator sequences (Phase D skeleton):

1) `PrepareInjection(...)`

```text
Caller            Orchestrator           Manager
  |                    |                    |
  | PrepareInjection   |                    |
  |------------------->|                    |
  |                    | BuildInjection     |
  |                    |------------------->|
  |                    |<-------------------|
  |<-------------------|  snapshot string   |
```

2) `Record(...)` (WAL append only)

```text
Caller            Orchestrator            Journal
  |                    |                    |
  | Record(request)    |                    |
  |------------------->|                    |
  |                    | build MemoryEvent  |
  |                    | Append(event)      |
  |                    |------------------->|
  |                    |<-------------------| offset(file,line)
  |<-------------------|                    |
```

3) `ProjectOnce(limit)` (deprecated compatibility wrapper)

Long-term design: projection trigger is externalized (scheduler/worker), not coupled to channel runtime request paths.

```text
External Worker       Projector           Journal/Manager
  |                      |                    |
  | ProjectOnce(limit)   |                    |
  |--------------------->| replay from cp     |
  |                      |--> apply projection |
  |                      |--> save checkpoint  |
  |<---------------------| result/error        |
```

### Phase H: Heartbeat Decoupling (Prerequisite)

- [x] Split heartbeat runtime flow from Telegram runtime.
- [x] Keep heartbeat dependency minimal:
  - requires task execution
  - optional notifier adapter (Telegram/Slack/etc.)
  - does not require Telegram runtime ownership
- [x] Remove Telegram-owned heartbeat control flow:
  - [x] scheduler/tick wiring
  - [x] `IsHeartbeat`-driven execution branching in Telegram request path
- [x] Preserve existing heartbeat alert semantics and task metadata.
- [x] Heartbeat run path must register tools like main flows:
  - register base registry tools
  - register runtime tools (`toolsutil.RegisterRuntimeTools`)

Phase H implementation steps (minimal):

1. [x] Add independent `internal/channelruntime/awareness` runtime:
   - owns ticker + enqueue + execution loop
   - no Telegram runtime dependency
2. [x] Keep execution path aligned with other run flows:
   - construct per-run registry from base tools
   - register runtime tools
   - run agent with heartbeat task/meta
3. [x] Integrate memoryruntime orchestrator:
   - write WAL event via `Record(...)`
   - fixed `subject_id/session_id` for heartbeat stream
   - do not couple runtime path with synchronous projection
4. [x] Move notification to adapter injection:
   - heartbeat runtime emits plain text notification message
   - adapter decides delivery channel (Telegram/Slack/etc.)
5. [x] Remove heartbeat ownership from Telegram runtime:
  - delete ticker/start/heartbeat state wiring there
  - keep Telegram as optional notifier adapter only

Notifier design (first-principles, no overdesign):

- Use one optional interface with one method only:
  - `Notify(ctx, text) error`
- Keep payload minimal:
  - plain text string only (no channel-specific fields in core interface)
- If notifier is nil:
  - heartbeat still runs; notification is skipped
- Channel-specific details belong in adapter implementations, not in heartbeat core runtime.

Acceptance:

- [x] Heartbeat can run without Telegram runtime enabled.
- [x] Telegram runtime no longer owns heartbeat scheduling/state transitions.
- [x] Heartbeat notification remains adapter-based and replaceable.

### Phase E: Telegram Migration (Final Path)

Goal:

- keep Telegram memory flow on the shared orchestrator-only path.

Decisions confirmed:

- Telegram `subject_id` keeps current semantics: chat-scoped (`tg:<chat_id>` equivalent mapping).
- No runtime path switch (`legacy|orchestrator`) is introduced; runtime path is orchestrator only.
- No shadow dual-run mode is introduced.
- Telegram adapter keeps existing draft construction behavior.
- Telegram adapter must keep current promote gating behavior (explicit-memory intent rules).

E1. Adapter Integration (no traffic switch):

- [x] Implement Telegram `InjectionAdapter` + `RecordAdapter` on top of `internal/memoryruntime`.
- [x] Wire Telegram runtime directly to `orchestrator` path as the only active runtime path.
- [x] Remove legacy runtime-selectable memory flow from Telegram.

E2. Cleanup:

- [x] Remove Telegram legacy direct memory flow code from Telegram runtime and tests.
- [x] Remove Telegram `/mem` command path and related docs/tests.

Acceptance:

- [x] Existing Telegram memory tests pass with `orchestrator` path.

### Phase F: Heartbeat and Slack Wiring

- [x] Heartbeat adapter wiring (reuse same orchestrator).
- [x] Slack adapter wiring (no copy-paste of flow).
- [x] Configure runtime flags consistently across channels for Slack memory options.

Acceptance:

- [x] Slack and heartbeat do not add a separate memory pipeline.

### Phase G: Verification and Operations

- [x] Add tests for:
  - append durability
  - rotate
  - replay
  - projector idempotency
- [x] Add kill/restart scenario test:
  - crash between append and projection
  - restart replay fixes projection
- [x] Add minimal operational docs:
  - how to inspect logs
  - how to replay/rebuild projection

Acceptance:

- [x] End-to-end recovery scenario is reproducible with documented steps.

## 5. Suggested Sequence

1. Phase A + Phase B
2. Phase C
3. Phase D
4. Phase H
5. Phase E
6. Phase F
7. Phase G

## 6. Stop Conditions (Avoid Overdesign)

- If a task does not improve durability, replayability, or de-duplication of runtime wiring, defer it.
- Keep first implementation single-process and local-only.
- Prefer simple append/replay correctness over optimization.
