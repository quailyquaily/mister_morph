# LLM Profiles and Routes

## Goal

Add first-class support for routing different internal LLM flows to different LLM configs.

## Status

- implemented
- `go test ./...` passes

Later follow-up: the implicit default profile also supports ordered transient-error fallback via `llm.fallback_profiles`.

This is for two concrete needs:

1. Different flows have different quality/cost/latency needs.
2. `mistermorph` already has multiple internal LLM call sites, not just one main loop.

Examples already present in the codebase:

- channel main loop
- group addressing LLM
- heartbeat loop
- `plan_create`

## First Principles

There are three different concepts:

1. `profile`
   - one complete LLM config
   - provider, endpoint, api key, model, timeout, temperature, reasoning params, provider-specific fields

2. `route`
   - which profile a flow should use

3. `fallback`
   - what to try if a profile fails transiently

V1 only implements:

- `profiles`
- `routes`

V1 explicitly does **not** implement:

- automatic fallback chains
- arbitrary scene-name routing
- a generic LLM orchestration framework

## Non-Goals

- do not expose raw internal scene strings like `telegram.loop` or `slack.addressing_decision` as config contract
- do not support every sub-call in the system on day one
- do not redesign provider config loading
- do not break existing single-LLM config

## V1 Route Keys

Only these semantic route purposes are supported in V1:

- `main_loop`
- `addressing`
- `heartbeat`
- `plan_create`

Routing lookup supports:

- global purpose
  - example: `main_loop`

Resolution order:

1. `<purpose>`
2. implicit default profile

## Config Shape

Keep the existing top-level `llm.*` config as the implicit default profile.

Add:

```yaml
llm:
  inference_provider: openai
  api_key: "${OPENAI_API_KEY}"
  model: gpt-5.2

  profiles:
    cheap:
      inference_provider: openai
      api_key: "${OPENAI_API_KEY}"
      model: gpt-4.1-mini
    reasoning:
      inference_provider: xai
      api_key: "${XAI_API_KEY}"
      model: grok-4.1-fast-reasoning

  routes:
    main_loop: default
    addressing: cheap
    heartbeat: cheap
    plan_create: reasoning
```

Notes:

- `default` is reserved and means the top-level `llm.*` config
- named profiles are independent and do not inherit top-level LLM fields
- each named profile must provide its own provider identity, plus a model unless the provider supplies one, and any credentials, endpoint, or runtime fields required by that provider
- provider defaults such as the built-in Codex endpoint remain available and are not profile inheritance
- secret-like LLM fields remain backward compatible with plaintext values and also support `*_ref` in `llm` / `llm.profiles`

## Route Application Points

V1 wiring:

1. generic CLI / daemon run path
   - route: `main_loop`

2. Telegram runtime main loop
   - route: `main_loop`

3. Slack runtime main loop
   - route: `main_loop`

4. LINE runtime main loop
   - route: `main_loop`

5. Lark runtime main loop
   - route: `main_loop`

6. Telegram group addressing
   - route: `addressing`

7. Slack group addressing
   - route: `addressing`

8. LINE group addressing
   - route: `addressing`

9. Lark group addressing
   - route: `addressing`

10. heartbeat
   - route: `heartbeat`

11. `plan_create`
   - route: `plan_create`

## Design Constraints

- route keys are a fixed semantic set, not free-form strings
- route keys are global by purpose; V1 does not support channel-specific overrides
- route resolution must happen before client creation
- runtime code should ask for a route, not manually stitch provider/model/api key
- stats/inspect scene labels remain for observability only
- route resolution must freeze with runtime snapshot / resolver state, not read live mutable viper on each call

## Backward Compatibility

Existing config without `llm.profiles` / `llm.routes` must keep working.

In that case:

- everything resolves to the implicit default profile built from top-level `llm.*`

## Validation Rules

- route target must be either:
  - `default`
  - an existing named profile
- invalid profile target should fail fast at startup / resolver creation time
- invalid profile field values should fail with existing `llm.*`-style validation errors

## Implementation Plan

1. Add `llm.profiles` and `llm.routes` config parsing.
2. Add a small route resolver that returns a fully resolved LLM config for:
   - `purpose`
3. Route main loop clients for:
   - CLI
   - daemon
   - telegram/slack/line/lark runtimes
4. Route sub-clients for:
   - addressing
   - heartbeat
   - `plan_create`
5. Add tests for:
   - named profile isolation
   - route lookup order
   - invalid route targets
   - unchanged default behavior

## Compression Review

This design is intentionally narrow.

It avoids overdesign by:

- not adding fallback yet
- not adding arbitrary route names
- not adding per-tool generic routing
- not adding a large LLM router service abstraction

The minimum new concepts are:

- `profile`
- `route`

That is the smallest model that matches the real structure already present in the codebase.
