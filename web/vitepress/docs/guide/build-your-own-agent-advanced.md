---
title: "Create Your Own AI Agent: Advanced"
description: Focuses on integration.Config, custom tools, run modes, and channel integration.
---

# Create Your Own AI Agent: Advanced

## Config Layer

`integration.Config` is the only explicit config entry point in the `integration` package.

Your host program can read environment variables, config files, or a database, write the final values into `Config`, then pass it to `integration.New(cfg)`.

### Example

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.inference_provider", "openai")
cfg.Set("llm.model", "gpt-5.4")
cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))
```

### Override Defaults

Mister Morph itself supports CLI flags, environment variables, and `config.yaml`.
As an embedding user, you can use any configuration source you prefer, then call `Set(key, value)` to override any default value. Any field from `config.yaml` can be set this way. See [Config Fields](/guide/config-reference).

### Feature Toggles

`Features.*` controls optional runtime features. The current toggles are:

- `PlanTool`: whether to register the runtime helper tool `plan_create`.
- `Guard`: whether to inject guard into the runtime.
- `Skills`: whether to enable skill loading during prompt construction.

### Built-in Tools

`BuiltinToolNames` controls which built-in tools are enabled. Leave it empty to include all built-ins.

### Custom Prompt

If you want prompt customization at the `integration` layer, use `cfg.AddPromptBlock(...)`.

These blocks are appended to the end of the system prompt automatically.

### Inspectors

Mister Morph provides inspector options so you can inspect lower-level LLM behavior:

```go
cfg.Inspect.Prompt = true
cfg.Inspect.Request = true
cfg.Inspect.DumpDir = "./dump"
```

This writes detailed prompt and request dumps into the dump directory.

### LLM Routing Policies

Similarly, you can override `llm.routes.*` to customize which LLM route is used by each purpose.

You can define multiple profiles under `llm.profiles`, then use a route like this to require one feature to use a specific profile:

```go
// Use the profile named reasoning when creating plans.
cfg.Set("llm.routes.plan_create", "reasoning")
```

Full route rules and examples are documented separately in [LLM Routing Policies](/guide/llm-routing).

### Switch Main LLM Profile at Runtime

If your host app needs a model picker, `integration.Runtime` can inspect and override the current `main_loop` profile at runtime.

```go
profiles, err := rt.ListLLMProfiles()
if err != nil {
  panic(err)
}

selection, err := rt.GetLLMProfileSelection()
if err != nil {
  panic(err)
}

fmt.Println("mode:", selection.Mode)

if len(profiles) > 0 {
  if err := rt.SetLLMProfile(profiles[0].Name); err != nil {
    panic(err)
  }
}

rt.ResetLLMProfile()
```

Important semantics:

- This state is scoped to one `integration.Runtime` instance.
- `SetLLMProfile(...)` only overrides `main_loop`.
- `ResetLLMProfile()` returns to the configured `llm.routes.main_loop` policy.
- If `main_loop` uses weighted `candidates`, `GetLLMProfileSelection()` reports the strategy as candidates instead of pretending there is one fixed profile.

### Config Snapshot

Once configuration is complete, call `integration.New(cfg)` to snapshot the config and create the agent runtime.

## Custom Tools

If you want to keep the built-in tools wired by `integration` and also add your own tools, use the runtime method `rt.NewRegistry()` to create a tool registry.

### Custom Tool Example

The following example shows how to define an echo tool and register it with the agent:

```go
package main

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "strings"

  "github.com/quailyquaily/mistermorph/agent"
  "github.com/quailyquaily/mistermorph/integration"
)

type EchoTool struct{}

func (t *EchoTool) Name() string { return "echo_text" }

func (t *EchoTool) Description() string {
  return "Echoes input text as JSON."
}

func (t *EchoTool) ParameterSchema() string {
  return `{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Text to echo."}
  },
  "required": ["text"]
}`
}

func (t *EchoTool) Execute(_ context.Context, params map[string]any) (string, error) {
  text, _ := params["text"].(string)
  text = strings.TrimSpace(text)
  if text == "" {
    return "", fmt.Errorf("text is required")
  }
  b, _ := json.Marshal(map[string]any{"text": text})
  return string(b), nil
}

func main() {
  cfg := integration.DefaultConfig()
  cfg.Set("llm.inference_provider", "openai")
  cfg.Set("llm.model", "gpt-5.4")
  cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))

  rt := integration.New(cfg)
  reg := rt.NewRegistry()
  reg.Register(&EchoTool{})

  task := "Call tool echo_text with text 'hello from tool', then answer with that text."

  prepared, err := rt.NewRunEngineWithRegistry(context.Background(), task, reg)
  if err != nil {
    panic(err)
  }
  defer prepared.Cleanup()

  final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{Model: prepared.Model})
  if err != nil {
    panic(err)
  }

  fmt.Println("Agent:", final.Output)
}
```

## Runtime Execution Modes

### Prepared Engine API

Use this when you want lifecycle control, session reuse, explicit cleanup, or your own scheduling/orchestration layer.

- Controlled lifecycle: you decide when to call `Cleanup()`.
- Reusability: reuse the same `prepared.Engine` across multiple runs.
- Per-run flexibility: each `Run` call can receive different `RunOptions` such as `History`, `Meta`, or `OnStream`.
- Better orchestration: you can access both `prepared.Model` and `Engine` directly.

```go
prepared, err := rt.NewRunEngine(context.Background(), task)
if err != nil {
  panic(err)
}
defer prepared.Cleanup()

final, _, err := prepared.Engine.Run(context.Background(), task, agent.RunOptions{
  Model: prepared.Model,
})
```

### Convenience API

This is a good fit for one-shot tasks. If you need a custom registry, engine reuse, or explicit lifecycle control, prefer `PreparedRun`.

```go
final, runCtx, err := rt.RunTask(context.Background(), task, agent.RunOptions{})
_ = final
_ = runCtx
_ = err
```

If the host needs a task id, run id, and optional task journal records for later observation, use `RunTaskWithOptions`:

```go
result, err := rt.RunTaskWithOptions(context.Background(), task, integration.RunTaskOptions{
  Agent: agent.RunOptions{},
  PersistTask: true,
  TopicID: "support-thread-123", // optional
  TraceID: "http-request-123",   // optional external correlation id
})
if err != nil {
  panic(err)
}

fmt.Println(result.TaskID)
fmt.Println(result.Final.Output)
```

`RunTaskWithOptions` writes task lifecycle records only when `PersistTask` is true. It does not write memory journal records.

## Channel Integration

Besides the Web UI, Mister Morph supports channels such as Telegram and Slack as conversation surfaces.

The integration path is straightforward:

### Telegram

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN")})
_ = tg
```

If you want to handle Telegram inbound, outbound, and error events in your host program, pass `TelegramHooks` through `TelegramOptions.Hooks`:

```go
tg, _ := rt.NewTelegramBot(integration.TelegramOptions{
  BotToken: os.Getenv("MISTER_MORPH_TELEGRAM_BOT_TOKEN"),
  Hooks: integration.TelegramHooks{
    OnInbound: func(ev integration.TelegramInboundEvent) {
      fmt.Printf("telegram inbound: %+v\n", ev)
    },
    OnOutbound: func(ev integration.TelegramOutboundEvent) {
      fmt.Printf("telegram outbound: %+v\n", ev)
    },
    OnError: func(ev integration.TelegramErrorEvent) {
      fmt.Printf("telegram error: %+v\n", ev)
    },
  },
})
_ = tg
```

### Slack

```go
sl, _ := rt.NewSlackBot(integration.SlackOptions{
  BotToken: os.Getenv("MISTER_MORPH_SLACK_BOT_TOKEN"),
  AppToken: os.Getenv("MISTER_MORPH_SLACK_APP_TOKEN"),
})
_ = sl
```
