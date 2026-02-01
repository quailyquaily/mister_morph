---
status: done
---

# Long-Term Memory Requirements

## 1. Background

The agent needs a **long-term (persistent) memory** capability to retain user-related information across interactions (facts, preferences, profile, project/task state, etc.).
For now, **short-term memory** stays **in-memory only** and is **not persisted**.

The current integration target is **Telegram** only. The design must **anticipate future channels** (e.g., Slack) and **future account registration + multi-channel binding**, where multiple external identities belong to the same real person.

## 2. Goals

1. When a request has a **usable user identity**, the agent can:

* **Read** long-term memory for context building
* **Write/update** long-term memory as needed

2. When a request has **no user identity** (e.g., CLI or localhost API calls):

* Memory is **disabled** (no reads, no writes)

3. The identity model is **forward-compatible**:

* Without registration/binding: memory is isolated per channel user (e.g., Telegram user A vs B)
* With account binding: multiple external identities map to the same canonical subject and can share memory

4. **Privacy requirement**:

* Some memories are **private-only** and must **never be referenced in public contexts** (e.g., group chats).

## 3. Scope

### In Scope (current implementation)

* Long-term memory storage (KV/structured JSON stored as text)
* Telegram identity resolution
* Privacy visibility control (private-only vs public-safe)
* Memory disabled path for identity-less channels (Noop store)
* Data model and interfaces that reserve room for Slack and account binding

### Out of Scope (for now)

* Persisting conversation/event logs (short-term memory persistence)
* Vector search / ANN / embeddings
* Automatic cross-channel identity inference (without explicit binding)
* Automatic TTL expiration (manual cleanup only)
* Background asynchronous jobs (migration/compaction can be added later)

## 4. Definitions

* **External Identity**: channel-provided user identity (e.g., Telegram user ID)
* **external_key**: normalized external identity key:
  `"<channel>:<external_user_id>"`

  * Example: `telegram:123456789`
  * Slack reserved example: `slack:<team_id>:<user_id>`
* **Canonical Subject**: the entity that memory is stored under (a “person” / “account” / “subject”)
* **subject_id**: canonical subject identifier

  * Unregistered/unbound fallback: `ext:<external_key>`
  * Registered/bound: `acct:<account_id>`
* **Visibility**: per-memory access control based on context:

  * `public_ok`: safe to use/mention in public contexts (e.g., group chat)
  * `private_only`: may only be used in private contexts (e.g., DM); must not be used/mentioned in public contexts
* **Memory Disabled**: when no external identity can be resolved, all memory operations are no-ops.

## 5. Core Requirements

### 5.1 Identity resolution and enable/disable rule

For each incoming request, the system must resolve:

* `enabled` (bool)
* `external_key` (string, if enabled)
* `subject_id` (string, if enabled)

#### Telegram (current)

* If Telegram user identity is unavailable: `enabled=false`
* Else:

  * `external_key = "telegram:<user_id>"`
  * Lookup `identity_links`:

    * If found: `subject_id = mapped_subject_id` (typically `acct:<id>`)
    * Else: `subject_id = "ext:" + external_key`

#### CLI / localhost API (no identity)

* `enabled=false` (no reads, no writes)

### 5.2 Memory visibility and public context constraint

The agent must determine a **request context**:

* `private` (DM / private chat)
* `public` (group chat / public setting)
* (optional) `unknown` (treat as public-safe only)

**Read filtering rules**

* In `private` context: allow `public_ok` and `private_only`
* In `public` context: allow **only** `public_ok`
  Private-only memory must be **completely excluded from reads** in public contexts (not even for internal reasoning).

**Write default**

* Default visibility for newly extracted/stored memory is **`private_only`**.
  It should only be set to `public_ok` if the user explicitly indicates it is safe to be used publicly.

### 5.3 Long-term memory data model and categories

Supported namespaces (initial set):

* `profile`
* `preference`
* `fact`
* `project`
* `task_state`

### 5.4 Write strategy: overwrite allowed (UPSERT)

For `(subject_id, namespace, key)`:

* Writes are **UPSERT** (insert or update in place)
* No mandatory history retention in Phase 1

### 5.5 Manual cleanup

No automatic TTL expiry. Provide manual cleanup methods, e.g.:

* delete a specific key
* delete by namespace
* wipe all memory for a subject (optional)
* database maintenance hooks (optional)

## 6. Future Requirements (Reserved)

### 6.1 Add Slack channel

* Implement Slack identity extraction
* Generate `external_key = "slack:<team_id>:<user_id>"`
* Reuse the same resolution pipeline
* No schema changes required

### 6.2 Account registration and multi-channel binding

When accounts exist, multiple external identities can map to one canonical subject:

* `telegram:<id> -> acct:<account_id>`
* `slack:<team_id>:<user_id> -> acct:<account_id>`

## 7. Binding history handling: Strategy A (Lazy merge)

This project adopts **Strategy A** for account binding:

### 7.1 Binding action

* Only write mapping in `identity_links`
* Do **not** migrate historical data from `ext:*` subjects to `acct:*`

### 7.2 Post-binding writes

* All new writes go to `acct:<account_id>`

### 7.3 Post-binding reads (recommended default: Union read)

Two possible read modes:

* **A1: Canonical-only read**: read only `acct:<account_id>`
* **A2: Union read (recommended default)**: read:

  * `acct:<account_id>` and
  * all linked `ext:<external_key>` subjects (for external identities mapped to the account)

**Conflict resolution for union read**
For the same `(namespace, key)`:

1. Prefer `acct:*` record if present
2. Otherwise prefer the record with the latest `updated_at`

**Visibility filtering applies after union**:

* In public context: only `public_ok`
* In private context: `public_ok + private_only`

## 8. Storage Engine and Database Constraints

* Storage: SQLite (pure Go driver via modernc)
* ORM: GORM + gorm/gen

Recommended SQLite pragmas (implementation guidance):

* WAL mode
* busy timeout
* foreign keys enabled

Connection pooling should be conservative due to seeialized write behavior (implementation detail).

## 9. Schema (SQLite)

### 9.1 Table: `memory_items`

Stores the latest state per `(subject_id, namespace, key)`.

Fields:

* `subject_id` TEXT NOT NULL
* `namespace` TEXT NOT NULL
* `key` TEXT NOT NULL
* `value` TEXT NOT NULL (JSON string or plain text)
* `visibility` INTEGER NOT NULL (0=public_ok, 1=private_only)
* `confidence` REAL NULL (0..1)
* `source` TEXT NULL (message id / request id / etc.)
* `created_at` INTEGER NOT NULL (unix seconds)
* `updated_at` INTEGER NOT NULL (unix seconds)

Constraints and indexes:

* `UNIQUE(subject_id, namespace, key)`
* `INDEX(subject_id, namespace, visibility, updated_at)`

Notes:

* JSON is stored as TEXT; parsing is application responsibility.

### 9.2 Table: `identity_links` (reserved for future binding)

Maps external identity to canonical subject.

Fields:

* `external_key` TEXT PRIMARY KEY
* `subject_id` TEXT NOT NULL (typically `acct:<id>`)
* `created_at` INTEGER NOT NULL
* `updated_at` INTEGER NOT NULL

Indexes:

* `INDEX(subject_id)` for reverse lookup (union reads)

## 10. Application Interfaces

### 10.1 IdentityResolver

`Resolve(request) -> (enabled, external_key, subject_id, aliases[])`

* `enabled=false`: memory disabled; caller should use Noop store
* `subject_id`: canonical subject for reads/writes
* `aliases[]`: optional list of alias subjects for union reads (e.g., `ext:telegram:...`)

### 10.2 MemoryStore

Operations are always expressed in terms of `subject_id` (and optional alias subjects for union reads).

* `Get(subject_id, namespace, key, context) -> (value, ok)`
* `Put(subject_id, namespace, key, value, opts)`

  * UPSERT overwrite
  * opts: `visibility`, `confidence`, `source`
* `List(subject_id, namespace, prefix?, context, limit, cursor?) -> []items`
* `Delete(subject_id, namespace, key)`
* `Cleanup(subject_id?, namespace?, ...)` (manual cleanup hooks)

### 10.3 NoopMemoryStore

Used when memory is disabled:

* Reads return empty
* Writes/Deletes are no-ops

## 11. Telegram-specific notes (current)

* Memory isolation is per **user**, not per chat.
* Privacy is enforced via `visibility` and request context (public vs private), not via including chat_id in subject identity.

### 11.1 Telegram commands for memory (implemented)

These are Telegram bot commands (not LLM tools):

- `/id`: returns `chat_id` and `chat.type` (allowed even when `telegram.allowed_chat_ids` is configured, so you can discover your private chat id).
- `/mem`: list current user's long-term memory items (private chat only).
- `/mem del <id>`: delete one item by id (private chat only).
- `/mem vis <id> <public|private>`: change one item's visibility (private chat only).

Notes:

- `id` is SQLite `rowid` for `memory_items` (scoped to the current `subject_id`).
- `/mem*` requires `memory.enabled=true` and still respects `telegram.allowed_chat_ids`.

## 12. Assumptions

* Expected memory scale: ~1k items per subject
* Overwrite is acceptable; no audit history in Phase 1
* Privacy defaults to safest behavior: `private_only` by default; excluded from public context reads.

## 13. Todo

WIP: finalize and review.

---

# Implementation Plan (Phase 1)

This section turns the requirements above into a concrete, implementable plan for **Phase 1**, with minimal changes to the existing agent core and a clean extension surface for future channels/binding.

## 14. Design choices (key decisions)

### 14.1 Storage + ORM choice: **Use GORM + gorm/gen in Phase 1**

This repo will implement long-term memory with **GORM** and **gorm/gen** code generation for typed queries.

**Decision (Phase 1):**

- Use **SQLite** + **GORM** for schema + CRUD.
- Use **gorm/gen** to generate query code (checked into the repo).
- Keep the public storage surface behind a `MemoryStore` interface so provider/channel code does not depend on GORM types.

**SQLite driver choice:**

Use **pure-Go** SQLite via `modernc.org/sqlite`, through the GORM dialector `github.com/glebarez/sqlite` (which is based on modernc).

### 14.2 Where memory logic lives

- Channel adapters (current: Telegram) are responsible for:
  - Extracting identity + request context (public/private)
  - Constructing a per-request `MemoryView` (already filtered for public/private)
  - Injecting memory into the system prompt (as a prompt block)
  - Registering a per-request memory tool (optional) bound to the resolved identity/context
- `agent/` remains mostly unchanged; it already supports:
  - Custom prompt builder (`agent.WithPromptBuilder(...)`)
  - Per-run tool registry injection

## 15. Proposed directory / package layout

Add two new top-level packages: a shared `db/` for database initialization + schema/query codegen, and `memory/` for the long-term memory feature.

```
db/
  config.go           # DB config parsing helpers (from viper config)
  open.go             # Open *gorm.DB from driver+DSN; set pool options
  sqlite_pragmas.go   # SQLite pragmas (WAL, busy_timeout, foreign_keys)
  migrate.go          # Central AutoMigrate coordinator (modules register models)
  models/             # All DB tables' GORM models live here (not per-feature)
    memory_item.go    # memory_items
    identity_link.go  # identity_links
    # future: user.go, ...
  gen/                # gorm/gen generator entrypoint(s)
    main.go
  query/              # gorm/gen output (committed)
    ...

memory/
  types.go            # Visibility, RequestContext, Item, options
  identity.go         # IdentityResolver + Telegram resolver implementation (or kept in cmd/)
  store.go            # MemoryStore interface + query options
  noop_store.go       # NoopMemoryStore
  gorm_store.go       # GORM implementation; visibility filtering helper
  tool.go             # tools.Tool implementation: memory_get/put/list/delete (optional)
```

Integration points (existing):

- `cmd/mister_morph/telegram.go` wires memory enabled/disabled, prompt injection, and tool registration.
- `cmd/mister_morph/serve.go` and `cmd/mister_morph/run.go` keep memory **disabled** (no identity).
- `cmd/mister_morph/registry.go` may remain unchanged; Telegram can build a registry and then register the memory tool per-request (or build a new registry factory).

## 16. Interfaces (Go) and data flow

### 16.1 RequestContext and Visibility

```go
package memory

type RequestContext string

const (
	ContextUnknown RequestContext = "unknown"
	ContextPublic  RequestContext = "public"
	ContextPrivate RequestContext = "private"
)

type Visibility int

const (
	PublicOK    Visibility = 0
	PrivateOnly Visibility = 1
)
```

### 16.2 Identity resolution

```go
package memory

type Identity struct {
	Enabled     bool
	ExternalKey string   // e.g. "telegram:123"
	SubjectID   string   // e.g. "ext:telegram:123" or "acct:42"
	// Reserved for future union reads / binding:
	// Aliases []string
}

type IdentityResolver interface {
	ResolveTelegram(userID int64) (Identity, error)
	// Reserved: ResolveSlack(teamID, userID string) ...
}
```

**Telegram rules (Phase 1):**

- If `msg.From == nil` ⇒ `Enabled=false`
- Else:
  - `ExternalKey = fmt.Sprintf("telegram:%d", msg.From.ID)`
  - Look up `identity_links.external_key`:
    - found ⇒ `SubjectID = subject_id` (likely `acct:<id>`)
    - not found ⇒ `SubjectID = "ext:" + ExternalKey`
  - Phase 1 does not use union reads; canonical subject only.

### 16.3 Memory store surface

```go
package memory

type Item struct {
	SubjectID   string
	Namespace   string // profile|preference|fact|project|task_state
	Key         string
	Value       string // JSON string or plain text
	Visibility  Visibility
	Confidence  *float64
	Source      *string
	CreatedAt   int64 // unix seconds
	UpdatedAt   int64 // unix seconds
}

type ReadOptions struct {
	Context RequestContext // public/private/unknown
	Limit   int
	Prefix  string // optional key prefix for List
}

type PutOptions struct {
	Visibility Visibility // default: PrivateOnly
	Confidence *float64
	Source     *string
}

type MemoryStore interface {
	Get(ctx context.Context, subjectID, namespace, key string, opt ReadOptions) (Item, bool, error)
	List(ctx context.Context, subjectID, namespace string, opt ReadOptions) ([]Item, error)
	Put(ctx context.Context, subjectID, namespace, key, value string, opt PutOptions) (Item, error)
	Delete(ctx context.Context, subjectID, namespace, key string) error

	// Manual cleanup hooks (Phase 1)
	DeleteNamespace(ctx context.Context, subjectID, namespace string) error
	WipeSubject(ctx context.Context, subjectID string) error
}
```

### 16.4 Visibility filtering (canonical-only in Phase 1)

Phase 1 reads memory from **canonical `SubjectID` only** (no union/aliases yet).

**Critical privacy rule:** visibility filtering is applied **before** any prompt injection or tool return, and in `public` context `PrivateOnly` items are excluded entirely.

## 17. SQLite + GORM implementation details (Phase 1)

### 17.1 Dependencies

- Add module dependencies:
  - `gorm.io/gorm`
  - `gorm.io/gen`
  - SQLite (pure Go, modernc-based):
    - `github.com/glebarez/sqlite`
    - `modernc.org/sqlite` (direct or transitive; required by policy)

### 17.2 Connection + pragmas

Open one `*gorm.DB` per process and share it (via `db/` package):

- `PRAGMA journal_mode=WAL;`
- `PRAGMA busy_timeout=5000;` (configurable)
- `PRAGMA foreign_keys=ON;`

Recommended pool settings (SQLite single-writer):

- `db.SetMaxOpenConns(1)` (or 2 if benchmarks show value)
- `db.SetMaxIdleConns(1)`

### 17.3 Migrations

Use GORM migrations (Phase 1 default: `AutoMigrate` on startup) via `db/migrate.go`:

- `memory_items` as defined in Section 9.1
- `identity_links` as defined in Section 9.2

Prefer explicit indexes:

- `UNIQUE(subject_id, namespace, key)`
- `INDEX(subject_id, namespace, visibility, updated_at)`
- `INDEX(identity_links.subject_id)` for reverse lookup

### 17.4 gorm/gen code generation

In-repo generation approach (recommended):

- Add `db/models/` with GORM model structs (`MemoryItem`, `IdentityLink`) and tags for:
  - primary keys / unique constraints
  - indexes (as much as GORM supports; remaining indexes can be ensured via migrator calls)
- Add `db/models/gen_annotations.go` defining gorm/gen **Annotation Syntax** interfaces:
  - `MemoryItemStore` (for `memory_items`)
  - `IdentityLinkStore` (for `identity_links`)
- Add `db/gen/` containing a small generator program and a `//go:generate` directive (see `db/gen/main.go`), outputting generated code under `db/query/` (committed).

Command:

- `go generate ./...`
- (optional, only the DB store): `go generate ./db/gen`
- If you see `permission denied` under `$HOME/.cache/go-build`, run: `env GOCACHE=/tmp/go-build go generate ./...`

Generated code policy:

- Generated files are committed so end users do not need gorm/gen installed locally.

## 18. Tool surface (optional but recommended)

### 18.1 Why a tool at all?

Prompt-injected memory covers “read for context building”. A tool allows:

- manual cleanup commands (“forget X”)
- explicit updates (“remember my timezone is …”)
- listing memory for debugging (in private only)

### 18.2 Tool API (names + parameters)

In Phase 1, memory tools are **always enabled** when memory is enabled for the request (i.e., identity resolved + `memory.enabled=true`).

Add tools (registered only when memory is enabled for the request):

- `memory_get` `{ "namespace": "...", "key": "..." }`
- `memory_put` `{ "namespace": "...", "key": "...", "value": "...", "visibility": "private_only|public_ok" }`
  - default `visibility="private_only"`
- `memory_list` `{ "namespace": "...", "prefix": "...", "limit": 50 }`
- `memory_delete` `{ "namespace": "...", "key": "..." }`
- `memory_delete_namespace` `{ "namespace": "..." }`

Note: Phase 1 does **not** expose a `memory_wipe_subject` tool.

**Enforcement:**

- In `public` context, `memory_get/list` must only ever return `public_ok`.
- In `public` context, `memory_put` still defaults to `private_only`.
- If the tool call explicitly sets `visibility="public_ok"`, allow it (no extra confirmation in Phase 1; model may choose).

## 19. Prompt injection format (Phase 1)

Inject a single block in the **system prompt** (not user history) so it is clearly “context”:

Title: `Long-term Memory (filtered)`

Content format (deterministic, compact, and bounded):

- Group by namespace
- Within namespace: sorted by key
- Include `visibility` only for debugging; omit in production prompt to reduce leakage risk
- Hard limits:
  - `max_items_in_prompt` (count)
  - `max_chars_in_prompt` (bytes/chars)

Example (conceptual):

```
profile:
  timezone = "Asia/Shanghai"
preference:
  response_style = {"lang":"zh","brevity":"concise"}
```

## 20. Configuration (new keys; Phase 1)

Because the same SQLite file may store more than memory in the future, and because we may swap to a “bigger” database later, DB configuration is defined at the **root** under `db.*` (not `memory.sqlite.*`).

Add to `config.example.yaml` when implementing:

```yaml
db:
  # Reserved for future: sqlite|postgres|mysql...
  driver: "sqlite"
  # Driver-specific DSN; for sqlite this is usually a file path.
  # If empty, Phase 1 auto-selects an on-disk sqlite file (see Notes).
  dsn: ""
  pool:
    max_open_conns: 1
    max_idle_conns: 1
    conn_max_lifetime: "0s"
  sqlite:
    busy_timeout_ms: 5000
    wal: true
    foreign_keys: true
  automigrate: true

memory:
  enabled: false
  injection:
    enabled: true
    max_items: 50
    max_chars: 6000
```

Notes:

- In Phase 1, only `db.driver="sqlite"` is implemented; other values should return a clear error (future-proof config, not future-proof runtime).
- If `db.dsn` is set, it is passed through to the driver. For sqlite (glebarez/modernc), it may be a plain file path (recommended) or a SQLite URI such as `file:./mister_morph.sqlite?cache=shared`.
- If `db.dsn` is empty, Phase 1 resolves it with this precedence:
  1) If `$HOME/.mister_morph/mister_morph.sqlite` exists, use it.
  2) Else if `./mister_morph.sqlite` exists, use it.
  3) Else create and use `$HOME/.mister_morph/mister_morph.sqlite` (ensuring `$HOME/.mister_morph/` exists).
- Even when `memory.enabled=true`, memory is still **disabled per-request** if no identity is resolved.
- For `serve` and `run` (CLI), keep identity resolution disabled so memory is a noop regardless of config.
- In Phase 1, memory tools are always enabled when memory is enabled for the request; dangerous wipe operations are not exposed (no `wipe_subject` tool), so there is no `memory.tools.*` config.

## 21. Telegram wiring plan (minimal code changes)

In `cmd/mister_morph/telegram.go`, per incoming message (after trigger decision):

1. Determine `RequestContext`:
   - `private` if `chat.type=="private"`, else `public`
2. Resolve identity:
   - `Enabled=false` if `msg.From==nil` or `msg.From.IsBot==true` (optional hardening)
   - else `ExternalKey = telegram:<from_id>` and resolve to `SubjectID` (+aliases)
3. If memory enabled:
   - Create/open `*gorm.DB` singleton (process-wide) via `db/` using `db.driver` + `db.dsn`
   - Load a bounded set of items for prompt injection (namespace allowlist + limit), filtered by request context
   - Register memory tools bound to `(store, identity, requestContext)`
   - Pass a custom prompt builder via `agent.WithPromptBuilder(...)` capturing the injected memory block
4. Run engine as today.

## 22. Testing plan (table-driven; Phase 1)

Add focused `*_test.go` coverage in `memory/`:

- Visibility filtering:
  - public ⇒ excludes `private_only` (including tool return)
  - private ⇒ includes both
- UPSERT semantics:
  - same (subject, namespace, key) overwrites and bumps `updated_at`
- Union conflict resolution:
  - acct wins over ext for same key
  - otherwise latest `updated_at` wins
- Pragmas/migration smoke test:
  - migrations create required tables and indexes

## 23. Implementation TODO (phased)

### Phase 0: groundwork

- [ ] Add `memory/` package skeleton + interfaces
- [ ] Add shared `db/` package (driver+dsn, pooling, sqlite pragmas, automigrate)
- [ ] Add GORM + sqlite dialector + modernc + gorm/gen dependencies
- [ ] Add `db/models` + `db/gen` + commit generated `db/query/`
- [ ] Implement memory store (GORM) against shared `db/` + visibility filtering
- [ ] Implement `NoopMemoryStore`

### Phase 1: Telegram integration (read-only injection)

- [ ] Resolve Telegram identity (`external_key`, `subject_id`, `enabled`)
- [ ] Compute request context (`public` vs `private`)
- [ ] Load bounded memory snapshot and inject into system prompt via `agent.WithPromptBuilder`
- [ ] Ensure public context never loads `private_only` (DB query-level filter + tests)

### Phase 2: Tools + manual cleanup

- [ ] Implement memory tools (`memory_get/put/list/delete/...`)
- [ ] Register memory tools only when identity enabled
- [ ] Add config flags for tool enablement + wipe gating

### Phase 3: Reserved (binding support scaffolding)

- [ ] Add `identity_links` read path + alias/union plumbing
- [ ] Add union read mode + conflict resolution
- [ ] Keep binding action itself out-of-scope (admin/manual insertion for now)
