---
title: Security
---

# Security

This document focuses on:

- **process isolation** and **filesystem sandboxing** when running `mister_morph` as a long-lived daemon (for example, via `mister_morph serve`)
- **secret handling** for authenticated outbound HTTP calls (profile-based credential injection)

## Table of contents

- [Threat model](#threat-model)
- [Secret handling (profile-based auth)](#secret-handling-profile-based-auth)
  - [Configure profiles](#configure-profiles)
  - [Tool behavior and safeguards](#tool-behavior-and-safeguards)
- [Systemd sandbox](#systemd-sandbox)
  - [Recommended deployment layout](#recommended-deployment-layout)
  - [Filesystem sandboxing](#filesystem-sandboxing)
  - [Other hardening knobs](#other-hardening-knobs)
- [Notes and limitations](#notes-and-limitations)

## Threat model

`mister_morph` is an agent that can:

- make outbound HTTP(S) calls (LLM provider, web_search, url_fetch, Telegram)
- read local files (read_file tool, skill discovery)
- optionally execute shell commands (bash tool, if enabled)

The main security risks are:

- **data exfiltration** (sending local data to external services)
- **secret leakage** (prompts, tool params, logs, traces)
- **over-broad capabilities** (shell access, unrestricted networking, unrestricted filesystem reads/writes)

## Systemd sandbox

Because of those capabilities, daemon mode is a good candidate for a **deny-by-default** runtime profile:

- keep the root filesystem read-only
- allow writes only to explicitly declared directories
- reduce kernel / device / namespace surface
- run without elevated privileges

systemd provides a first-class “service hardening” feature set for this. Unlike `chroot`, it does not require building a separate root filesystem; it applies restrictions directly to the unit.

### Recommended deployment layout

The recommended unit assumes:

- Binary: `/opt/morph/mister_morph`
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

To avoid this, `mister_morph` supports **profile-based credential injection**:

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

For `mister_morph`, this split is recommended because sqlite DB files are not “cache” and should not be treated as disposable.

The example unit additionally pins the agent’s paths via env vars:

- `MISTER_MORPH_DB_DSN=/var/lib/morph/mister_morph.sqlite`
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

## Guard DNS resolution (SSRF protection)

When Guard is enabled with `deny_private_ips: true` and `resolve_dns: true` (default), Guard resolves hostnames via DNS before allowing outbound requests. Any hostname that resolves to a private, loopback, link-local, or unspecified IP is blocked. This prevents SSRF attacks where a hostname like `evil.example.com` resolves to `127.0.0.1` or cloud metadata endpoints (`169.254.169.254`).

DNS-based SSRF checking is centralized in the Guard module. The `url_fetch` tool delegates all SSRF policy to Guard via context, ensuring a single policy applies to both initial requests and redirects.

## read_file: allowed_dirs and symlink protection

The `read_file` tool supports an optional `allowed_dirs` allowlist. When configured, only files within the listed directories can be read (fail-closed):

```yaml
tools:
  read_file:
    allowed_dirs:
      - "/opt/morph/workspace"
      - "/var/cache/morph"
```

To prevent symlink-based escapes (a symlink inside an allowed directory pointing outside it), `read_file` uses `os.Lstat` to detect and reject symlinks before reading.

## Notes and limitations

- systemd hardening is not a perfect sandbox. If you need stronger isolation, consider running in a container/VM.
- If you enable the `bash` tool, treat it as high risk. Prefer keeping it disabled in daemon mode, or requiring confirmations and using a strict allowlist of read-only bind mounts.
- Even with profile-based auth, avoid enabling arbitrary outbound execution paths (e.g. shelling out to network tools). Prefer structured tools with explicit allowlists and fail-closed policy.
