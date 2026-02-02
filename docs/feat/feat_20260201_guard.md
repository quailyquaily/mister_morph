---
status: draft
---

# Guard Module — Minimal Requirements (M1)

Date: 2026-02-01

## 1. Background

Agents that ingest untrusted content while holding tool/network/file privileges are vulnerable to prompt injection and accidental data exfiltration.

This Guard module is a **content-level and workflow-level safety layer**:
- It does **not** replace OS/container sandboxing.
- It focuses on high-value controls that the OS cannot reliably express (content redaction, outbound destination policy, human approval workflow, audit trail).

## 2. Positioning: Prefer OS/Container for Capability Boundaries

Many “policies” are better implemented by the runtime environment:

- **Privilege escalation**: prefer running as an unprivileged user with no sudo (e.g. systemd `User=` + `NoNewPrivileges=yes`).
- **Filesystem scope**: prefer systemd/container allowlists (e.g. bind-mount a workspace read-only; restrict writable paths to state/cache dirs).

The Guard module should **assume the process is already sandboxed**.

## 3. M1 Scope: Keep Only 3 High-Value Capabilities

M1 should ship with only:

1) **Outbound network allowlists (destination control)**  
   Prevent exfiltration by restricting where outbound requests can go.

2) **Sensitive data redaction (tool outputs + final output)**  
   Prevent secrets/tokens/private keys from being stored, displayed, or forwarded.

3) **Asynchronous approvals + audit trail**  
   When policy requires approval, pause the run and return `pending` to the caller; store a durable approval request and resume only after approval.

Everything else (fine-grained command classification, complex per-method HTTP policies, perfect prompt-injection detection) is explicitly out of M1.

## 4. Core Concepts

### 4.1 Actions

Guard evaluates discrete “actions”:

- `ToolCallPre`: before executing a tool call
- `ToolCallPost`: after a tool returns (inspection + redaction)
- `OutputPublish`: before publishing a final answer / message
- (Optional but recommended) `SkillInstall`: before writing downloaded skill files into a skills directory

### 4.2 Decisions

Every action yields:

- `risk_level`: `low | medium | high | critical`
- `decision`: `allow | allow_with_redaction | require_approval | deny`
- `reasons[]`: explainable matched rules

Default posture: **fail closed for outbound and secret-like content** (deny or require approval).

## 5. Capability #1 — Outbound Network Allowlists

### 5.1 Goal

Block any outbound network request whose destination is not explicitly allowed.

This applies to:
- `url_fetch` (primary)
- `web_search` (usually allow only its configured endpoint)
- any tool that can send data to the network

If `bash` is enabled, this is especially important: `bash` can bypass `url_fetch` policies via `curl/wget/nc/...`. For M1, the safe default is:
- either disable `bash`, or
- require approval for any `bash`, or
- deny known network binaries/tokens (e.g. `curl`, `wget`) in `bash` (best-effort; OS sandboxing is still preferred).

### 5.2 Matching Model (simple by design)

Use one of:
- `allowed_domains: ["api.example.com"]` (hostname only), or
- `allowed_url_prefixes: ["https://api.example.com/v1/"]` (recommended; simplest to reason about)

**M1 does not need method-based risk rules**. Method can be logged/audited, but is not required for policy.

Recommended hardening defaults:
- deny localhost / private IPs (with DNS resolution: hostnames resolving to private IPs are also blocked)
- disable proxies by default
- allow redirects only if same-origin and still allowlisted (or disable redirects)

Notes on the two “hardening” toggles:
- `deny_private_ips`: reduces SSRF risk by blocking literal `localhost` / `127.0.0.1` / RFC1918 private IP targets.
- `resolve_dns` (default `true`): when enabled alongside `deny_private_ips`, Guard resolves hostnames via `net.LookupHost` and checks all returned IPs against loopback/private/link-local/unspecified ranges. This prevents DNS rebinding attacks where a hostname resolves to a private IP. If DNS resolution fails, the request is allowed through (it will fail at the HTTP layer).
- `allow_proxy`: when false, the HTTP client should ignore `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` env vars so requests cannot be transparently routed through a proxy/MITM.

### 5.3 Example config sketch

```yaml
guard:
  enabled: true
  network:
    # Prefer url_prefixes for clarity.
    url_fetch:
      allowed_url_prefixes:
        - "https://api.jsonbill.com/tasks/"
        - "https://duckduckgo.com/html/"
      deny_private_ips: true
      follow_redirects: false
      allow_proxy: false
```

### 5.4 Example decisions

- `url_fetch("https://api.jsonbill.com/tasks/123")` → allow
- `url_fetch("https://paste.example/upload")` → deny (`non_allowlisted_domain`)
- `url_fetch("http://127.0.0.1:8080/")` → deny (`private_ip`)

## 6. Capability #2 — Sensitive Data Redaction

### 6.1 Goal

Redact secrets and secret-like material before it can:
- enter the run context/history,
- be shown to the user/admin,
- be sent out via tools,
- be emitted in logs/audit records.

### 6.2 What to redact (M1)

Rule-based detection with configurable regexes:
- API keys/tokens (provider-specific patterns)
- JWT-like strings
- private key blocks (`-----BEGIN ... PRIVATE KEY-----`)
- common `key=value` pairs with sensitive keys (`token=`, `api_key=`, `password=`)

Redaction outputs should preserve only minimal structure (e.g. `sk-[redacted]`).

### 6.3 Where to apply redaction (M1)

- `ToolCallPost`: redact tool observations before appending to messages/context.
- `OutputPublish`: redact final output before returning/sending.
- Approval summaries: always redacted; never include full secret-bearing content.

### 6.4 Example

Tool output contains a JWT; the user sees:

```text
token=[redacted_jwt]
```

and the unredacted token never enters logs/context.

## 7. Capability #3 — Async Approvals + Audit Trail

### 7.1 Goal

Support approvals without blocking the entire system:
- The current run can be paused.
- The caller receives a `pending` result containing `approval_request_id`.
- The external controller (daemon/Telegram/embedded host) completes the approval flow and resumes the run.

### 7.2 Approval binding (prevent “approve A, execute B”)

Approvals must bind to a specific action snapshot:
- Compute `action_hash = SHA256(canonical_json(action))`.
- Store `approval_request_id -> {action_hash, created_at, expires_at, status, actor, reasons}`.
- On resume, recompute `action_hash` and require it matches the stored value; otherwise deny.

M1 note: approval expiry is currently **hard-coded** (5 minutes) to keep config surface small.

### 7.3 API shape (conceptual)

Guard integration should support:

```go
// Evaluate a candidate action.
Decision := Guard.Evaluate(action, meta)

// If Decision requires approval:
reqID := Approvals.Create(actionHash, Decision, meta)
return Pending{ApprovalRequestID: reqID}

// Later: after an admin approves/denies:
Approvals.Resolve(reqID, Approved|Denied, actor)
Run.Resume(reqID) // resumes from the paused point
```

Implementation detail is up to the runtime:
- CLI can “wait synchronously” by polling `Approvals.Resolve(...)` (TTY prompt), but it still uses the same async primitives.
- Daemon/Telegram should return immediately with `pending`, then resume after approval, avoiding global blocking.

### 7.4 What triggers approval (M1)

Keep this small and high-signal:

- Any action that sends data to a destination not in allowlist → **deny** (prefer deny over approval for exfil).
- Any action that might reveal secrets to the user/admin (e.g. reading a private key file) → **require_approval** (or deny if OS sandboxing is expected to block it anyway).
- Any `bash` action when `bash` is enabled and network binaries are not fully blocked → **require_approval** by default.

### 7.5 Audit trail (M1)

Every Guard decision produces an audit record:

- `event_id`, `run_id`, timestamp
- `action_type`, `tool_name` (if any)
- redacted action summary (never store raw secrets)
- decision/risk level/reasons
- approval id + result (if applicable)

## 8. Examples (End-to-end)

### Example A — `url_fetch` blocked by allowlist

Action:

```json
{"type":"ToolCallPre","tool":"url_fetch","params":{"url":"https://paste.example/upload","method":"POST"}}
```

Decision:
- `risk_level: high`
- `decision: deny`
- reason: `non_allowlisted_domain`

### Example B — Final output redaction

Action:

```json
{"type":"OutputPublish","content":"Here is the token: sk-abcdef..."}
```

Decision:
- `risk_level: high`
- `decision: allow_with_redaction`
- output becomes: `Here is the token: sk-[redacted]`

### Example C — Async approval flow (embedded host)

1) Run reaches a guarded action requiring approval → returns:

```json
{
  "status": "pending",
  "approval_request_id": "apr_123",
  "message": "Approval required: reading a sensitive file."
}
```

2) Host notifies admin (Telegram/UI). Admin approves `apr_123`.
3) Host resumes the run; the engine continues from the paused step.

## 9. Integration Notes for this Repo (non-normative)

This repo already has pieces that can be reused:
- `tools/builtin/confirm.go`: TTY confirmation primitive (useful for CLI “sync wait”).
- `cmd/mister_morph/skills_install_builtin.go`: remote skill review + confirm flow; can be refactored to emit Guard audit events and to use the same approval binding.
- `tools/builtin/url_fetch.go`: already enforces destination allow policies at the tool layer (auth profiles). Guard should complement this with “global outbound allowlists” and a consistent approval/audit story.

## 10. M1 Acceptance Criteria

- Outbound network requests are denied unless destination is allowlisted (prefix/domain).
- Tool outputs and final outputs are redacted for secret-like material before being stored/logged/published.
- Approval requests are durable, bound to a specific action hash, and can be resolved asynchronously.
- Runs can pause/resume without blocking the whole daemon; `pending` is a first-class outcome for the controller.

## 11. Audit Storage (M1)

M1 should treat audit as a first-class output of Guard, but keep the storage mechanism flexible:

- Guard produces **structured audit events** (in-memory structs).
- An `AuditSink` persists them (or forwards them).
- The default sink can be “structured logs”, but production deployments typically want a queryable store.

### 11.1 What to store (minimal, safe)

Audit records must be **redaction-safe** by construction:

- Store only a **redacted action summary** (never raw secrets; never full file contents; never full HTTP bodies).
- Store action binding fields (`action_hash`) so approvals cannot be replayed for a different action.

Suggested fields:

- `event_id`, `run_id`, timestamp
- `action_type` (`ToolCallPre|ToolCallPost|OutputPublish|SkillInstall`)
- `tool_name` (if any)
- `action_summary_redacted`
- `risk_level`, `decision`, `reasons[]`
- `approval_request_id` (optional), `approval_status` (optional), `actor` (optional)
- `action_hash` (SHA256 of canonical action)

### 11.2 Where to store it

M1 should use a single, clear storage approach:

- **Audit events**: append-only **JSONL** under the service state directory (one JSON object per line).
  - Rationale: high-throughput, low overhead, resilient to schema evolution.
  - Retention (M1): rotate by size (e.g. 50–200MB per file) and/or by time (daily), and keep `N` days or a max total size cap.
- **Approval state**: a small **SQLite** database under the same state directory (for durability across restarts).
  - Store only: pending approvals, action_hash binding, decision/actor/expiry, and resume metadata.
  - Do **not** store the full audit event stream in SQLite in M1.

Future extensibility:
- The JSONL stream can be shipped to external sinks (SIEM/IDS/DLP/log pipeline) via an agent (tail/forward), or replaced with a dedicated sink implementation later without changing Guard’s core logic.

### 11.3 Retention and privacy

M1 should include basic retention controls:
- max age (e.g. 7–30 days) and/or max rows/bytes
- rotation/retention for JSONL
- periodic compaction for SQLite (approvals table) if needed

Even with redaction, treat audit logs as sensitive:
- do not store unredacted tool params or outputs
- do not store “full previews” for approvals; keep previews minimal

## 12. Implementation TODO (M1)

- [x] Add `guard/` package: actions, decisions, redactor, network policy context helpers
- [x] Add JSONL audit sink with size-based rotation
- [x] Add SQLite approvals store (`guard_approvals`) for async approvals + resume metadata
- [x] Wire Guard into the engine:
  - [x] Tool pre-hook (`ToolCallPre`): enforce url_fetch destination allowlist, require approval for bash
  - [x] Tool post-hook (`ToolCallPost`): redact observations before adding to context
  - [x] Output hook (`OutputPublish`): redact final output before returning
- [x] Enforce guard network policy in `url_fetch` redirects/proxy handling (when policy is present in context)
- [x] Add viper config defaults + `config.example.yaml` examples for `guard.*`
- [x] Add daemon HTTP admin endpoints to approve/deny/resume (`/approvals/{id}/*`)
- [ ] Add Telegram approval commands to approve/deny/resume runs (M1 controller integration)
- [ ] Persist daemon task metadata across restarts (current in-memory queue means resuming is only possible while the daemon is still running)
- [ ] Add retention/rotation plumbing for JSONL (delete old files / max total size) and document recommended ops setup
- [ ] Add more test coverage:
  - [ ] approval flow (create → approve → `Engine.Resume`)
  - [ ] url_fetch redirect bypass cases
  - [ ] audit JSONL write/rotate behavior
