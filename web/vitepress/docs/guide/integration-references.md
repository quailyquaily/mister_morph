---
title: Integration API
description: Exported functions, methods, struct fields, parameters, return values, and purpose of the integration package.
---

# Integration API

This page lists the exported API of `github.com/quailyquaily/mistermorph/integration`.

If you mainly want to see how to configure `integration.Config`, use `PreparedRun`, or connect Telegram / Slack, start with [Create Your Own AI Agent: Advanced](/guide/build-your-own-agent-advanced).

## Top-Level Functions

### `ApplyViperDefaults(v *viper.Viper)`

| Item | Value |
| --- | --- |
| Parameters | `v *viper.Viper`: target viper instance; if `nil`, it falls back to `viper.GetViper()` |
| Returns | none |
| Description | Writes the shared default config used by both `integration` and first-party runtimes into a viper instance. You only need this if your host app also organizes config through viper. |

### `DefaultFeatures() Features`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `integration.Features` |
| Description | Returns the default feature switches. `PlanTool`, `Guard`, and `Skills` are currently enabled by default. |

### `DefaultConfig() Config`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `integration.Config` |
| Description | Returns the default config. `Overrides` starts as an empty map, `Features` comes from `DefaultFeatures()`, and `Inspect` is the zero value. |

### `New(cfg Config) *Runtime`

| Item | Value |
| --- | --- |
| Parameters | `cfg integration.Config`: explicit config assembled by the host program |
| Returns | `*integration.Runtime` |
| Description | Builds a reusable runtime and snapshots defaults plus overrides during construction. |

## Config-Related Types

### `type Features struct`

| Field | Type | Description |
| --- | --- | --- |
| `PlanTool` | `bool` | Whether to register runtime helper tooling related to `plan_create`. |
| `Guard` | `bool` | Whether to enable guard inside the runtime. |
| `Skills` | `bool` | Whether to enable skill loading during prompt construction. |

### `type InspectOptions struct`

| Field | Type | Description |
| --- | --- | --- |
| `Prompt` | `bool` | Whether to dump prompts to disk. |
| `Request` | `bool` | Whether to dump requests / responses to disk. |
| `DumpDir` | `string` | Output directory for dumps. |
| `Mode` | `string` | Inspect mode name used to distinguish output. |
| `TimestampFormat` | `string` | Timestamp format used in file names. |

### `type Config struct`

| Field | Type | Description |
| --- | --- | --- |
| `Overrides` | `map[string]any` | Final override map written using viper keys; highest precedence. |
| `Features` | `integration.Features` | Controls which runtime capabilities get wired in. |
| `PromptBlocks` | `[]string` | Static blocks appended under system prompt `Additional Policies`. |
| `BuiltinToolNames` | `[]string` | Built-in tool allowlist; empty means enable all. |
| `Inspect` | `integration.InspectOptions` | Prompt / request dump options for debugging. |

### `(*Config).Set(key string, value any)`

| Item | Value |
| --- | --- |
| Parameters | `key string`: viper config key; `value any`: override value |
| Returns | none |
| Description | Writes one override entry into `Overrides`. Blank keys are ignored. |

### `(*Config).AddPromptBlock(content string)`

| Item | Value |
| --- | --- |
| Parameters | `content string`: prompt block text to append |
| Returns | none |
| Description | Appends one static prompt block. Blank strings are ignored, and blocks are applied to the runtime in order. |

## Runtime and Execution

### `type Runtime struct`

`Runtime` is the main entry point for third-party embedding. It does not expose fields directly; it mainly works through methods.

### `type PreparedRun struct`

| Field | Type | Description |
| --- | --- | --- |
| `Engine` | `*agent.Engine` | Prepared engine ready to run. |
| `Model` | `string` | Model name resolved from the current main route. |
| `Cleanup` | `func() error` | Releases temporary resources such as inspect output or MCP wiring. |

### `type RunTaskOptions struct`

| Field | Type | Description |
| --- | --- | --- |
| `Agent` | `agent.RunOptions` | Agent run options passed to `Engine.Run`. |
| `LLMProfile` | `string` | Optional LLM profile override for this task only. Empty follows the configured runtime route and does not change the runtime selection. |
| `TaskID` | `string` | Optional persisted task id. Empty uses `Agent.Meta["task_id"]`, then an auto id. |
| `TopicID` | `string` | Optional topic id. Empty uses `Agent.Meta["topic_id"]`, then stays empty. |
| `TraceID` | `string` | Optional external trace/correlation id. Empty uses `Agent.Meta["trace_id"]`, then stays empty. |
| `PersistTask` | `bool` | Writes queued/running/done/failed one-shot lifecycle events to the task journal. |

### `type RunTaskResult struct`

| Field | Type | Description |
| --- | --- | --- |
| `Final` | `*agent.Final` | Final agent output. |
| `Context` | `*agent.Context` | Agent run context. |
| `TaskID` | `string` | Task id used for this one-shot run. |
| `RunID` | `string` | Run id used for logs and LLM stats. Defaults to `TaskID`. |
| `TopicID` | `string` | Topic id used for metadata and task persistence, when supplied. |
| `TraceID` | `string` | External trace id, when supplied. |

### `type LLMProfile struct`

| Field | Type | Description |
| --- | --- | --- |
| `Name` | `string` | Profile name. |
| `InferenceProvider` | `string` | Human-facing inference provider when known. |
| `Provider` | `string` | Resolved provider name. |
| `ModelName` | `string` | Resolved model name. |
| `APIBase` | `string` | Resolved API base when present. |

### `type LLMProfileCandidate struct`

| Field | Type | Description |
| --- | --- | --- |
| `LLMProfile` | `integration.LLMProfile` | Candidate profile info. |
| `Weight` | `int` | Candidate weight from `llm.routes.main_loop.candidates`. |

### `type LLMProfileSelection struct`

| Field | Type | Description |
| --- | --- | --- |
| `Mode` | `string` | `auto` or `manual`. |
| `ManualProfile` | `string` | Manual override target when `Mode` is `manual`. |
| `RouteType` | `string` | `profile` or `candidates`. |
| `Current` | `*integration.LLMProfile` | Current resolved profile when the strategy is a single profile. |
| `Candidates` | `[]integration.LLMProfileCandidate` | Weighted candidates when the strategy is `candidates`. |
| `FallbackProfiles` | `[]integration.LLMProfile` | Fallback profiles resolved from the route. |

### `(*Runtime).NewRegistry() *tools.Registry`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `*tools.Registry` |
| Description | Builds the default registry from the current runtime snapshot. This is usually where you start before adding custom tools. |

### `(*Runtime).NewRunEngine(ctx context.Context, task string) (*PreparedRun, error)`

| Item | Value |
| --- | --- |
| Parameters | `ctx context.Context`: setup context; `task string`: current task text |
| Returns | `*integration.PreparedRun`, `error` |
| Description | Prepares a reusable engine with the default registry. |

### `(*Runtime).NewRunEngineWithRegistry(ctx context.Context, task string, baseReg *tools.Registry) (*PreparedRun, error)`

| Item | Value |
| --- | --- |
| Parameters | `ctx context.Context`: setup context; `task string`: current task text; `baseReg *tools.Registry`: base registry |
| Returns | `*integration.PreparedRun`, `error` |
| Description | Prepares an engine on top of the registry you provide. If you want built-ins plus custom tools, call `rt.NewRegistry()` first and then register your additions. |

### `(*Runtime).RunTask(ctx context.Context, task string, opts agent.RunOptions) (*agent.Final, *agent.Context, error)`

| Item | Value |
| --- | --- |
| Parameters | `ctx context.Context`: run context; `task string`: task text; `opts agent.RunOptions`: run options for this execution |
| Returns | `*agent.Final`, `*agent.Context`, `error` |
| Description | One-shot convenience entry point. It prepares an engine internally and calls `Cleanup()` automatically after execution. |

### `(*Runtime).RunTaskWithOptions(ctx context.Context, task string, opts RunTaskOptions) (RunTaskResult, error)`

| Item | Value |
| --- | --- |
| Parameters | `ctx context.Context`: run context; `task string`: task text; `opts integration.RunTaskOptions`: one-shot execution and persistence options |
| Returns | `integration.RunTaskResult`, `error` |
| Description | One-shot entry point with explicit run ids and optional task journal persistence. It injects `task_id` / `run_id`, uses `trace_id` and `topic_id` only when supplied, and does not write memory journal records. |

### `(*Runtime).GetLLMProfileSelection() (LLMProfileSelection, error)`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `integration.LLMProfileSelection`, `error` |
| Description | Returns the current `main_loop` selection view for this runtime instance. In `candidates` mode it reports the weighted strategy instead of forcing a single profile. |

### `(*Runtime).ListLLMProfiles() ([]LLMProfile, error)`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `[]integration.LLMProfile`, `error` |
| Description | Lists all configured LLM profiles with `name`, optional `inference_provider`, resolved `provider`, `model_name`, and optional `api_base`. |

### `(*Runtime).SetLLMProfile(profileName string) error`

| Item | Value |
| --- | --- |
| Parameters | `profileName string`: profile name to force for `main_loop` |
| Returns | `error` |
| Description | Switches this runtime instance to `manual` mode for `main_loop`. Other route purposes such as `plan_create` are unchanged. |

### `(*Runtime).ResetLLMProfile()`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | none |
| Description | Clears the manual `main_loop` override for this runtime instance and returns to the configured route policy. |

### `(*Runtime).RequestTimeout() time.Duration`

| Item | Value |
| --- | --- |
| Parameters | none |
| Returns | `time.Duration` |
| Description | Returns the LLM request timeout resolved from the current runtime snapshot. |

## Channel Runner

### `type BotRunner interface`

| Method | Parameters | Returns | Description |
| --- | --- | --- | --- |
| `Run` | `ctx context.Context` | `error` | Starts a long-running channel bot. |
| `Close` | none | `error` | Closes the runner proactively. |

### `type TelegramOptions struct`

| Field | Type | Description |
| --- | --- | --- |
| `BotToken` | `string` | Telegram bot token. |
| `AllowedChatIDs` | `[]int64` | Allowed chat whitelist. |
| `PollTimeout` | `time.Duration` | Telegram polling timeout. |
| `TaskTimeout` | `time.Duration` | Per-task run timeout. |
| `MaxConcurrency` | `int` | Maximum concurrent task count. |
| `GroupTriggerMode` | `string` | Group trigger mode. |
| `AddressingConfidenceThreshold` | `float64` | Addressing hit threshold. |
| `AddressingInterjectThreshold` | `float64` | Interject threshold. |
| `Hooks` | `integration.TelegramHooks` | Event callbacks. |

### `type SlackOptions struct`

| Field | Type | Description |
| --- | --- | --- |
| `BotToken` | `string` | Slack bot token. |
| `AppToken` | `string` | Slack app token. |
| `AllowedTeamIDs` | `[]string` | Allowed team whitelist. |
| `AllowedChannelIDs` | `[]string` | Allowed channel whitelist. |
| `TaskTimeout` | `time.Duration` | Per-task run timeout. |
| `MaxConcurrency` | `int` | Maximum concurrent task count. |
| `GroupTriggerMode` | `string` | Group trigger mode. |
| `AddressingConfidenceThreshold` | `float64` | Addressing hit threshold. |
| `AddressingInterjectThreshold` | `float64` | Interject threshold. |
| `Hooks` | `integration.SlackHooks` | Event callbacks. |

### `type TelegramHooks struct`

| Field | Type | Description |
| --- | --- | --- |
| `OnInbound` | `func(TelegramInboundEvent)` | Triggered when an inbound event is received. |
| `OnOutbound` | `func(TelegramOutboundEvent)` | Triggered when an outbound event is emitted. |
| `OnError` | `func(TelegramErrorEvent)` | Runtime error event callback. |

### `type SlackHooks struct`

| Field | Type | Description |
| --- | --- | --- |
| `OnInbound` | `func(SlackInboundEvent)` | Triggered when an inbound event is received. |
| `OnOutbound` | `func(SlackOutboundEvent)` | Triggered when an outbound event is emitted. |
| `OnError` | `func(SlackErrorEvent)` | Runtime error event callback. |

### `(*Runtime).NewTelegramBot(opts TelegramOptions) (BotRunner, error)`

| Item | Value |
| --- | --- |
| Parameters | `opts integration.TelegramOptions` |
| Returns | `integration.BotRunner`, `error` |
| Description | Builds a Telegram runner. Returns an error immediately if `BotToken` is empty. |

### `(*Runtime).NewSlackBot(opts SlackOptions) (BotRunner, error)`

| Item | Value |
| --- | --- |
| Parameters | `opts integration.SlackOptions` |
| Returns | `integration.BotRunner`, `error` |
| Description | Builds a Slack runner. Returns an error immediately if `BotToken` or `AppToken` is empty. |

## Event Alias Types

These exported aliases are mainly used in hook signatures:

- `TelegramInboundEvent`
- `TelegramOutboundEvent`
- `TelegramErrorEvent`
- `SlackInboundEvent`
- `SlackOutboundEvent`
- `SlackErrorEvent`

For most business logic, you only need to consume these event values inside `Hooks`; you do not need to manipulate the lower-level runtime directly.
