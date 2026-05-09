---
title: TODO and Heartbeat
description: How TODO files and HEARTBEAT.md let the agent track work outside the current chat.
---

# TODO and Heartbeat

Heartbeat is the timer behavior of the awareness runtime. Each heartbeat run creates a fresh runtime task from `HEARTBEAT.md` and does not include chat history.

`POST /poke` uses the same awareness runtime lock, but it is a separate behavior. A poke does not read `HEARTBEAT.md`; its request body becomes the task text. Empty poke bodies are rejected with `400 Bad Request`.

## Heartbeat Flow

On each heartbeat tick:

1. The runtime reads `HEARTBEAT.md`.
2. If `HEARTBEAT.md` is not empty, its content becomes the heartbeat task.
3. If the heartbeat task has content, the agent handles it with normal tools.

If `HEARTBEAT.md` is empty, no agent task is started.

Heartbeat no longer reads `TODO.md` or expands `TODO.RECUR.md`.

## Poke Flow

On authenticated `POST /poke`:

1. The server reads a small textual preview of the request body.
2. If the body is empty or not usable as text, the server returns `400 Bad Request`.
3. The poke body becomes the task text.
4. The task runs without chat history and without `HEARTBEAT.md`.

## HEARTBEAT.md

`HEARTBEAT.md` is the standing instruction for each heartbeat. It should describe what the agent should check, not a one-off user request.

Good uses:

- Look for due follow-ups.
- Inspect routine files.
- Send reminders based on files or service state that the heartbeat instructions explicitly read.

Avoid putting one-off tasks directly into `HEARTBEAT.md`. Put those in `TODO.md`, or use `TODO.RECUR.md` if they repeat.

## TODO Flow

TODO files hold concrete work. The `todo_update` tool writes and completes TODO records during normal agent tasks that include the TODO workflow. Heartbeat and poke tasks do not automatically include the TODO workflow.

There are three TODO files.

### TODO.md

`TODO.md` contains work that should happen once:

```text
- [ ] [Created](2026-05-01 12:41), [ChatID](tg:-100123) | Remind [John](tg:@john) to submit report.
```

Use `TODO.md` for reminders and tasks that should be handled once.

### TODO.DONE.md

`TODO.DONE.md` contains completed one-off todos. When `todo_update` completes a `TODO.md` item, it moves the record here.

Recurring todos do not move to `TODO.DONE.md`.

### TODO.RECUR.md

`TODO.RECUR.md` contains repeat rules:

```text
- [ ] [Next](2026-05-07 15:00), [Repeat](weekly), [TZ](Asia/Tokyo) | Play tennis.
- [ ] [Next](2026-05-02 09:00), [Repeat](every 6 hours) | Check the report queue.
```

Supported repeat values:

- `daily`
- `weekly`
- `every N days`
- `every N hours`

`TZ` is optional. If it is omitted, the runtime local timezone is used.

Recurring records stay in `TODO.RECUR.md`. Heartbeat does not currently copy due recurring records into `TODO.md`.

## Choosing the File

| Need | File |
|---|---|
| Tell the agent what to check on each heartbeat | `HEARTBEAT.md` |
| Do this once | `TODO.md` |
| Keep a record of completed one-off todos | `TODO.DONE.md` |
| Do this repeatedly | `TODO.RECUR.md` |

For the tool that updates todo files, see [`todo_update`](/guide/built-in-tools#todo_update). For state directory placement, see [Filesystem Roots](/guide/filesystem-roots).
