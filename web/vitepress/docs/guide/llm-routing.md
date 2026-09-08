---
title: LLM Routing Policies
description: Choose profiles, traffic splitting, and error fallback for different llm purposes.
---

# LLM Routing Policies

Mister Morph provides flexible routing policies to solve the following problems:

1. Different purposes may need different LLM configs.
2. LLM requests may need traffic splitting.
3. When one LLM config fails, it should be possible to fall back to a backup LLM config.

## LLM Profiles

Each profile is an LLM config. Top-level `llm.*` is itself the default profile, and `llm.profiles.<name>` is used to declare named profiles.

Notes:

- Named profiles inherit from top-level `llm.*` and only override the fields they change.
- `default` is a reserved name that means "continue using top-level `llm.*`".
- Use `inference_provider` for the user-facing provider choice. `provider` is the derived protocol provider and is mainly for older configs or advanced overrides.

In the example below, the top-level model is OpenAI GPT-5.4. Two additional profiles are defined: GPT-4o mini and Claude Opus 4.6.

From the names, you can already see the intent: GPT-4o mini is for cheaper work, while Claude Opus 4.6 is for deeper reasoning.

```yaml
llm:
  inference_provider: "openai"
  model: "gpt-5.4"
  api_key: "${OPENAI_API_KEY}"

  profiles:
    cheap:
      model: "gpt-4o-mini"
    reasoning:
      inference_provider: "anthropic"
      model: "claude-opus-4-6"
      api_key: "${CLAUDE_API_KEY}"
```

In other words, profiles define which reusable LLM configs exist, so that the routing, traffic splitting, and fallback features can use them later.

## Routing

`llm.routes.*` defines how different llm purposes should use different model configs.

Besides `main_loop`, which is responsible for running the agent itself, the other purposes are separate llm calls. You can think of them as simple sub-agents.

### Currently supported purposes

- `main_loop`: main agent loop.
- `addressing`: only used for addressing detection in group chats or channels.
- `awareness`: only used for scheduled awareness tasks.
- `heartbeat`: legacy alias for `awareness`.
- `think`: only used by the `/think <task>` command prefix. It also temporarily applies `reasoning_effort=xhigh`.
- `plan_create`: only used for planning requests inside the `plan_create` tool.

In the example below, plan creation and `/think` use the `reasoning` profile, which means `claude-opus-4-6`; group-chat addressing uses the cheaper `gpt-4o-mini` through `cheap`:

```yaml
llm:
  routes:
    plan_create: reasoning
    think: reasoning
    addressing: cheap
```

### Traffic splitting for a route

Mister Morph supports traffic splitting for LLM requests. Use the `candidates` field to define the split table.

The example below shows how to split traffic between `default_apple` and `default_banana` (you need to define them first under `llm.profiles`):

```yaml
llm:
  routes:
    main_loop:
      candidates:
        - profile: "default"
          weight: 1
        - profile: "default_apple"
          weight: 1
        - profile: "default_banana"
          weight: 1
```

Rules:

- `candidates.weight` controls the selection weight.
- Within one run loop, only one profile is used. Profiles are not interleaved. Selection is based on `run_id`.
- If the current llm hits a fallback-eligible error, the runtime first tries the remaining candidates under the same route.

### Route fallback

Besides traffic splitting, Mister Morph supports error fallback for LLM requests. For example:

```yaml
llm:
  routes:
    plan_create:
      profile: "reasoning"
      fallback_profiles: [ "default" ]
```

If the current llm hits a fallback-eligible error, and no other candidate is available, the runtime tries the configs in `fallback_profiles` one by one.

For request timeouts, the runtime first retries the same profile up to five times
after the initial attempt. This includes network timeouts and HTTP 408/504 errors.
Retries wait 0.5–1, 1–2, 2–4, 4–8, and 8–16 seconds, with random jitter to avoid
synchronized retry bursts. Only after these attempts fail does the runtime move
to another candidate or fallback profile. Each backup profile has the same retry
limit. Other errors retain the fallback behavior described above.

Timeout retries also apply when no fallback is configured. Each attempt gets a
fresh `llm.request_timeout`, but all attempts and waits share the task's deadline.
Cancellation or task expiry stops retries and fallback immediately. For example,
six requests at the default 90-second timeout can use nine minutes plus backoff;
a shorter task deadline stops them earlier. Retries repeat only the LLM request,
not tools already executed in earlier steps. Logs record each scheduled retry as
`llm_request_timeout_retry`, including its profile, retry number, and delay.

## How to write this in integration

The configuration style is similar. You just replace YAML with `cfg.Set(...)`:

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.routes.plan_create", "reasoning")
cfg.Set("llm.routes.addressing", map[string]any{
  "profile": "cheap",
  "fallback_profiles": []string{"default"},
})
```
