# Integration

## Embedding to other projects

Two common integration options:

- As a Go library: see `demo/embed-go/`.
- As a subprocess CLI: see `demo/embed-cli/`.

For Go-library embedding with built-in wiring, use `integration`:

```go
cfg := integration.DefaultConfig()
cfg.BuiltinToolNames = []string{"read_file", "url_fetch", "todo_update"} // optional; empty = all built-ins
cfg.AddPromptBlock(`[[ Project Policy ]]
- Keep answers under 3 sentences unless asked for detail.`) // optional; appended under "Additional Policies"
cfg.Inspect.Prompt = true   // optional
cfg.Inspect.Request = true  // optional
cfg.Set("llm.routes", map[string]any{
	"main_loop": map[string]any{
		"candidates": []map[string]any{
			{"profile": "default", "weight": 1},
			{"profile": "cheap", "weight": 1},
		},
		"fallback_profiles": []string{"reasoning"},
	},
})
cfg.Set("llm.api_key", os.Getenv("OPENAI_API_KEY"))

rt := integration.New(cfg)

reg := rt.NewRegistry() // built-in tools wiring
prepared, err := rt.NewRunEngineWithRegistry(ctx, task, reg)
if err != nil { /* ... */ }
defer prepared.Cleanup()

final, runCtx, err := prepared.Engine.Run(ctx, task, agent.RunOptions{Model: prepared.Model})
_ = final
_ = runCtx
```

## Prompt blocks

`integration.Config.AddPromptBlock(...)` appends static prompt content into the system prompt's `Additional Policies` section.

Use it when you want integration-level customization without dropping down to `agent.New(...)`:

- project-wide safety or style rules
- tenant- or deployment-specific policies
- static instructions that should apply to one-shot runs and channel runtimes

The same configured blocks are applied to:

- `rt.NewRunEngine(...)`
- `rt.NewRunEngineWithRegistry(...)`
- `rt.RunTask(...)`
- `rt.NewTelegramBot(...)`
- `rt.NewSlackBot(...)`

If you need per-task dynamic prompt shaping or full prompt replacement, use the lower-level `agent` APIs instead.

## Route Policies

`integration.Config.Set(...)` can also configure the same route policies used by first-party runtimes.

Use `llm.routes.<purpose>` to choose one of:

- a fixed profile, for example `plan_create: reasoning`
- a weighted candidate list for per-run traffic split
- a route-local `fallback_profiles` chain

Example:

```go
cfg := integration.DefaultConfig()
cfg.Set("llm.profiles", map[string]any{
	"cheap": map[string]any{"model": "gpt-4.1-mini"},
	"reasoning": map[string]any{
		"provider": "xai",
		"model":    "grok-4.1-fast-reasoning",
		"api_key":  os.Getenv("XAI_API_KEY"),
	},
})
cfg.Set("llm.routes", map[string]any{
	"main_loop": map[string]any{
		"candidates": []map[string]any{
			{"profile": "default", "weight": 1},
			{"profile": "cheap", "weight": 1},
		},
		"fallback_profiles": []string{"reasoning"},
	},
	"think":       "reasoning",
	"plan_create": "reasoning",
})
```

When a route uses `candidates`, one primary profile is selected once for the current run and reused for that run's LLM calls. If the selected primary fails with a fallback-eligible error, the route's `fallback_profiles` are tried in order.

`/think <task>` resolves `llm.routes.think` when present, otherwise it uses the default profile. The run then temporarily sets `reasoning_effort` to `xhigh`.

## Runtime LLM Profile Selection

`integration.Runtime` also exposes runtime-level helpers for reading and overriding the current `main_loop` LLM profile selection:

- `rt.GetLLMProfileSelection()`
- `rt.ListLLMProfiles()`
- `rt.SetLLMProfile(profileName)`
- `rt.ResetLLMProfile()`

These methods are scoped to one `integration.Runtime` instance, not process-global state.

- If your host creates one runtime, the behavior feels global inside that host.
- If your host creates multiple runtimes, each runtime keeps its own selection state.
- `ResetLLMProfile()` clears the manual override and returns to the configured `llm.routes.main_loop` policy.

Example:

```go
selection, err := rt.GetLLMProfileSelection()
if err != nil { /* ... */ }

profiles, err := rt.ListLLMProfiles()
if err != nil { /* ... */ }

if len(profiles) > 0 {
	if err := rt.SetLLMProfile(profiles[0].Name); err != nil { /* ... */ }
}

rt.ResetLLMProfile()
```

`GetLLMProfileSelection()` returns a structured view of the current strategy, not always a single active profile.

- In `manual` mode, `Current` describes the forced main profile.
- In `auto` mode with `llm.routes.main_loop: some_profile`, `Current` describes that resolved profile.
- In `auto` mode with `llm.routes.main_loop.candidates`, `RouteType` is `candidates`, `Candidates` lists the weighted profiles, and `Current` may be empty because the configured strategy is not a single fixed profile.

This override only changes `main_loop`. Other purposes such as `plan_create` still follow config unless you explicitly change their routes.
