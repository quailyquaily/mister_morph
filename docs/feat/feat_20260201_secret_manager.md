---
status: done
---

# Secret Manager / Credential Provider — Review & TODO

Date: 2026-02-01

## 1. Requirements Review (Technical Soundness)

The overall direction is correct: fully separate secrets (API keys / tokens) from skill definitions, logs, and the LLM context, then inject them into real requests at runtime with the smallest possible exposure surface (“least exposure”).

This design significantly reduces the following risks:

- **Prompt injection / social-engineering leakage**: the model never sees the raw key, so it cannot leak it even if coerced.
- **Tool call trace leakage**: tool params / traces / debug logs must not carry secrets.
- **Untrusted skill blast radius**: a skill can declare “what it needs” but never holds the secret value.
- **Accidental copy/commit**: avoids secrets ending up in `SKILL.md`, example configs, or run logs.

There are also a few points that need to be strengthened/clarified (recommended TODOs):

- **Secrets must have authorization boundaries**: if arbitrary user text can make the model choose any profile, it can still access unrelated secrets. You need an allowlist / binding relationship (skill ↔ profile ↔ tool).
- **Do not let the LLM assemble full HTTP requests**: headers/body assembly must be controlled by the host (tool implementation or middleware).
- **Log redaction must cover common header names**: redaction based on `api_key` / `authorization` alone is easy to miss; names like `X-API-Key` must be covered.

## 2. Proposed Architecture (mistermorph)

### 2.1 Concepts and Minimal Data Model

This design recommends **profile-based auth** as the only supported path:

- Skills/LLM reference **only** a profile id.
- Skills/LLM do **not** specify secrets.
- Skills/LLM do **not** specify injection location/header names.

This is necessary to achieve “least exposure” and a small, host-controlled input surface, preventing prompt injection from rewriting injection details or target domains.

#### Skill side (declare required profiles only)

Skill frontmatter example:

```yaml
---
name: jsonbill-client
description: Call JSONBill API safely via url_fetch + auth_profile.
auth_profiles: ["jsonbill"]
---
```

At the LLM/tool call layer, only this is allowed:

- `auth_profile`: `"jsonbill"` (profile id string)

### 2.2 Secret Resolver / Credential Provider (Host side)

Secrets are resolved at config load time via `${ENV_VAR}` expansion in all string config values.

- `credential.secret` holds the secret value (use `${ENV_VAR}` to reference env vars)

- Failure policy: fail-closed (empty secret after expansion => error)

### 2.3 Injection Point (HTTP layer only)

For tools that need auth, enforce two hard rules:

1) **The LLM can only pass `auth_profile`, never secrets, and never any auth value (header/query/body/subprotocol/etc).**
2) **Final auth injection happens inside the tool (or middleware) and must not appear in observations/logs.**

In this repo, `url_fetch` (`tools/builtin/url_fetch.go`) is the right place to implement “safe HTTP” because:

- `url_fetch` currently accepts user-provided `headers`, which encourages putting secrets into tool params (high risk).
- `agent/logging.go` redaction helps, but header-name variants like `X-API-Key` are easy to miss without strict policy.

Recommended tool call shape (profile only):

```json
{
  "url": "https://api.example.com/v1/xxx",
  "method": "GET",
  "auth_profile": "jsonbill"
}
```

Additional constraints:

- Allow `headers`, but enforce a safe allowlist (P0) and deny sensitive/proxy headers (P0).
- Do not echo request headers in the observation (current `url_fetch` behavior already does this).

### 2.4 Extensibility of Profiles (Future tools)

The value of `auth_profile` is taking “what the credential is” and “how it is applied” out of LLM-controlled inputs; it is not about hard-coding “HTTP header injection” as the only mechanism.

Future tools (e.g. `websocket_fetch`) may need query params, WebSocket subprotocols, handshake headers, or request signing. Therefore the profile config should be split into:

- **Credential**: `secret` + kind (api_key/bearer/…), the resolved secret value (use `${ENV_VAR}` in config)
- **Bindings**: tool-specific injection strategy declared per tool (host-controlled)

This way:

- Skills/LLM still pass only `auth_profile="jsonbill"`.
- Whether it uses header vs query, header name, redirect policy, allowed hosts/paths, etc. are all host-controlled.

To keep this both extensible and auditable, `bindings.<tool>` should remain structured and enumerable:

- Use structured injection primitives (MVP): `inject.location` + `inject.name` + `inject.format`
- Reserve enum space for future injection locations (e.g. `subprotocol`, `cookie`, `signed`), but **each tool must only accept the subset it explicitly supports**; otherwise error (fail-closed).
- `inject.format` must be an enum (e.g. `raw` / `bearer` / `basic`); do not allow free-form templates such as `format: "X-API-Key: {{secret}}"`.

### 2.5 Authorization Boundaries (Prevent “model can grab anything”)

“Which profiles can be used” must be a host policy, not an LLM decision:

- **Skill declaration**: skill frontmatter may declare `auth_profiles: ["jsonbill"]` for prompt/context clarity only; it is not an authorization boundary.
- **Runtime allowlist**: config/flags declare which `auth_profile` ids are allowed for the run.
- **Tool-level constraints**: only selected tools may accept `auth_profile` (e.g. `url_fetch`); `bash` must not be used to carry authenticated HTTP (see TODO).

If user task text can influence `auth_profile`, validate strictly:

- `auth_profile` must match a strict regex (e.g. `^[a-z][a-z0-9_.-]{1,63}$`)
- Must be in the runtime allowlist
- Must be declared by at least one loaded skill (when enforcing skill declarations)

## 3. Implementation TODO (Prioritized)

### P0 (Must)

- Add `secrets` (or `credentials`) package: define `Resolver` interface + `EnvResolver` implementation (env only, never persisted).
- Inject the resolver during engine/tool construction (avoid tools calling `os.Getenv` directly).
- Introduce **profile-based auth (only supported path)**:
  - Add `auth_profile` param to `url_fetch`: the LLM/skill can pass only `profile_id`, not injection details or secrets.
  - Load `auth_profiles` from config (host-defined):
    - `credential`: `secret` + `kind` (api_key/bearer/…)
    - `allow`: `url_prefixes` + `methods` (plus flags like `follow_redirects`)
    - `bindings`: tool injection rules (e.g. `bindings.url_fetch.inject.location=header` + `inject.name=Authorization` + `inject.format=bearer`)
  - `url_fetch` execution: validate the URL against `allow` first, then resolve/inject the secret according to `bindings.url_fetch`; do not leak injection through redirects (recommended default: redirects disabled).
  - Explicitly do not support passing secrets directly (avoid a “bypass API” that skips profile boundaries).
- Binding validation + failure policy:
  - If `bindings.<tool>` is missing: using that `auth_profile` with that tool must error (avoid silently dropping auth).
  - If `inject.location` / fields are unsupported by the tool: error (fail-closed).
- `url_fetch.headers` strict policy (P0: header name constraints; P1: header value detection/redaction):
  - Allowlist (recommended default): `Accept`, `Content-Type`, `User-Agent`, `If-None-Match`, `If-Modified-Since`, (optional) `Range`
  - Denylist (at least): `Authorization`, `Cookie`, `Host`, `Proxy-*`, `X-Forwarded-*`
  - Extra rule: any header name matching `(?i).*api[-_]?key.*` or `(?i).*token.*` is rejected (configurable to avoid false positives later).
  - Normalize header names before matching (trim + lower + remove `-`/`_`) to avoid case/separator bypasses.
- Redirect policy (P0, hard rules):
  - Default `follow_redirects=false`
  - If enabled:
    - only allow same-origin (same scheme + host + port)
    - strict redirect count limit (e.g. ≤3)
    - validate allow policy on every hop for 301/302/303/307/308 (note 307/308 preserve method/body)
    - re-inject auth on every hop; do not rely on net/http defaults
- Logging/trace leakage prevention:
  - Extend redaction to cover `api-key` / `x-api-key` / `set-cookie` and similar forms.
  - Ensure errors/observations never contain secret values (avoid “debug print request” patterns).

### P1 (Strongly recommended)

- Extend skill frontmatter schema: allow `auth_profiles: ["..."]` (profile id only; no injection details).
- Add policy config (recommend in `assets/config/config.example.yaml`):
  - `secrets.allow_profiles: ["jsonbill", "..."]` (recommended main allowlist)
- Default policy for `bash` (recommend hard-coded safe default):
  - When allowlisted auth profiles are configured, allow `bash` for local automation, but deny `curl` by default to avoid “bash + curl” carrying authenticated HTTP; use `url_fetch + auth_profile` for HTTP.
  - If curl features are needed, prefer a structured subprocess tool (e.g. `exec`/`curl_fetch`) that takes `profile_id + argv + stdin` and injects secrets host-side with a minimal environment.
- `auth_profiles` config (recommend in `assets/config/config.example.yaml`):
  - `auth_profiles.<id>.credential.secret`
  - `auth_profiles.<id>.credential.kind` (api_key/bearer/...)
  - `auth_profiles.<id>.allow.url_prefixes` / `methods`
  - `auth_profiles.<id>.allow.follow_redirects` (default false)
  - `auth_profiles.<id>.bindings.<tool>` (url_fetch/websocket_fetch/...)
  - `auth_profiles.<id>.bindings.<tool>.allow_user_headers` (default false)
- Add uniform validation before tool execution:
  - validate `auth_profile` is allowed (`secrets.allow_profiles` + skill declarations)
  - validate URL matches profile restrictions (origin/path + redirect policy)
- Add output redaction beyond “key name” matching:
  - JWTs, common token prefixes, `-----BEGIN ... PRIVATE KEY-----`, etc.
  - Apply minimal redaction to HTTP response bodies before they enter context (avoid an API returning a token that then gets stored).
  - Optionally detect suspicious header values (token/JWT/private key blocks) and reject or redact (risk-dependent).

### P2 (Future)

- Strengthen “short lifetime” handling:
  - Resolver may return `{Value, ExpiresAt, RedactHint}` for caching/expiry
  - Ensure secrets never enter memory/history/context (enforced in code, not via prompt rules)
- Extend `auth_profile` support to more tools (instead of encouraging `bash + curl`).
- Integrate with Guard module:
  - pre-tool: block sensitive headers, unknown auth_profile, high-risk outbound domains
  - post-tool: redact responses before they enter context

### Implementation Checklist (Detailed)

> Goal: implement profile-based auth so that plaintext secrets never appear in: `SKILL.md` / tool params / tool logs / tool observations / LLM context / long-lived process memory.

**Config & Wiring (cmd/)**

- [x] Add default fail-closed values in `cmd/mistermorph/defaults.go`:
  - [x] `secrets.allow_profiles=[]` (empty means “all profiles disabled”)
  - [x] `auth_profiles={}`
- [x] Add full examples and comments for `secrets:` and `auth_profiles:` in `assets/config/config.example.yaml` (and clarify: bash disabled by default; when allowlisted auth profiles are configured, curl is denied by default).
- [x] In `cmd/mistermorph/registry.go`:
  - [x] Build `EnvResolver`
  - [x] Load/validate `auth_profiles` and build a read-only `ProfileStore` (discard invalid profiles)
  - [x] When allowlisted auth profiles are configured, keep `bash` optional but deny `curl` by default
  - [x] Inject `ProfileStore + Resolver` into the `url_fetch` tool constructor

**Profile Schema (Go struct + viper unmarshal)**

- [x] Define `AuthProfile` (`secrets/`):
  - [x] `ID` (map key)
  - [x] `Credential`: `kind` + `secret`
  - [x] `Allow` (fail-closed):
    - [x] `url_prefixes` (each entry is a full URL prefix; scheme/host/port/path are bound in one rule)
    - [x] `methods` (normalize to upper)
    - [x] `follow_redirects`
    - [x] `allow_proxy` (default false)
    - [x] `deny_private_ips` (default true; MVP rejects literal IP/localhost only, no DNS resolution)
  - [x] `Bindings`: `map[string]ToolBinding`
- [x] Define `ToolBinding`:
  - [x] `inject.location` enum (MVP: `header`)
  - [x] `inject.name` (header name)
  - [x] `inject.format` enum (`raw|bearer|basic`)
  - [x] `allow_user_headers` (default false)
  - [x] `user_header_allowlist` (active when `allow_user_headers=true`)
- [x] Fail-closed validation at load time:
  - [x] `url_prefixes` / `methods` must be non-empty
  - [x] `bindings.url_fetch` must exist
  - [x] `inject.name` must match header token syntax

**Tool: url_fetch (`tools/builtin/url_fetch.go`)**

- [x] Update `ParameterSchema()`:
  - [x] Add `auth_profile` (optional string)
  - [x] Allow `headers` but enforce allowlist: `Accept`/`Content-Type`/`User-Agent`/`If-None-Match`/`If-Modified-Since`/`Range`
  - [x] Explicitly reject sensitive headers: `Authorization`/`Cookie`/`Host`/`Proxy-*`/`X-Forwarded-*` and any `*api[-_]?key*` / `*token*`
- [x] Update `Execute()`:
  - [x] If params include `auth_profile`:
    - [x] require `auth_profile` is in `secrets.allow_profiles` and exists in `auth_profiles`
    - [x] validate URL matches profile allow policy (scheme/host/port/path prefix)
    - [x] resolve and inject secret per `bindings.url_fetch.inject` (MVP: header injection only)
  - [x] Redirect policy:
    - [x] default: do not follow redirects (`follow_redirects=false`)
    - [x] if allowed: limit to ≤3 and same-origin only (scheme+host+port)
    - [x] validate allow policy on every hop; re-inject auth on every hop
  - [x] Strictly reject sensitive headers (regardless of whether auth_profile is used)
- [x] Adjust observation output to avoid leaks:
  - [x] sanitize `url:` by redacting sensitive query keys (future-proof)
  - [x] redact response bodies before they enter context (JWT / Bearer / private key blocks / common key=value patterns)

**Logging Redaction (`agent/logging.go`)**

- [x] Improve `shouldRedactKey`:
  - [x] normalize keys (lower + remove `-`/`_`) before matching
  - [x] cover `x-api-key` / `api-key` / `set-cookie` etc.
- [ ] Ensure no code path prints “final request headers” (even in debug mode)

**Skills (`skills/`)**

- [x] Add lightweight parsing of SKILL frontmatter in `skills/skills.go` (extract `auth_profiles: []` only)
- [x] When skills are loaded, record which profiles were declared for the run (auditing + optional enforcement)
- [ ] Clarify rule: a skill declaration is informational only; the actual permission boundary remains `secrets.allow_profiles` (avoid prompt injection expanding privileges via skill loading)

**Tests**

- [x] `tools/builtin/url_fetch*_test.go`:
  - [x] auth_profile header injection works
  - [x] non-allowlisted profiles are rejected
  - [x] redirects: same-origin only and re-inject on each hop (cover 307)
  - [x] explicit `X-API-Key` in `headers` is rejected
  - [x] response redaction works (JWT does not appear in output)
- [x] `agent/logging*_test.go`:
  - [x] header names like `X-API-Key` / `api-key` / `set-cookie` are redacted

## 4. Acceptance Criteria (MVP)

- Plaintext secrets never appear in SKILL, logs, tool params, or LLM context.
- `url_fetch` can access authenticated HTTP APIs without exposing the key (inject via `auth_profile`).
- Any `auth_profile` not in the allowlist must fail (fail-closed), and errors must not contain the secret value.
- Enabling `logging.include_tool_params=true` does not leak secrets: sensitive headers are rejected or redacted.
- When allowlisted auth profiles are configured, authenticated HTTP must not be carried via `bash + curl` (default allow bash but deny curl, or require explicit approval policy).

## 5. Samples (Configuration and Usage)

### 5.1 config.yaml snippet

Note: `config.yaml` here refers to the main `mistermorph` config (passed via `--config`, or your local convention). It should not live inside a skill directory.

- Skill directories should contain only shareable content like `SKILL.md` / `scripts/` / `references/` and must not contain environment-specific config or plaintext secrets.
- If you want a skill to carry “how to configure the main config” examples, include them as text in `SKILL.md` or `references/` (e.g. `references/config_snippet.yaml`) for manual merging.

```yaml
secrets:
  allow_profiles: ["jsonbill"]

auth_profiles:
  jsonbill:
    credential:
      kind: api_key
      secret: "${JSONBILL_API_KEY}"
    allow:
      url_prefixes: ["https://api.jsonbill.com/tasks/docs"]
      methods: ["POST"]
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

tools:
  url_fetch:
    enabled: true
```

### 5.2 tool_call example

```json
{
  "url": "https://api.jsonbill.com/tasks/docs",
  "method": "POST",
  "auth_profile": "jsonbill"
}
```

### 5.3 SKILL.md frontmatter example

```yaml
---
name: jsonbill
description: Call JSONBill API safely.
auth_profiles: ["jsonbill"]
---
```

## 6. Potential Gaps (Recommended additions)

- **Redirect safety**: Go redirects may carry custom headers to the redirected target; keep redirects disabled by default or use `CheckRedirect` to allow only same-origin and to re-inject auth safely.
- **`bash` environment inheritance**: even if the LLM never sees the key, a `bash` tool that inherits env can read it via `env`/`printenv`/`/proc/self/environ` and exfiltrate; recommend a minimal env allowlist and Guard hard blocks for risky patterns.
- **LLM provider key (`llm.api_key`)**: this is also a secret (even if not placed into prompts) and can leak through logs/traces; ensure provider auth is never logged, and consider unifying it under the `${ENV_VAR}` config expansion.
- **Secret/profile scope model**: beyond allowlists, define scope boundaries (per-run / per-skill / per-tool / per-domain) to prevent reuse across unrelated actions.
- **Network boundaries**: proxy usage (`HTTP_PROXY`), custom `Host`, and TLS verification policies all affect “least exposure”; document defaults and disallowed options.
- **Test coverage**: ensure tests cover sensitive header rejection, auth_profile injection without observation/log leakage, redirect behavior, and response redaction rules.
