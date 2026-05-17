---
title: TODO and Heartbeat
description: How cron.yaml and HEARTBEAT.md let the agent track work outside the current chat.
---

# TODO and Heartbeat

MisterMorph provides two awareness mechanisms: Heartbeat and TODOs. Heartbeat is a simple fixed wake-up behavior. TODOs let you schedule work that should run once in the future or repeat on a precise rule.

## TODOs

One-off or recurring scheduled work is better stored as a TODO.

You can edit `cron.yaml` directly, edit it through the Console GUI, or let the agent use the `todo_update` tool when it needs to add or remove a TODO.

Each TODO contains a title, schedule rule, timezone, and content. The content is the text actually handed to the agent, and it can include contact references such as `[John](tg:@john)`.

### Due Execution

The agent checks `cron.yaml` once per minute. Due TODOs run as awareness tasks with `behavior=cron`.

Details:

- One-time TODOs are deleted after a successful run. If a run fails or times out, the TODO is kept and retried later.
- Recurring TODOs stay in the list.

> Notes:
>
> 1. If the agent misses multiple trigger times, it does not replay each missed TODO run after recovery.
> 2. If the same TODO is already queued or running, that trigger is skipped.

### Configuration

TODOs are stored in `file_state_dir/cron.yaml`.

The basic `cron.yaml` structure is:

```yaml
version: 1
tasks:
  - id: submit-report
    title: Submit report
    at: "2026-05-12 09:00"
    tz: "Asia/Tokyo"
    content: "Remind [John](tg:@john) to submit the report."

  - id: weekly-invoice-review
    title: Invoice review
    cron: "0 10 * * 1"
    tz: "UTC+8"
    content: "Review open invoices."
```

Fields:

- `id`: stable identifier used for deduplication, update, and deletion.
- `title`: TODO title.
- `at`: execution time for a one-time TODO, in `YYYY-MM-DD HH:mm` format.
- `cron`: five-field cron expression for a recurring TODO.
- `tz`: IANA timezone or fixed UTC offset, such as `Asia/Tokyo` or `UTC+8`.
- `content`: task content passed to the agent when the TODO is due.
- `chat_id`: optional channel context, kept only as metadata and not used for routing.

Exactly one of `at` and `cron` must be set. Use `at` for a one-time TODO; use `cron` for a recurring TODO.

## Heartbeat

If Heartbeat is enabled in config, each heartbeat tick works like this:

1. The agent reads `HEARTBEAT.md`.
2. If `HEARTBEAT.md` is not empty, its content becomes the heartbeat task.
3. The agent handles that task.

If `HEARTBEAT.md` is empty, no agent task is started.

Heartbeat does not read TODOs. TODOs are triggered independently by the cron service reading `cron.yaml`.

### HEARTBEAT.md

`HEARTBEAT.md` is the standing instruction for each heartbeat. It describes what the agent should check on every heartbeat.

Good uses:

- Look for due follow-ups.
- Inspect routine files.

For the tool that updates TODOs, see [`todo_update`](/guide/built-in-tools#todo_update). For state directory placement, see [Filesystem Roots](/guide/filesystem-roots).
