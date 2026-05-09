# Memory System (Core + Wiring)

This document describes how memory works in `mistermorph`.

## 1. Scope and Status

- Core memory subsystem is channel-agnostic and lives in `memory/*`.
- Durable source of truth is WAL under `memory/log/*.jsonl`.
- Markdown files under `memory/index.md` and `memory/YYYY-MM-DD/*.md` are projections (read model).
- Runtime-level memory wiring now uses one path for all channels:
  - shared orchestrator adapter (`internal/memoryruntime`) for Telegram, Slack, and Awareness.

## 2. Core Components

- Memory core (channel-agnostic):
  - `memory/manager.go`, `memory/update.go`, `memory/merge.go`, `memory/inject.go`
- Memory WAL and projection:
  - `memory/journal.go` (append/replay/rotate/checkpoint)
  - `memory/projector.go` (replay -> markdown projection)
- Shared runtime orchestrator:
  - `internal/memoryruntime/orchestrator.go`
  - `internal/memoryruntime/adapter.go`
  - `internal/memoryruntime/worker.go`
- `internal/memoryruntime/resolver.go`
- `internal/memoryruntime/draft.go`
- `internal/memoryruntime/prompts/memory_draft_system.md`
- `internal/memoryruntime/prompts/memory_draft_user.md`
- Runtime adapters:
  - `internal/channelruntime/telegram/runtime_task.go`
  - `internal/channelruntime/telegram/memory_flow.go`
  - Slack: `internal/channelruntime/slack/runtime.go`, `internal/channelruntime/slack/runtime_task.go`, `internal/channelruntime/slack/memory_flow.go`
  - Awareness: `internal/channelruntime/awareness/run.go`, `internal/channelruntime/awareness/memory_flow.go`
- LLM semantic helpers used by projection dedupe:
  - `internal/entryutil/semantic_llm.go`

## 3. ASCII Architecture (Memory Path)

```text
                 +----------------------------------------+
                 | Runtime Memory Adapter                 |
                 | (telegram/slack/awareness)             |
                 +------------------+---------------------+
                                    |
          +-------------------------+--------------------------+
          |                                                    |
 +--------v---------+                                +---------v----------+
 | Injection Path   |                                | Writeback Path     |
 | PrepareInjection |                                | Record raw event   |
 +--------+---------+                                +---------+----------+
          |                                                    |
 +--------v---------+                                +---------v----------+
 | memory.Manager   |                                | memory.Journal     |
 | BuildInjection   |                                | append + fsync     |
 +--------+---------+                                +---------+----------+
          |                                                    |
          |                                                    |
 +--------v-----------------------------+          +-----------v------------+
 | Markdown projections (read model)    |          | WAL source of truth    |
 | memory/index.md                      |          | memory/log/*.jsonl      |
 | memory/YYYY-MM-DD/*.md               |          +-----------+------------+
 +------------------+-------------------+                      |
                    ^                                          |
                    |                                          |
                    |                              +-----------v------------+
                    |                              | memory.Projector       |
                    +------------------------------+ Replay + Resolve Draft |
                                                   | + Merge + Save         |
                                                   +-----------+------------+
                                                               |
                                                               v
                                                     +---------------------+
                                                     | LLM(optional)       |
                                                     | memory.draft / dedupe|
                                                     +---------------------+
```

## 4. ASCII Runtime Flows (Shared Orchestrator Wiring)

### 4.1 Main Skeleton

```text
runtime task
  -> injection phase
  -> agent.Engine.Run
  -> writeback gate (publishText + orchestrator + subject_id)
  -> record event to WAL (if gate passed)
```

### 4.2 Injection Flow

```text
Runtime(Adapter)         Orchestrator               Manager/FS
     |                        |                        |
     | PrepareInjection(...)  |                        |
     |----------------------->|                        |
     |                        | BuildInjection(...)    |
     |                        |----------------------->|
     |                        | read projected md files|
     |                        |<-----------------------|
     | snapshot text          |                        |
     |<-----------------------|                        |
     | append memory block into prompt                 |
```

### 4.3 Writeback Flow

```text
Runtime(Adapter)                  Orchestrator          Journal(WAL)
     |                                 |                     |
     | [gate] publishText && subject_id present?             |
     | build raw memory event input    |                     |
     | Record(...)                     |                     |
     |-------------------------------->| append + fsync      |
     |                                 |-------------------->|
     | record offset                   |                     |
     |<--------------------------------|                     |
```

### 4.4 Projection Flow (External Worker)

```text
External Project Worker       Projector            LLM(optional)                  FS
          |                      |                      |                           |
          | ProjectOnce(N)       |                      |                           |
          |--------------------->| load checkpoint      |                           |
          |                      |----------------------------------------------->   |
          |                      | replay WAL from cp   |                           |
          |                      |----------------------------------------------->   |
          |                      | resolve draft from raw event                    |
          |                      |--------------------->|                           |
          |                      |<---------------------|                           |
          |                      | group/merge buckets  |                           |
          |                      | semantic dedupe      |                           |
          |                      |--------------------->|                           |
          |                      |<---------------------|                           |
          |                      | write short/long md  |                           |
          |                      |----------------------------------------------->   |
          |                      | write checkpoint     |                           |
          |                      |----------------------------------------------->   |
          | result               |                      |                           |
          |<---------------------|                      |                           |
```

Notes:

- Flows above are shared wiring for Telegram, Slack, and Heartbeat.
- Hot path writes only WAL; markdown projection is out-of-band.
- Runtime starts one projection worker per process when memory is enabled.
- Worker trigger policy:
  - timer trigger every `N` (default `10m`)
  - count trigger when unprojected WAL events reach `M` (default `10`)
  - skip when no new WAL records since checkpoint
  - skip when previous round is still running
  - bounded drain each round: `limit=50`, `max_rounds=20`
- `memory.Manager` and markdown formats remain channel-agnostic.

## 5. Injection Behavior

- Telegram subject/session:
  - `subject_id = tg:<chat_id>`
  - `session_id = tg:<chat_id>`
- Telegram request context:
  - `private` chat -> `ContextPrivate`
  - `group/supergroup` -> `ContextPublic`
- Injection content:
  - `ContextPrivate`: long-term summary + recent short-term summaries
  - `ContextPublic`: recent short-term summaries only
- Injection is bounded by `memory.injection.max_items` (default fallback to 50 inside `BuildInjection` when value <= 0).

## 6. Writeback Conditions and Rules

- Writeback runs only when:
  - `publishText == true`
  - memory orchestrator exists
  - `subject_id` is non-empty
- Writeback does not call `memory.draft`; it appends raw events only.
- `source_history` is bounded twice before or at append time:
  - channel-level history cap for prompt/runtime context
  - journal hard cap keeps only the latest 3 history items
- If final reply is lightweight (no text publish), memory write is skipped.
- Telegram participants capture sender + all mention users; participants may be empty when unavailable.

## 7. Draft and Merge Rules

- Draft generation (`memory.draft`) outputs:
  - `summary_items`
  - `promote.goals_projects`
  - `promote.key_facts`
- Promote strictness:
  - Promotion is kept only when explicit “remember/store in memory” intent is detected.
  - At most one promote item is kept.
- Merge strategy moved to projector path:
  - projector replays WAL events and groups by target projection file.
  - when existing summaries exist and semantic resolver is configured, semantic dedupe runs in projection.
  - otherwise direct newest-first merge is used.

## 8. Storage Layout and Data Model

File layout:

```text
memory/
  index.md
  YYYY-MM-DD/
    <sanitized-session-id>.md
```

Notes:

- Short-term path:
  - `memory/YYYY-MM-DD/<session_id>.md`
  - Telegram normal session id: `tg:<chat_id>`
  - Heartbeat session id: `heartbeat`
- Frontmatter stores:
  - `created_at`, `updated_at`, `summary`, `session_id`
  - `contact_id`, `contact_nickname`
- Long-term file path is global `memory/index.md` (`LongTermPath(...)` ignores subject id).

## 9. Telegram Admin Commands

- Telegram `/mem` debug command has been removed.
- Memory inspection should use filesystem artifacts directly (`memory/log/*.jsonl` and `memory/*.md` projections).

## 10. Journal/WAL Runtime Notes

Implementation:

- `memory/log/*.jsonl` stores append-only memory events (source of truth).
- `memory/*.md` is a projection/read model (rebuildable from WAL).

Database analogy:

- `memory/log` = database WAL / raw change log
- `memory/index.md` + `memory/YYYY-MM-DD/*.md` = derived tables/views

### 10.1 First-Principles Goals

- Never lose accepted memory updates.
- Keep write path deterministic and auditable.
- Allow projection rebuild when markdown view is corrupted or outdated.
- Keep implementation minimal and local-first.

### 10.2 Minimal Scope (No Overdesign)

- Single-process append writer.
- JSONL only; one event per line.
- Local rotation only (size based).
- Replay from local logs only.

Explicit non-goals for first iteration:

- no distributed consensus / replication protocol
- no event bus dependency
- no background compaction pipeline
- no schema-registry service
- no multi-node writer arbitration

### 10.3 Layout

```text
memory/
  log/
    since-2026-02-28-0001.jsonl
    since-2026-02-28-0002.jsonl
  index.md
  YYYY-MM-DD/
    <session>.md
```

### 10.4 Write Ordering (WAL Rule)

On each accepted memory update:

1. append event to `memory/log/*.jsonl`
2. flush/sync append result

Hot path does not block on markdown projection. Projection is now auto-triggered by runtime worker (`internal/memoryruntime/worker.go`) and calls `ProjectOnce(limit)` out-of-band.

### 10.5 Rotation and Replay

- Rotate only when file exceeds size threshold.
- Keep monotonic file naming for deterministic replay order.
- File naming carries segment start date for indexing:
  - `since-YYYY-MM-DD-0001.jsonl`
  - `since-YYYY-MM-DD-0002.jsonl`
- Store a small checkpoint (last applied log file + offset/line) for fast restart.
- On startup, replay from checkpoint to rebuild/repair markdown projections.

Compression rule:

- Active segment stays plain `.jsonl`.
- Optional compression applies only to closed old segments as `.jsonl.gz`.
- Do not bundle WAL segments into `tar.gz`.

Checkpoint structure (`memory/log/checkpoint.json`):

```json
{
  "file": "since-2026-02-28-0001.jsonl",
  "line": 18,
  "updated_at": "2026-02-28T06:30:12Z"
}
```

- `file`: last applied segment logical name (always `.jsonl` key).
- `line`: last applied line number in that segment.
- `updated_at`: checkpoint write timestamp (RFC3339, UTC).

### 10.6 Event Shape (Minimal)

Event includes fields needed for replay and audit, for example:

- `schema_version`
- `event_id`
- `task_run_id`
- `ts_utc`
- `session_id`
- `subject_id`
- `channel`
- `participants` (array, multi-party aware, may be empty)
- `task_text`
- `final_output`
- `source_history`
- `session_context`

The schema should evolve conservatively.

`participants` item shape (minimal required):

- `id` (for example `@johnwick`)
- `nickname` (for example `John Wick`)
- `protocol` (for example `tg`)

`participants` may be empty (`[]`) when participant identity is unavailable at write time.

Special case for agent self:

- if `id` is `0` and `protocol` is empty (`""`), this participant means the agent itself.
- recommended `nickname`: agent display name (for example `阿嬷`).

Example event:

```json
{
  "schema_version": 3,
  "event_id": "evt_01JY7K9M7T3H2QZ6A9D5V4N8P1",
  "task_run_id": "run_01JY7K9B3W8F6M2N4C1R0T9X5Q",
  "ts_utc": "2026-02-28T06:15:12Z",
  "session_id": "tg:-1003824466118",
  "subject_id": "tg:-1003824466118",
  "channel": "telegram",
  "participants": [
    {
      "id": "@johnwick",
      "nickname": "John Wick",
      "protocol": "tg"
    }
  ],
  "task_text": "emmm",
  "final_output": "",
  "source_history": [],
  "session_context": {}
}
```

Empty participants is also valid:

```json
{
  "participants": []
}
```

Agent-self participant example:

```json
{
  "participants": [
    {
      "id": 0,
      "nickname": "Miss A.Mo",
      "protocol": ""
    }
  ]
}
```

### 10.7 Async Projection Trigger (When to Run)

Projection is asynchronous by design (not per-event immediate), because merge can involve LLM semantic dedupe.

Implementation:

- Runtime starts one projection worker per process (when memory is enabled).
- Worker triggers projection and calls projector explicitly (`ProjectOnce(limit)`).
- Projector enforces single-call execution (`ProjectOnce` guarded by mutex).
- Startup/restart replay can run the same projector entrypoint repeatedly from checkpoint.

- Auto trigger when either condition is met:
  - timer interval reached (`N` minutes)
  - newly appended WAL events reached threshold (`M` records)
- Skip trigger when:
  - there are no new WAL records since last projection checkpoint
  - previous projection round is still running
- Manual trigger:
  - not provided
- Worker parallelism per round:
  - run with `X` goroutines
  - `X` is derived from UTC-day short-memory file count
  - clamp `X` to at least `1`
- Worker defaults:
  - `N = 10m`
  - `M = 10`
  - `limit = 50`
  - `max_rounds = 20`

### 10.8 Projection Window (How Much Log per Run)

`window` means how much unapplied WAL is consumed in one projection pass.

Projection operates by target file grouping:

- Read WAL by event-count window.
- Group read events by projection target.
- Run one projection per touched target file in that pass.

Target organization uses `event.subject_id` as projection key:

- `memory/YYYY-MM-DD/{sanitize(subject_id)}.md`
- examples:
  - `memory/YYYY-MM-DD/heartbeat.md`
  - `memory/YYYY-MM-DD/tg_-1003824466118.md`

Projection outputs in one pass:

- one short-memory file per touched `(day, subject_id)` bucket
- optional long-term update (`memory/index.md`) when resolved draft contains `promote`

When projecting one target file, summary merge uses all existing summary items in that file.

If backlog exceeds window, projector continues in later passes from updated checkpoint.

### 10.9 Semantic Dedupe (ASCII)

Projection uses one flow:

- merge incoming WAL records with existing short-memory summary items
- run semantic dedupe only when existing summary is non-empty and resolver is configured
- otherwise keep direct merge result

No separate dedupe pipeline is used.

Replay/checkpoint policy:

- At-least-once processing is acceptable (replay may process duplicate events).
- Checkpoint is advanced in batches (for example every 10 processed events).
- On projection error, checkpoint still advances; caller receives the returned error.

```text
Projector                 LLM Semantic Resolver                   FS
    |                               |                             |
    | replay WAL window             |                             |
    |-------------------------------|---------------------------->|
    | load existing short-term      |                             |
    |------------------------------------------------------------>|
    | merge incoming + existing     |                             |
    | (newest-first, normalized)    |                             |
    |                               |                             |
    | existing summary exists?      |                             |
    |-- no --> skip semantic dedupe |                             |
    |                               |                             |
    |-- yes --> SemanticDedupeSummaryItems(items)                |
    |--------------->|                                            |
    |                | select keep indices                        |
    |<---------------|                                            |
    | apply deduped items                                        |
    | write short-term markdown                                  |
    |------------------------------------------------------------>|
```

Notes:

- Semantic dedupe is only called when existing short-term summary is non-empty and resolver is configured.
- If semantic dedupe fails, projection returns error for that batch; checkpoint still advances by this policy.

## 11. Operational Log Keywords

Use these keywords for fast runtime memory troubleshooting:

- Projection worker:
  - `memory_projection_run_error`
  - `memory_projection_error`
- Writeback record:
  - `memory_record_ok`
  - `memory_record_error`
- Injection:
  - `memory_injection_applied`
  - `memory_injection_error`
  - `memory_injection_skipped` (Telegram)
- Telegram writeback path (legacy name kept in code):
  - `memory_update_error`

Quick grep for projection issues:

- `memory_record_ok|memory_projection_error|memory_projection_run_error`

## 12. Replay / Rebuild Runbook

What to check first:

- WAL: `memory/log/*.jsonl`
- Projection: `memory/index.md`, `memory/YYYY-MM-DD/*.md`
- Progress: `memory/log/checkpoint.json`

Safe rebuild procedure:

1. Stop runtime processes (telegram/slack/heartbeat).
2. Back up projection files if needed.
3. Remove checkpoint:

```bash
rm -f memory/log/checkpoint.json
```

4. Start runtime with memory enabled.

Projection worker will replay WAL from the beginning and rewrite projection files.

Verify rebuild:

1. `memory/log/checkpoint.json` is recreated and advances.
2. `memory/index.md` and `memory/YYYY-MM-DD/*.md` are updated.
3. Logs do not show:
   - `memory_projection_run_error`
   - `memory_projection_error`

Notes:

- WAL replay order is deterministic (`file + line`).
- Projection is asynchronous and bounded per trigger round.
- At-least-once replay is allowed; merge handles duplicates.
