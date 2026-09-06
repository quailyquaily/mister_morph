# Configuration

This page holds the config details moved out of the top-level README.

The canonical config template is [../assets/config/config.example.yaml](../assets/config/config.example.yaml).

## Sources and Precedence

`mistermorph` uses Viper. You can configure it with:

- CLI flags
- environment variables
- a config file

Precedence:

`CLI flag > MISTER_MORPH_* env > ~/.morph/config.yaml > default`

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

YAML scalar values in config support `${ENV_VAR}` expansion. Secret values can also use `${aws-sm:<secret-id>}`, `${aws-sm:<secret-id>#<field>}`, or a system-keyring reference such as `${secret:<opaque-id>}`. A system-keyring reference must occupy the entire scalar. Comments and mapping keys are not expanded.

Interactive setup and Console settings save new local credentials in macOS Keychain, Windows Credential Manager, or Linux Secret Service. The YAML file normally receives only the opaque system-keyring reference. At runtime startup, MisterMorph checks whether the system store is reachable and logs a warning when it is not. If a Settings save cannot write a submitted secret to the system store, it logs a warning and saves that submitted value to the 0600 YAML file using the previous behavior. Headless services should still prefer environment variables or AWS Secrets Manager. An existing `${secret:...}` reference that is missing or unavailable stops config loading; reference resolution never falls back to another source.

YAML expansion is done on parsed config values before they are passed to Viper. This may normalize the in-memory YAML formatting, but it does not write back to the config file.

## Web Settings

Console Settings can read and edit every supported public field in the example config. Settings are grouped by Agent, Tools, MCP, ACP, Skills, Channels, Automation, Security, and System rather than by raw YAML nesting.

The browser submits only changed fields and the revision of the file it loaded. A stale revision returns `409 Conflict`; it does not overwrite a newer external edit. Saving preserves unrelated YAML fields and comments.

Secret values are never returned to the browser. The UI reports only whether a secret is configured and lets the user replace or clear it. New secrets use the system secret store when available, with the documented 0600-file fallback.

Each field shows its source and apply mode. Environment- and command-line-managed values are read-only. A save response distinguishes immediate changes, changes used by new tasks, runtime restarts, and process restarts. Console does not restart a runtime or process automatically.

When one Console manages another Console through an endpoint, Settings requests are sent to the selected endpoint. Paths and system-secret references therefore belong to the target machine.

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
- otherwise `~/.morph/config.yaml`
- if it does not exist yet, `~/.morph/config.yaml` is still the default write target

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
        | 4. read + ref expansion for config values   |
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

## Untriggered Group Messages

Each group-chat channel has its own switch for recording valid messages that do not pass group trigger admission:

```yaml
telegram:
  record_untriggered: false
slack:
  record_untriggered: false
line:
  record_untriggered: false
lark:
  record_untriggered: false
```

The switches are independent and default to `false`. When enabled, the channel writes a compact `conversation/untriggered_inbound` event to the shared journal. It does not create a task or call another LLM. Messages rejected before trigger admission, including commands, private messages, unauthorized messages, bot messages, and duplicates caught by existing ingress filtering, are not recorded. The feature adds no dedupe state. Stored text is limited to 2048 bytes; attachments are represented only by `has_attachment: true`.

There is no global fallback or CLI flag for this setting.

## Task Journal and Projections

Accepted task and topic changes from every runtime are written to the unified journal under `<file_state_dir>/journal/`. `tasks.persistence_targets` only selects the runtimes whose task projections are saved and restored across process restarts. Removing a runtime from this list does not disable its journal records.

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
- `--workspace`
- `--no-workspace`
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
- `--verbose`
- `--skills-dir` (repeatable)
- `--skill` (repeatable)
- `--skills-enabled`
- `--max-steps`
- `--parse-retries`
- `--max-token-budget`
- `--tool-repeat-limit`
- `--timeout`
- `--workspace`
- `--no-workspace`

For `run` and `chat`, workspace selection uses `--no-workspace`, then `--workspace`, then `workspace_dir`, then the current directory.

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
- `MISTER_MORPH_WORKSPACE_DIR`
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
- `xai_oauth` uses the local xAI OAuth token store. It ignores `llm.endpoint`, `llm.api_key`, and credential headers.
- Azure uses `llm.azure.deployment`.
- Bedrock uses `llm.bedrock.*`.
- `llm.cache_ttl` controls cache intent across providers. Supported values are `off`, `short`, `long`, and Go duration strings such as `5m`, `1h`, and `24h`. The runtime maps this to each provider's supported cache buckets.
- `llm.cache_key_prefix` is optional and defaults to empty. For providers that support `prompt_cache_key`, the runtime prepends it to the generated key so changing the value forces a new cache group.
- For GPT-5.6-family OpenAI and Responses-compatible requests, the runtime generates `prompt_cache_key` and marks the fixed system prompt as an explicit cache breakpoint when caching is enabled. It leaves `prompt_cache_options` unset so the provider's default implicit breakpoint remains active.
- `llm.tools_emulation_mode` controls tool-call emulation for models without native tool calling.
- `llm.profiles` defines independent named LLM configurations; blank fields do not fall back to top-level `llm` values.
- `llm.routes` routes semantic purposes such as `main_loop`, `addressing`, `awareness`, `think`, and `plan_create`. `heartbeat` is still accepted as a legacy alias for `awareness`.
- Each route can be a simple profile name or an object with `profile`, `candidates`, and `fallback_profiles`.
- `candidates` enables per-run weighted traffic split; one candidate is selected once for the current run and reused for all LLM calls in that run.
- `fallback_profiles` is route-local and only applies after the chosen primary route candidate fails with a fallback-eligible error.
- `/think <task>` uses `llm.routes.think` when set. If it is unset, it uses the default profile. After the LLM is selected, the runtime temporarily applies `reasoning_effort: "xhigh"` for that task.

Logging and runtime limits:

- `logging.level`
- `logging.format`
- `logging.include_thoughts`
- `logging.include_tool_params`
- `max_steps`
- `parse_retries`
- `max_token_budget`
- `timeout`

Local paths:

- `workspace_dir` is the default project directory when a Console topic or channel conversation has no workspace attachment. Empty means no global default.
- `file_state_dir` stores durable MisterMorph state.
- `file_cache_dir` stores disposable downloads and temporary files.
- A topic or conversation attachment overrides `workspace_dir`; removing the attachment reveals the configured default again.

Skills:

- `skills.enabled`
- `skills.load`
- `file_state_dir`
- `skills.dir_name`

Tools:

- all tool toggles live under `tools.*`
- examples: `tools.bash.enabled`, `tools.url_fetch.enabled`, `tools.coder.enabled`

Console:

- `console.listen`
- `console.base_path`
- `console.static_dir`
- `console.session_ttl`

Auth profiles and secrets:

- `secrets.allow_profiles` is the runtime allowlist.
- `secrets.aws_secrets_manager.region` sets the AWS Secrets Manager region for `${aws-sm:...}` refs; empty uses the AWS SDK default region lookup.
- `secrets.aws_secrets_manager.profile` sets the AWS shared config profile for `${aws-sm:...}` refs; empty uses the AWS SDK default profile lookup.
- `auth_profiles.<id>.credential.secret` holds the secret value.
- Use `${ENV_VAR}`, `${aws-sm:<secret-id>}`, or `${secret:<opaque-id>}` in secret scalar values.

If you configure at least one allowlisted auth profile, `bash` still works but `curl` is denied by default. Use `url_fetch` for authenticated HTTP.

## xAI Grok OAuth

`xai_oauth` is separate from the API-key-based `xai` provider. It uses an eligible Grok subscription and never falls back to `XAI_API_KEY`.

Login and inspect the local status:

```bash
morph auth xai login
morph auth xai status
```

To select it as the default provider after login:

```bash
morph auth xai login --set-default
```

Equivalent configuration:

```yaml
llm:
  inference_provider: xai_oauth
  provider: xai_oauth
  model: grok-4.5
```

Named profiles and routes may select `xai_oauth` in the same way. Do not set an endpoint, API key, or `Authorization` header for it; those values are ignored. OAuth tokens remain in `<file_state_dir>/auth/xai.json` and are not returned to Console clients.

Named profiles are independent LLM configurations. A blank profile field does not use the corresponding top-level `llm` value. Configure each profile's inference provider, model, credentials, endpoint, and optional runtime fields explicitly. `llm.pricing_file`, `llm.image`, and the process state directory remain shared runtime settings.

Chat requests support text and image input. The OAuth login does not provide credentials for `image_generate` or `image_edit`; configure `llm.image` separately for those tools.

MisterMorph uses the xAI shared public OAuth client also used by OpenClaw. It requests `openid`, `profile`, `offline_access`, `grok-cli:access`, and `api:access`; it does not request email data. The inference endpoint remains fixed at `https://api.x.ai/v1`, and user-configured credential headers are ignored.

Login success confirms authentication, not model entitlement. xAI may still reject inference because of subscription level, region, team policy, or quota.

Logout removes the local token even if remote revocation fails:

```bash
morph auth xai logout
```

## Example

```yaml
llm:
  provider: openai
  model: gpt-5.4
  api_key: "${OPENAI_API_KEY}"
  profiles:
    cheap:
      inference_provider: openai
      model: gpt-4.1-mini
      api_key: "${OPENAI_API_KEY}"
      supports_image_parts: false # optional; overrides model-name capability detection for this profile
    reasoning:
      inference_provider: xai
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
    think: reasoning
```
