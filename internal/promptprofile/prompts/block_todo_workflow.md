[[ Cron Task Workflow ]]
Use this workflow only when you need to schedule something for future work, or remove an existing scheduled task.

Scheduled tasks live in `cron.yaml` under `file_state_dir`. Do not use `TODO.md`, `TODO.DONE.md`, or `TODO.RECUR.md`.

`cron.yaml` task examples:
```yaml
version: 1
tasks:
  - id: submit-report
    at: "2026-05-12 09:00"
    tz: "Asia/Tokyo"
    content: "Remind [John](tg:@johnwick) to submit the report."

  - id: weekly-invoice-review
    cron: "0 10 * * 1"
    tz: "UTC+8"
    content: "Review open invoices."
```

- If a new one-time task is identified, use `todo_update` with action `add_once`. Pass `content`, `at` (`YYYY-MM-DD HH:mm`), optional `tz`, optional `chat_id`, and optional `people`.
- If a new recurring task is identified, use `todo_update` with action `add_recurring`. Pass `content`, `cron` (five numeric fields), optional `tz`, optional `chat_id`, and optional `people`.
- Use exactly one of `at` or `cron`; `at` means one-time, `cron` means recurring.
- If the user states a timezone, write it as an IANA timezone or UTC offset in `tz` (for example `Asia/Tokyo` or `UTC+8`). If no timezone is stated, omit `tz`; the runtime local timezone is used.
- If the user asks to remove a scheduled task, use `todo_update` with action `delete`. Prefer passing `id` when known; otherwise pass precise `content` so the tool can semantically match exactly one task.
- If a cron awareness task is due, handle the task content directly. Do not describe `cron.yaml`, scheduler internals, pending counts, or delivery status to the user.
