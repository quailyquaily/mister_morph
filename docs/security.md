---
title: Security
---

# Security

This document focuses on:

- **process isolation** and **filesystem sandboxing** when running `mistermorph` as a long-lived daemon (for example, via `mistermorph serve`)
- **secret handling** for authenticated outbound HTTP calls (profile-based credential injection)
- **Guard (M1)**: outbound allowlists, redaction, async approvals, and audit logs

## Table of contents

- [Threat model](#threat-model)
- [Guard (M1)](#guard-m1)
  - [Outbound allowlists](#outbound-allowlists)
  - [Redaction](#redaction)
  - [Async approvals and audit](#async-approvals-and-audit)
- [Secret handling (profile-based auth)](#secret-handling-profile-based-auth)
  - [Configure profiles](#configure-profiles)
  - [Tool behavior and safeguards](#tool-behavior-and-safeguards)
- [Systemd sandbox](#systemd-sandbox)
  - [Recommended deployment layout](#recommended-deployment-layout)
  - [Filesystem sandboxing](#filesystem-sandboxing)
  - [Other hardening knobs](#other-hardening-knobs)
- [Notes and limitations](#notes-and-limitations)

## Threat model

`mistermorph` is an agent that can:

- make outbound HTTP(S) calls (LLM provider, web_search, url_fetch, Telegram)
- read local files (read_file tool, skill discovery)
- optionally execute shell commands (bash tool, if enabled)

The main security risks are:

- **data exfiltration** (sending local data to external services)
- **secret leakage** (prompts, tool params, logs, traces)
- **over-broad capabilities** (shell access, unrestricted networking, unrestricted filesystem reads/writes)

## Guard (M1)

Guard is a lightweight, content/workflow safety layer designed to complement (not replace) OS/container sandboxing.

M1 focuses on three high-value capabilities:

- outbound destination allowlists (primarily `url_fetch`)
- redaction for tool outputs and final outputs
- async approvals and an audit trail (JSONL)

### Outbound allowlists

Guard can enforce a **global allowlist** for unauthenticated `url_fetch` calls (i.e. calls without `auth_profile`).

Config (example):

```yaml
guard:
  enabled: true
  network:
    url_fetch:
      allowed_url_prefixes:
        - "https://api.example.com/v1/"
      deny_private_ips: true
      allow_proxy: false
      follow_redirects: false
```

Behavior:

- If `guard.network.url_fetch.allowed_url_prefixes` is **empty**, unauthenticated `url_fetch` is **blocked** (fail-closed).
- If `url_fetch` uses `auth_profile`, the destination boundary is enforced by `auth_profiles.<id>.allow.url_prefixes` (Guard still audits, but does not add an extra global allowlist layer by default).

Hardening toggles:

- `deny_private_ips`: blocks literal `localhost` / `127.0.0.1` / RFC1918 private IP targets to reduce SSRF risk. M1 does not resolve hostnames to IPs; enforce egress at the network/OS layer if you need stronger guarantees.
- `allow_proxy`: when false, the HTTP client ignores `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` to avoid unexpected routing/MITM.

### Redaction

Guard redacts secret-like content **before it enters context/logs**:

- tool observations (`ToolCallPost`)
- final output (`OutputPublish`)

Built-in redactors cover common patterns (private key blocks, JWT-like strings, bearer tokens, and sensitive `key=value` forms). You can add extra regex patterns under `guard.redaction.patterns`.

### Async approvals and audit

Guard approvals are asynchronous by design:

- When an action requires approval (M1 default: `bash` tool when enabled), the run pauses and returns a `final.output` object like:
  - `{ "status": "pending", "approval_request_id": "apr_...", "message": "..." }`
- Approval state is stored in SQLite (configure via `guard.approvals.sqlite_dsn`, default: `$HOME/.morph/guard_approvals.sqlite`).
- Approval expiry is **hard-coded to 5 minutes** in M1.

Daemon (`mistermorph serve`) exposes minimal admin endpoints (authenticated with `server.auth_token`):

- `GET /approvals/{id}` (status + metadata; never returns `resume_state`)
- `POST /approvals/{id}/approve`
- `POST /approvals/{id}/deny`
- `POST /approvals/{id}/resume` (re-queues the paused task)

Audit:

- Guard emits structured audit events to an append-only JSONL log.
- Configure via `guard.audit.jsonl_path` (default: `$HOME/.morph/guard_audit.jsonl`) and `guard.audit.rotate_max_bytes`.

## Systemd sandbox

Because of those capabilities, daemon mode is a good candidate for a **deny-by-default** runtime profile:

- keep the root filesystem read-only
- allow writes only to explicitly declared directories
- reduce kernel / device / namespace surface
- run without elevated privileges

systemd provides a first-class “service hardening” feature set for this. Unlike `chroot`, it does not require building a separate root filesystem; it applies restrictions directly to the unit.

### Recommended deployment layout

The recommended unit assumes:

- Binary: `/opt/morph/mistermorph`
- Config: `/opt/morph/config.yaml`
- Skills: `/opt/morph/skills` (configure `skills.dirs` to point here)
- Persistent state (sqlite DB): `/var/lib/morph/`
- Ephemeral cache (file_cache_dir, Telegram downloads, write_file tool output): `/var/cache/morph/`
- Non-secret env/config: `/opt/morph/morph.env` (mode `0640`, owned by root or `morph`)
- Secrets env: `/opt/morph/morph.secrets.env` (mode `0600`, owned by root or `morph`)

See the example unit file: `deploy/systemd/mister-morph.service`.

## Secret handling (profile-based auth)

Agents are extremely good at “accidentally” leaking secrets if you ever put them into:

- prompts / skill docs
- tool parameters (especially headers)
- logs / traces

To avoid this, `mistermorph` supports **profile-based credential injection**:

- Skills/LLM only reference a profile id (e.g. `auth_profile: "jsonbill"`).
- The host resolves the real secret value from the environment (via `secret_ref`).
- The tool injects the credential into the actual HTTP request (e.g. `Authorization: Bearer …`) without logging it.

### Configure profiles

In `/opt/morph/config.yaml`:

```yaml
secrets:
  enabled: true
  allow_profiles: ["jsonbill"]
  # Optional hardening: require the profile id to be declared by at least one loaded skill frontmatter.
  require_skill_profiles: true

auth_profiles:
  jsonbill:
    credential:
      kind: api_key
      secret_ref: JSONBILL_API_KEY
    allow:
      url_prefixes: ["https://api.jsonbill.com/tasks"]
      methods: ["POST", "GET"]
      follow_redirects: false
      allow_proxy: false
      deny_private_ips: true
    bindings:
      url_fetch:
        inject:
          location: header
          name: Authorization
          format: bearer
        allow_user_headers: true
        user_header_allowlist: ["Accept", "Content-Type", "User-Agent"]
```

In `/opt/morph/morph.secrets.env`:

```bash
JSONBILL_API_KEY="..."
MISTER_MORPH_LLM_API_KEY="..."
MISTER_MORPH_SERVER_AUTH_TOKEN="..."
```

### Tool behavior and safeguards

- `url_fetch` supports `auth_profile` and injects credentials server-side.
- `url_fetch` rejects sensitive headers in user-provided `headers` to reduce accidental leaks.
- `url_fetch` supports saving binary responses to `file_cache_dir` (instead of inlining bytes in the LLM context), which is recommended for PDFs.
- When `secrets.enabled=true`, `bash` can still be enabled for local automation, but `curl` is rejected by default to avoid “bash + curl” carrying authenticated HTTP requests.

### Filesystem sandboxing

#### Read-only system files

- `ProtectSystem=strict` makes the system directories effectively read-only for the service (with a small set of unavoidable exceptions handled by systemd).
- This prevents accidental or malicious writes to places like `/etc`, `/usr`, `/bin`, etc.

#### No access to home directories

- `ProtectHome=true` hides `/home`, `/root`, and `/run/user` from the service.
- This is a strong default: it prevents the agent from reading shell history, SSH keys, dotfiles, and other sensitive user data.

If you need access to specific content, prefer moving it under `/opt/morph/...` or using explicit bind mounts (see below) rather than opening up all of `/home`.

#### Explicit writable directories only

systemd can create and manage service-owned directories under `/var/lib`, `/var/cache`, and `/var/log`:

- `StateDirectory=morph` → writable `/var/lib/morph` (persistent state)
- `CacheDirectory=morph` → writable `/var/cache/morph` (ephemeral cache)

For `mistermorph`, this split is recommended because approval state uses a sqlite file that should not be treated as disposable.

The example unit additionally pins the agent’s paths via env vars:

- `MISTER_MORPH_GUARD_APPROVALS_SQLITE_DSN=/var/lib/morph/guard_approvals.sqlite`
- `MISTER_MORPH_FILE_CACHE_DIR=/var/cache/morph`

#### Optional bind mounts (fine-grained allowlist)

If you want the agent to read a specific project directory, allowlist it explicitly:

- Read-only: `BindReadOnlyPaths=/some/dir:/workspace/dir`
- Read-write: `BindPaths=/some/dir:/workspace/dir`

Prefer bind mounts over weakening `ProtectSystem`/`ProtectHome`.

### Other hardening knobs

The example unit also enables common isolation options:

- `NoNewPrivileges=true`: prevents gaining extra privileges (e.g. via setuid binaries).
- `PrivateTmp=true`: private `/tmp` for the service.
- `PrivateDevices=true` + `DevicePolicy=closed`: blocks access to device nodes.
- `ProtectProc=invisible` + `ProcSubset=pid`: reduce process info leakage.
- `RestrictNamespaces=true`: reduces attack surface from namespace creation.
- `MemoryDenyWriteExecute=true`: blocks W+X memory mappings (mitigates some exploit classes).
- `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`: allow only typical networking families (needed for outbound HTTP(S) and local sockets).

## Notes and limitations

- systemd hardening is not a perfect sandbox. If you need stronger isolation, consider running in a container/VM.
- If you enable the `bash` tool, treat it as high risk. Prefer keeping it disabled in daemon mode, or requiring confirmations and using a strict allowlist of read-only bind mounts.
- Even with profile-based auth, avoid enabling arbitrary outbound execution paths (e.g. shelling out to network tools). Prefer structured tools with explicit allowlists and fail-closed policy.
- Guard M1 is intentionally small: Telegram approval UX and durable task storage across daemon restarts are not implemented yet.
