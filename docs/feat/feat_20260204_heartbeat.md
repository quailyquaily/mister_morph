---
date: 2026-02-04
title: Agent Heartbeat (Periodic Awareness)
status: draft
---

# Feature: Agent Heartbeat (Periodic Awareness / Checkpoint)

## Summary
Add a lightweight, configurable **heartbeat** system for long-running agent deployments (daemon/Telegram) that acts as a **periodic awareness/checkpoint**. Each heartbeat can review a checklist + context, optionally do small actions, and **always emits a short summary** for visibility.

This is a low-noise “wake up and check” loop, not a strict cron and not limited to health checks.

## Goals
- Provide **periodic awareness** runs for daemon and Telegram modes.
- Allow **checklist-driven** reviews (inbox, queues, reminders, etc.).
- Allow **normal tool/skill usage** during heartbeat (same capabilities as regular runs).
- Avoid chat spam by **suppressing OK responses**.
- Make heartbeat results visible to operators (logs, Telegram, or future webhook).

## Non-Goals
- Exact-time scheduling (cron replacement).
- Full metrics/observability stack.
- High-frequency monitoring (seconds-level probes).
- Long-running workflows.
- Automatic remediation (restart, rollout, etc.).

## Positioning: Heartbeat vs Cron
- **Heartbeat** is “natural time”: it may drift with load and focuses on periodic awareness + low noise.
- **Cron** is exact-time scheduling for deterministic jobs.
- **Isolation** (main vs isolated session) is a cron decision; heartbeat runs in the main session context.

## Design

### 1) Config Surface (New)
Add a top-level `heartbeat` section in `config.yaml`:

```yaml
heartbeat:
  enabled: true
  # Interval between checks. "0m" disables.
  interval: "30m"

  # Optional checklist file. If set, its contents become the heartbeat task.
  # Default: ~/.morph/HEARTBEAT.md
  checklist_path: "~/.morph/HEARTBEAT.md"
```

Notes:
- `interval: "0m"` fully disables heartbeats.
- `enabled: true` by default (set false to hard-disable regardless of interval).

Defaults (no explicit config needed):
- **Delivery**: Telegram heartbeat alerts go to the same chat; daemon alerts go to logs.
- `checklist_path` defaults to `~/.morph/HEARTBEAT.md`.

### 2) Heartbeat Scheduler (Controller Layer)
Implement a small scheduler in long-running runtimes:
- **Daemon** (`cmd/mistermorph/daemoncmd/serve.go`): run a goroutine with `time.Ticker` that enqueues heartbeat runs into the task queue (respect `max_queue`).
- **Telegram / Slack** (`cmd/mistermorph/telegramcmd/command.go`, `cmd/mistermorph/slackcmd/command.go`): run a heartbeat runtime alongside the channel runtime; alerts are delivered via the runtime notifier.

Behavior:
- If the agent is already saturated (e.g., queue full), skip with log.
- If the previous heartbeat is still running, skip to avoid piling up.
- The embedded admin server also exposes `POST /poke` as a manual wake trigger. It runs one heartbeat immediately when idle, and returns `409 Conflict` if a heartbeat is already in progress.

### 3) Heartbeat Task Contract
Heartbeats are normal agent runs with **special metadata** and a strict response contract.

**Injected meta** (via `RunOptions.Meta`):
```json
{
  "mister_morph_meta": {
    "trigger": "heartbeat",
    "heartbeat": {
      "source": "daemon",
      "scheduled_at_utc": "2026-02-04T12:00:00Z",
      "interval": "30m",
      "channel": "telegram"
    }
  }
}
```

**Task text** (fixed):
```
You are running a heartbeat checkpoint for the agent.
Review the provided checklist and context. Always respond with a short summary of what you checked/did.
If anything requires user attention or action, make that explicit in the summary.
```

**System prompt rule** (new):
- If `mister_morph_meta.heartbeat` is present, always return a concise summary (no placeholders).
- Keep summaries short and action-oriented.

### 4) Inputs & Signals (Minimal)
Heartbeats should rely on **lightweight, local inputs** supplied by the controller:
- Optional **checklist file** (`HEARTBEAT.md`).
- Recent short-term progress (only if TODOs exist; included as a progress snapshot).
- Last successful heartbeat time.
- Consecutive failure count.
- Queue length (daemon) or per-chat backlog (telegram).
- Guard approval backlog (pending approvals count).
- Last tool error / LLM error (if recorded).
- Optional wake payload preview from `POST /poke` (small textual preview only; untrusted context).

This data should be **passed via meta** rather than retrieved via tools.

Example snapshot payload:
```json
{
  "heartbeat": {
    "uptime_sec": 86400,
    "queue_len": 4,
    "last_success_utc": "2026-02-04T11:30:00Z",
    "last_error": "",
    "pending_approvals": 1,
    "checklist_path": "HEARTBEAT.md"
  }
}
```

### 5) Output Handling (Summary)
- Always deliver the heartbeat summary to the default target.

### 6) Delivery Targets
- **Telegram**: send alert to the same chat that the heartbeat is associated with.
- **Daemon**: write alert to logs (`heartbeat_alert`).

### 7) Failure Handling
- If heartbeat run fails (LLM error, timeout), increment failure count.
- After repeated failures (implementation-defined threshold), emit a short error summary.

### 8) Checklist File (Optional)
If `checklist_path` is set, load the file and use it as the heartbeat task.

Recommended format for `HEARTBEAT.md` (short, action-oriented, no secrets):
```
# Heartbeat Checklist

- Check inbox for urgent items; if any, summarize in ALERT.
- Check upcoming calendar items (24h); alert if conflicts or prep needed.
- Check pending approvals; alert if backlog > 5.
- If a safe quick fix is possible, do it; otherwise alert.
```

Behavior details:
- If the checklist is missing or effectively empty, skip the heartbeat task.
- Keep the checklist short; recommended max length: **100 lines**.
- Prefer **self-resolving** actions. Avoid asking the user unless it is genuinely blocked.

### 5) Manual Wake Endpoint

- `POST /poke` is the manual wake endpoint.
- It is authenticated with `server.auth_token`.
- The request body is required and becomes the poke task text.
- Empty or non-text request bodies return `400 Bad Request`.
- If awareness is already running, the server returns `409 Conflict`; callers should retry later.

## Open Questions
- Should poke tasks also be persisted into a dedicated audit stream beyond normal logs and awareness meta?

## TODO
- [x] Add `heartbeat` config section + defaults in `assets/config/config.example.yaml` and `cmd/mistermorph/defaults.go` (enabled/interval/checklist_path with `~/.morph/HEARTBEAT.md` default).
- [x] Add heartbeat task builder (new helper in `cmd/mistermorph/`).
- [x] Read checklist file (use `internal/pathutil.ExpandHomePath`).
- [x] Detect empty/whitespace-only content (treat comment-only as empty).
- [x] Return fixed heartbeat task text + optional checklist block.
- [x] Add a heartbeat-specific rule in `agent.DefaultPromptSpec` (strict OK/ALERT contract; no extra text; use tools/skills as normal).
- [x] Daemon: add a `HeartbeatManager` in `cmd/mistermorph/serve.go`.
- [x] Ticker loop that enqueues heartbeat tasks.
- [x] Skip if queue is full or awareness is already running.
- [x] Log `heartbeat_ok` / `heartbeat_alert` results.
- [x] Daemon queue plumbing.
- [x] Extend `queuedTask` to carry `meta map[string]any` and a `heartbeat bool` (internal only).
- [x] Add `TaskStore.EnqueueHeartbeat(...)` (not exposed via HTTP) to enqueue with meta.
- [x] Telegram: schedule per-chat heartbeats in `cmd/mistermorph/telegram.go`.
- [x] Track last activity per chat.
- [x] Enqueue heartbeat jobs on the chat worker channel.
- [x] Skip if the worker queue is busy or awareness is already running.
- [x] Telegram heartbeat jobs.
- [x] Extend `telegramJob` with `IsHeartbeat bool`.
- [x] In `runTelegramTask`, set meta `trigger=heartbeat` + chat info.
- [x] Always send the heartbeat summary and append to history.
- [x] Heartbeat meta snapshot.
- [x] Include `scheduled_at_utc`, `interval`, `source`, `chat_id` (telegram), queue length (daemon), last success/error.
- [x] Inject via `RunOptions.Meta`.
- [x] Checklist fallback.
- [x] If checklist missing/empty, use short-term memory (when enabled) + current context to find next actions.
- [x] If nothing emerges, return a short summary stating no action needed.
- [ ] Tests:
- [ ] Heartbeat task builder (empty checklist detection).
- [ ] OK suppression vs ALERT delivery (telegram + daemon).
- [ ] Meta injection includes `trigger=heartbeat`.
