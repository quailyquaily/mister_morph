---
date: 2026-04-18
title: Config Defaults Sources
status: draft
---

# Config Defaults Sources

## 1) Goal

This note records the default-config decision for `mistermorph`.

The target is simple:

- there is only one source of truth for shared defaults
- the default process runtime and `integration` see the same defaults
- config snapshot lifecycle is separate from config file editing

This note is intentionally limited to defaults and snapshot boundaries.
It does not define a hot-reload mechanism.

## 2) Defaulting Shape

There are still two call paths that can write defaults into a `viper.Viper` instance:

- `internal/configdefaults.Apply`
- `integration.ApplyViperDefaults`

But they no longer define defaults independently.

The shared authority is:

- `internal/configdefaults.Apply`

The compatibility entrypoint is:

- `integration.ApplyViperDefaults`

`integration.ApplyViperDefaults` delegates to `internal/configdefaults.Apply`.
It exists because `integration` is a public package and some callers want a direct helper that writes defaults into their own `viper` instance.

## 3) Shared Authority

`internal/configdefaults.Apply` is the repo-wide authority for shared defaults.

It defines shared runtime defaults such as:

- `llm.*`
- agent limits like `max_steps`
- file state/cache directories
- `logging.*`
- `tasks.*`
- `multimodal.image.sources`
- `console.*`
- `telegram.*`
- `slack.*`
- `line.*`
- `lark.*`
- `tools.*`

Primary uses:

- CLI initialization through `initConfig -> initViperDefaults`
- main registry loading from the global `viper`
- isolated readers through delegation from `integration.ApplyViperDefaults`

Relevant code:

- `cmd/mistermorph/defaults.go`
- `cmd/mistermorph/root.go`
- `cmd/mistermorph/registry.go`
- `internal/configdefaults/defaults.go`

## 4) `integration.ApplyViperDefaults`

This remains the public helper for callers that construct a fresh `viper` instance and want the standard default set.

It is not an independent authority.
It is a compatibility and convenience entrypoint for `integration`.

Primary uses:

- `integration` runtime snapshot loading
- Console Settings read/default/expand flows
- Agent Settings read/default/validate flows
- setup repair config reload

Relevant code:

- `integration/runtime_snapshot_loader.go`
- `cmd/mistermorph/consolecmd/console_settings.go`
- `cmd/mistermorph/consolecmd/agent_settings.go`
- `cmd/mistermorph/consolecmd/setup_repair.go`

Operationally, this path is mostly for:

- local config editing
- preview/default payload generation
- config validation against a temporary reader
- integration runtime bootstrapping from explicit overrides

Because it delegates to the shared authority, the default process runtime and `integration` now see the same defaults.

## 5) Logging Defaults

`logging.*` belongs to the shared default set and should live in `internal/configdefaults.Apply`.

CLI logging flags are an input surface, not an independent default authority.
Their job is to override when the user explicitly passes a flag.
They should not carry a separate runtime truth.

## 6) Why This Cleanup Was Needed

Before this cleanup, the two call paths had already drifted.

Examples that had already diverged:

- `multimodal.image.sources` existed in one path but not the other
- `logging.*` existed in a different place from the rest of the shared defaults
- at least one shared key had conflicting values:
  - `telegram.addressing_interject_threshold`

That kind of duplication is enough to make generated config, console views, and runtime behavior disagree.

## 7) Snapshot Boundary

Defaults authority and snapshot lifecycle are related but not the same concern.

The intended runtime model is:

- each runtime operates on its own config snapshot
- a snapshot is rebuilt from resolved config input
- each runtime is responsible for its own concurrency safety

The intended config editing model is:

- Console Web API edits `config.yaml`
- editing `config.yaml` does not itself define snapshot refresh semantics

In other words:

- config mutation and snapshot regeneration should not be fused into one hidden responsibility
- reload, restart, file watch, or explicit regenerate flows should be treated as a separate design decision
