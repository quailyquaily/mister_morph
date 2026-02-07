---
status: draft
---

# Markdown-Based Memory System Requirements

## 1. Background

We are redesigning the agent memory system to be **Markdown file based** for readability, manual editing, and version control.
All memory files live under a configured `memory/` directory.

## 2. Goals

1. **Readable and editable**: markdown is easy to audit and maintain.
2. **Stable layout**: predictable file structure for indexing and loading.
3. **Linkable**: standard markdown links between memory files.
4. **Session-driven updates**: every session writes short-term memory to its own file and merges within that session when needed.
5. **Long-term compactness**: long-term memory retains only the most important items.

## 3. User Model and Scope

**Current model**: a single agent instance with **user-isolated long-term memory**.

- Long-term memory is private and stored per user.
- Short-term memory is public by default and **must not contain private data**.

`subject-id` should use the **existing identity resolution** output (the `SubjectID` value):

- Unbound: `ext:telegram:<user_id>`
- Bound: `acct:<account_id>`

For filesystem paths, the `subject-id` is **sanitized** into a safe directory name (e.g., replace `:` with `_`).
Optionally store the original `subject_id` in frontmatter to preserve the canonical identifier.

## 4. Storage Layout

All files are under the configured `memory/` directory:

- **Long-term memory**: `memory/_longterms/<subject-id>/_index.md`
- **Short-term memory**: `memory/YYYY-MM-DD/<session-id>.md`

Example:

```
memory/
  _longterms/
    acct_42/
      _index.md
  2026-02-04/
    telegram_12345.md
    cli_session_001.md
  2026-02-05/
    telegram_12345.md
```

## 5. File Format (Frontmatter)

Each memory file must include YAML frontmatter metadata.

**Required fields**

- `created_at`: UTC timestamp (ISO 8601)
- `updated_at`: UTC timestamp (ISO 8601)
- `summary`: short summary (short-term memory must update this every write)
- `tasks`: done/total counts for Tasks section (e.g., `2/5`)
- `follow_ups`: done/total counts for Follow Ups section (e.g., `1/3`)

**Optional fields**

- `session_id`: identifier for the session that wrote the content
- `source`: trigger source (e.g., `cli`, `telegram`)
- `channel`: channel/context (reserved)
- `tags`: list of keywords
- `subject_id`: canonical subject id (recommended in long-term files)
- `contact_id`: conversation counterpart id (recommended for contact-driven sessions)
- `contact_nickname`: conversation counterpart nickname (can be empty if unknown)

Example:

```
---
created_at: 2026-02-04T12:34:56Z
updated_at: 2026-02-04T12:34:56Z
summary: "Confirmed directory layout, merge rules, and templates."
session_id: s_20260204_001
source: cli
channel: local
subject_id: acct:42
---
```

## 6. Content Rules

### 6.1 Long-Term Memory (`memory/_longterms/<subject-id>/_index.md`)

- Store only **important, stable, high-signal** items.
- Keep it compact and easy to scan.
- Each entry should include an **added date** suffix (e.g., `(added 2026-02-05)`).
- If the file includes optional **Tasks** / **Follow Ups** sections, frontmatter `tasks` / `follow_ups` are updated from those TODOs.
- Links can reference short-term files for details.
- **Private-only**: must not be used in public contexts (e.g., group chats).

### 6.2 Short-Term Memory (`memory/YYYY-MM-DD/<session-id>.md`)

- Each session writes to its **own** file for that day.
- If similar content already exists for the same session file, **rewrite and merge** instead of duplicating.
- Update `updated_at` and `summary` after every write.
- Session summary entries should include **who**, **when**, **what happened**, and the **result** (if any).
- 对 contacts 相关会话，memory 写入必须包含联系人 `contact_id` 与 `contact_nickname`（`contact_nickname` 可为空，`contact_id` 不可为空）。
- When tasks are completed in a later session, scan recent short-term files and update matching TODO status.
- **Public-by-default**: do not write private information here.

## 7. Merge Rules (Same-Session Short-Term)

Primary strategy: **semantic merge** (LLM-assisted), with a rule-based fallback.

Semantic merge:

- Merge items that describe the same concept even if wording differs.
- Prefer the most recent or corrected information when conflicts occur.
- Tasks keep the latest completion status.
- Never add private/sensitive info (short-term is public).

Fallback (rule-based) strategy:

1. **Template section merge**: only merge within the same section (see templates).
2. **Title-based merge**:
   - Prefer `- **Title**: content` entries.
   - If titles match (case- and whitespace-insensitive), treat as same item.
   - Merge by **replacing with the new content**, optionally adding a short “updated to …” note.
3. **TODO merge**:
   - Tasks use `- [ ] ...` or `- [x] ...`.
   - If task text matches, keep the latest status (unchecked -> checked).
4. **Link-target merge**:
   - If an entry contains the same link target (same relative path or anchor), treat as same item.
5. **No match**: append as new item.

Optional upgrade (not required now): simple similarity checks using normalized text + keyword overlap.

## 8. Linking Rules

- Use standard markdown links: `[text](relative/path.md)` or `[text](#anchor)`.
- Links are relative to the `memory/` root.
- Use stable headings to support anchors.

## 9. Session Lifecycle

On session start (agent wake):

1. Load current user long-term memory from `memory/_longterms/<subject-id>/_index.md`.
2. Load recent short-term memory (default 7 days; configurable).
3. Always load **today’s short-term file for the current session** if present.

On session end (or after a batch):

1. Summarize the session into today’s short-term file (per session id).
2. Merge with existing same-session content using the rules above.
3. Update `updated_at` and `summary`.
4. Scan recent short-term files and update matching task statuses.
5. Promote key items into long-term memory (current user scope).

## 10. Long-Term Selection Rules

Strict selection (very conservative):

Include **only** if all are true:

1. The user explicitly asks to remember it (or to add to long-term memory).
2. It is **precious and long-lived** (weeks/months), not a transient detail.
3. It is **not** a short-term task, one-off detail, or time-bound item.

Hard limits:
- **At most 1 item per session** can be promoted.
- If uncertain, **skip promotion**.

Exclude (stored elsewhere, not in long-term memory):

- Profile / identity
- Stable preferences
- Constraints / bans

Capacity guidance:

- Limit to **~100 items** or **~3000 characters**.
- When exceeding, remove items with low reuse and low recency.

## 11. Injection Format (Prompt)

When `memory.injection.enabled = true`:

1. **Inject summaries only** (never full body content).
2. Order:
   - Long-term summaries first (private contexts only)
   - Recent short-term summaries (most recent first)
3. Short-term entries must include **file path references** (relative to `memory/`).
4. Long-term summaries must **not** include file paths to avoid exposing user identifiers.
5. Apply `memory.injection.max_items` as the only hard limit.

Recommended injection block:

```
[Memory:LongTerm:Summary]
- <summary line 1>
- <summary line 2>

[Memory:ShortTerm:Recent]
- 2026-02-04: <summary> (2026-02-04/telegram_12345.md) [progress: tasks 1/3, follow_ups 0/1]
- 2026-02-03: <summary> (2026-02-03/telegram_12345.md) [progress: tasks 0/0, follow_ups 0/0]
```

## 12. Templates

### 12.1 Long-Term Template (`memory/_longterms/<subject-id>/_index.md`)

```
---
created_at: 2026-02-04T12:34:56Z
updated_at: 2026-02-04T12:34:56Z
summary: "Most important and stable long-term facts and project state."
tasks: "0/0"
follow_ups: "0/0"
subject_id: acct:42
---

# Long-Term Memory

## Long-Term Goals / Projects
- **Project A**: ... (added 2026-02-05)

## Key Facts
- **Key fact**: ... (added 2026-02-05)
```

### 12.2 Short-Term Template (`memory/YYYY-MM-DD/<session-id>.md`)

```
---
created_at: 2026-02-04T12:34:56Z
updated_at: 2026-02-04T12:34:56Z
summary: "Discussed memory layout and merge rules."
tasks: "1/3"
follow_ups: "0/1"
session_id: s_20260204_001
source: cli
channel: local
contact_id: tg:@alice
contact_nickname: Alice
---

# 2026-02-04 Short-Term Memory

## Session Summary
1. **Topic**: ...
   - Users: ...
   - Datetime: ...
   - Event: ...
   - Result: ...

## Temporary Facts
1. Example:
   - 网站 URL: ...
   - API 示例: ...

## Tasks
- [ ] ...

## Follow Ups
- [ ] ...

## Related Links
- [Previous day](../2026-02-03/telegram_12345.md)
```

## 13. Configuration (Proposed)

- `memory.enabled`: enable/disable memory
- `memory.dir`: memory root path (default: `~/.morph/memory`, systemd: `/var/lib/morph/memory`)
- `memory.short_term_days`: number of recent days to load on session start (default 7)
- `memory.injection.enabled`: enable prompt injection
- `memory.injection.max_items`: max injected items

## 14. Implementation TODOs

- [x] Remove SQLite-based memory storage and related wiring.
- [x] Implement markdown file storage under `memory/` with the new layout.
- [x] Add subject-id directory mapping (sanitize `SubjectID` to path-safe).
- [x] Implement frontmatter parsing/writing with required fields and summary updates.
- [x] Implement short-term merge rules (section/title/TODO/link matching).
- [x] Write short-term memory per session file and sync completed TODOs across recent sessions.
- [x] Implement long-term selection/promotion rules and capacity limits.
- [x] Append added-date suffix to new long-term entries.
- [x] Implement session startup load (long-term + recent short-term).
- [x] Implement injection rendering (summaries + short-term path refs only).
- [x] Update configuration schema and defaults (remove `memory.injection.max_chars`).
- [ ] Add read/write abstraction (optional: `read_file` / `write_file` tools).
- [x] Update docs/examples to reflect markdown-based memory.

## 15. I/O Constraints

- Reading/writing memory files may use `read_file` / `write_file` tools when appropriate.
- All memory I/O must stay within `memory.dir`.

## 16. Assumptions

- **Single-process writes**: no concurrent writes, no file locking required.
- If multi-process or multi-user expansion is introduced later, add locks or per-user directories.
