# Configuration

This page holds the config details moved out of the top-level README.

The canonical config template is [../assets/config/config.example.yaml](../assets/config/config.example.yaml).

## Sources and Precedence

`mistermorph` uses Viper. You can configure it with:

- CLI flags
- environment variables
- a config file

Precedence:

`CLI flag > MISTER_MORPH_* env > config.yaml > default`

Supported config file formats:

- `.yaml`
- `.yml`
- `.json`
- `.toml`
- `.ini`

Env var rules:

- prefix: `MISTER_MORPH_`
- nested keys: replace `.` and `-` with `_`
- example: `tools.bash.enabled` -> `MISTER_MORPH_TOOLS_BASH_ENABLED=true`

All string values in config support `${ENV_VAR}` expansion.

## Runtime Model

There are two different config lifecycles:

- one-shot commands such as `run`, `telegram`, and `slack`
- the long-running `console serve` process

One-shot commands are simple:

```text
process start
    |
    v
load config once
    |
    v
run with that config until process exit
```

`console serve` is different because the process stays alive. It uses runtime snapshots.

Resolved console config path:

- `--config`, if explicitly set
- otherwise the first existing file in `config.yaml`, `~/.morph/config.yaml`
- if neither exists, the default write target is local `config.yaml`

Snapshot build flow:

```text
               startup / config file change
                           |
                           v
        +-------------------------------------------+
        | loadConsoleRuntimeConfig(configPath, ...)  |
        | ----------------------------------------- |
        | 1. shared defaults                         |
        | 2. MISTER_MORPH_* env                      |
        | 3. captured runtime flag overrides         |
        |    current code: inherited --log-* flags   |
        | 4. read + ${ENV_VAR} expand config.yaml    |
        +-------------------------------------------+
                           |
                           v
                 +---------------------+
                 | immutable snapshot  |
                 | reader: *viper.Viper|
                 +---------------------+
                    |               |
                    v               v
          +----------------+   +----------------------+
          | Console Local  |   | Managed Runtimes     |
          | in-process rt  |   | telegram / slack     |
          +----------------+   +----------------------+
```

What this means in practice:

- The runtime does not use the global process `viper` as live mutable state.
- A running `console serve` instance works from its current snapshot.
- When `config.yaml` changes, a new snapshot is built and swapped in.
- If rebuilding fails, the old snapshot keeps running.
- In-flight tasks keep their bound generation. New tasks use the next generation only after the swap.

## Console Update Path

The console Web API and setup repair path do not mutate runtime state directly. They only write YAML to the resolved config path.

```text
browser / repair UI
        |
        v
PUT /api/settings/*    or    PUT /api/setup/file?key=config
        |
        v
write config.yaml only
        |
        v
no direct global viper mutation
no direct runtime restart call
        |
        v
console config poller notices file fingerprint change
        |
        v
build new snapshot
    |                    |
    | success            | failure
    v                    v
prepare local generation
prepare managed runtimes
        |
        v
apply both sides
        |
        v
new tasks use new generation       keep old snapshot
old in-flight tasks finish on old generation
```

This separation is intentional:

- the write path is responsible only for durable config
- the runtime layer is responsible only for consuming snapshots
- concurrency stays inside each runtime instance, not inside the config writer
- config writes are atomic replace, so the poller sees either the old file or the new file

## Console Startup With Invalid Config

`console serve` tries to build a runtime snapshot from the resolved config path at startup.

If the config file is invalid:

- the HTTP server still starts
- the runtime falls back to a defaults-only snapshot
- the setup repair UI can fix `config.yaml`
- later successful file changes replace the fallback snapshot

This avoids a deadlock where a broken config prevents the repair UI from starting.

## Common CLI Flags

Global flags:

- `--config`
- `--log-level`
- `--log-format`
- `--log-add-source`
- `--log-include-thoughts`
- `--log-include-tool-params`
- `--log-include-skill-contents`
- `--log-max-thought-chars`
- `--log-max-json-bytes`
- `--log-max-string-value-chars`
- `--log-max-skill-content-chars`
- `--log-redact-key` (repeatable)

`run`:

- `--task`
- `--provider`
- `--endpoint`
- `--model`
- `--api-key`
- `--llm-request-timeout`
- `--interactive`
- `--skills-dir` (repeatable)
- `--skill` (repeatable)
- `--skills-enabled`
- `--max-steps`
- `--parse-retries`
- `--max-token-budget`
- `--timeout`
- `--inspect-prompt`
- `--inspect-request`

`console serve`:

- `--console-listen`
- `--console-base-path`
- `--console-static-dir`
- `--console-session-ttl`
- `--allow-empty-password`

`telegram`:

- `--telegram-bot-token`
- `--telegram-allowed-chat-id` (repeatable)
- `--telegram-group-trigger-mode`
- `--telegram-addressing-confidence-threshold`
- `--telegram-addressing-interject-threshold`
- `--telegram-poll-timeout`
- `--telegram-task-timeout`
- `--telegram-max-concurrency`

`slack`:

- `--slack-bot-token`
- `--slack-app-token`
- `--slack-allowed-team-id` (repeatable)
- `--slack-allowed-channel-id` (repeatable)
- `--slack-group-trigger-mode`
- `--slack-addressing-confidence-threshold`
- `--slack-addressing-interject-threshold`
- `--slack-task-timeout`
- `--slack-max-concurrency`

`skills`:

- `skills list --skills-dir` (repeatable)
- `skills install --dest --dry-run --clean --skip-existing --timeout --max-bytes --yes`

`install`:

- `install [dir]`
- `--yes`

`chat`:

- `--profile`
- `--provider`
- `--endpoint`
- `--model`
- `--api-key`
- `--llm-request-timeout`
- `--compact-mode`
- `--verbose`
- `--skills-dir` (repeatable)
- `--skill` (repeatable)
- `--skills-enabled`
- `--max-steps`
- `--parse-retries`
- `--max-token-budget`
- `--tool-repeat-limit`
- `--timeout`

## Common Environment Variables

- `MISTER_MORPH_CONFIG`
- `MISTER_MORPH_LLM_PROVIDER`
- `MISTER_MORPH_LLM_ENDPOINT`
- `MISTER_MORPH_LLM_MODEL`
- `MISTER_MORPH_LLM_API_KEY`
- `MISTER_MORPH_LLM_CACHE_KEY_PREFIX`
- `MISTER_MORPH_LLM_REQUEST_TIMEOUT`
- `MISTER_MORPH_LOGGING_LEVEL`
- `MISTER_MORPH_LOGGING_FORMAT`
- `MISTER_MORPH_SERVER_AUTH_TOKEN`
- `MISTER_MORPH_CONSOLE_PASSWORD`
- `MISTER_MORPH_CONSOLE_PASSWORD_HASH`
- `MISTER_MORPH_TELEGRAM_BOT_TOKEN`
- `MISTER_MORPH_SLACK_BOT_TOKEN`
- `MISTER_MORPH_SLACK_APP_TOKEN`
- `MISTER_MORPH_FILE_CACHE_DIR`
- `MISTER_MORPH_CHAT_COMPACT_MODE`

Provider-specific values use the same mapping. Examples:

- `llm.azure.deployment` -> `MISTER_MORPH_LLM_AZURE_DEPLOYMENT`
- `llm.bedrock.model_arn` -> `MISTER_MORPH_LLM_BEDROCK_MODEL_ARN`
- `llm.bedrock.aws_profile` -> `MISTER_MORPH_LLM_BEDROCK_AWS_PROFILE`
- `llm.bedrock.aws_session_token` -> `MISTER_MORPH_LLM_BEDROCK_AWS_SESSION_TOKEN`

## Key Config Areas

Core LLM:

- `llm.provider` selects the backend.
- Most providers use `llm.endpoint`, `llm.api_key`, and `llm.model`.
- Azure uses `llm.azure.deployment`.
- Bedrock uses `llm.bedrock.*`.
- `llm.cache_ttl` controls cache intent across providers. Supported values are `off`, `short`, `long`, and Go duration strings such as `5m`, `1h`, and `24h`. The runtime maps this to each provider's supported cache buckets.
- `llm.cache_key_prefix` is optional and defaults to empty. For providers that support `prompt_cache_key`, the runtime prepends it to the generated key so changing the value forces a new cache group.
- `llm.tools_emulation_mode` controls tool-call emulation for models without native tool calling.
- `llm.profiles` defines named profile overrides.
- `llm.routes` routes semantic purposes such as `main_loop`, `addressing`, `heartbeat`, `plan_create`, and `memory_draft`.
- Each route can be a simple profile name or an object with `profile`, `candidates`, and `fallback_profiles`.
- `candidates` enables per-run weighted traffic split; one candidate is selected once for the current run and reused for all LLM calls in that run.
- `fallback_profiles` is route-local and only applies after the chosen primary route candidate fails with a fallback-eligible error.

Logging and runtime limits:

- `logging.level`
- `logging.format`
- `logging.include_thoughts`
- `logging.include_tool_params`
- `max_steps`
- `parse_retries`
- `max_token_budget`
- `timeout`

Skills:

- `skills.enabled`
- `skills.load`
- `file_state_dir`
- `skills.dir_name`

Tools:

- all tool toggles live under `tools.*`
- examples: `tools.bash.enabled`, `tools.url_fetch.enabled`

Console:

- `console.listen`
- `console.base_path`
- `console.static_dir`
- `console.session_ttl`

Chat:

- `chat.compact_mode` — compact display mode: omit user/assistant name prefixes in prompts and output.

Auth profiles and secrets:

- `secrets.allow_profiles` is the runtime allowlist.
- `auth_profiles.<id>.credential.secret` holds the secret value.
- Use `${ENV_VAR}` for secret references.

If you configure at least one allowlisted auth profile, `bash` still works but `curl` is denied by default. Use `url_fetch` for authenticated HTTP.

## Example

```yaml
llm:
  provider: openai
  model: gpt-5.4
  api_key: "${OPENAI_API_KEY}"
  profiles:
    cheap:
      model: gpt-4.1-mini
    reasoning:
      provider: xai
      model: grok-4.1-fast-reasoning
      api_key: "${XAI_API_KEY}"
  routes:
    main_loop:
      candidates:
        - profile: default
          weight: 1
        - profile: cheap
          weight: 1
      fallback_profiles: [reasoning]
    plan_create: reasoning
```
