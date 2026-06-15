---
title: Memory
description: Introduces the journal-based memory architecture, projection, and injection rules.
---

# Memory

Mister Morph memory writes accepted events to the unified domain journal first, then projects them asynchronously.

## Journal

In plain terms, accepted memory events are written as JSONL records in order. The unified journal is the source of truth.

The journal lives under `<file_state_dir>/journal/`.

When the current segment reaches the configured size, the next append opens a new stable segment such as `events.000000000000000002.jsonl`.

## Projection

Mister Morph builds simple projections from journal events into two targets:

- `memory/index.md` (long-term memory)
- `memory/YYYY-MM-DD/*.md` (short-term memory)
  - Short-term memory files are isolated by channel. For example, memories from different Telegram group chats do not mix.

Projection means reading journal events, summarizing them with an LLM, and writing the result into those target files.

The projection checkpoint file is `memory/projection_checkpoint.json`, and it looks roughly like this:

```json
{
  "file": "events.000000000000000001.jsonl",
  "line": 18,
  "byte": 4096,
  "updated_at": "2026-02-28T06:30:12Z"
}
```

## Injection

When the following config is enabled, part of the projected memory is injected into the prompt:

- `memory.enabled = true`
- `memory.injection.enabled = true`

`memory.injection.max_items` controls the maximum number of injected items.

## Notes

1. If memory projections are damaged, rebuild them from the journal by deleting `memory/projection_checkpoint.json` and letting the Agent continue running.
2. In production, keep `file_state_dir` on persistent storage so runtime state, including memory, survives.
