# Prompt Notes

## Meta

### Injection mechanism

- `Engine.Run(...)` reads `RunOptions.Meta`.
- If `Meta` is non-empty, it injects one `user` message before the task:
  - `{"mister_morph_meta": <Meta>}`
- The injected payload is capped at 4 KB; oversized payloads are truncated to a stub.

Reference:
- `agent/engine.go`
- `agent/metadata.go`

### Current meta sources and payloads

1. CLI heartbeat run (`mistermorph run --heartbeat`)

Source:
- `cmd/mistermorph/runcmd/run.go`

Payload currently passed as `RunOptions.Meta`:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "trigger": "heartbeat",
    "heartbeat": {
      "source": "cli",
      "scheduled_at_utc": "...",
      "interval": "...",
      "checklist_path": "...",
      "checklist_empty": true
    }
  }
}
```

Note: this path currently nests a heartbeat envelope inside `heartbeat`.

2. Daemon scheduled heartbeat

Source:
- `cmd/mistermorph/daemoncmd/serve.go`
- `internal/heartbeatutil/heartbeat.go`

Payload currently passed as `RunOptions.Meta`:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "source": "daemon",
    "scheduled_at_utc": "...",
    "interval": "...",
    "checklist_path": "...",
    "checklist_empty": true,
    "queue_len": 3,
    "failures": 1,
    "last_success_utc": "...",
    "last_error": "..."
  }
}
```

3. Telegram normal chat task (default path)

Source:
- `cmd/mistermorph/telegramcmd/command.go`

If `job.Meta == nil`, payload currently passed as `RunOptions.Meta`:

```json
{
  "trigger": "telegram",
  "telegram_chat_id": 123,
  "telegram_message_id": 456,
  "telegram_chat_type": "private",
  "telegram_from_user_id": 789
}
```

4. Telegram scheduled heartbeat

Source:
- `cmd/mistermorph/telegramcmd/command.go`
- `internal/heartbeatutil/heartbeat.go`

Payload currently passed as `RunOptions.Meta`:

```json
{
  "trigger": "heartbeat",
  "heartbeat": {
    "source": "telegram",
    "scheduled_at_utc": "...",
    "interval": "...",
    "checklist_path": "...",
    "checklist_empty": true,
    "telegram_chat_id": 123,
    "telegram_chat_type": "group",
    "telegram_from_user_id": 789,
    "telegram_from_username": "alice",
    "telegram_from_name": "Alice",
    "queue_len": 0,
    "last_success_utc": "..."
  }
}
```

5. MAEP inbound auto-reply run

Source:
- `cmd/mistermorph/telegramcmd/command.go`

Payload currently passed as `RunOptions.Meta`:

```json
{
  "trigger": "maep_inbound"
}
```

### Paths that currently do not pass meta

- CLI normal `run` (non-heartbeat) does not set `RunOptions.Meta`.
- Daemon `/tasks` user-submitted jobs do not set `RunOptions.Meta` by default.

