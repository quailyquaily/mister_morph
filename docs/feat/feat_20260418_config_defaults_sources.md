---
date: 2026-04-18
title: Config Defaults Sources
status: draft
---

# Config Defaults Sources

## 1) Goal

This note explains where configuration defaults come from today, why there are two defaulting entrypoints, and where each one is used.

This is intentionally documented outside the current PowerShell PR scope.

## 2) Current Defaulting Entrypoints

There are currently two functions that apply defaults to a `viper.Viper` instance:

- `internal/configdefaults.Apply`
- `integration.ApplyViperDefaults`

They overlap heavily.

Both set defaults for common runtime and tool keys such as:

- `llm.*`
- agent limits like `max_steps`
- file state/cache directories
- `tools.read_file.*`
- `tools.write_file.*`
- `tools.bash.*`
- `tools.powershell.*`
- `tools.url_fetch.*`
- `tools.web_search.*`

## 3) `internal/configdefaults.Apply`

This is the main program default source for the global CLI/runtime `viper`.

Primary uses:

- CLI initialization through `initConfig -> initViperDefaults`
- main registry loading from the global `viper`

Relevant code:

- `cmd/mistermorph/defaults.go`
- `cmd/mistermorph/root.go`
- `cmd/mistermorph/registry.go`

Operationally, this is the defaulting path for the normal application runtime:

- `run`
- `console`
- `telegram`
- `slack`
- `line`
- `lark`

as long as they are reading from the process-wide `viper`.

## 4) `integration.ApplyViperDefaults`

This is used when code creates a fresh temporary `viper` instance and wants a self-contained config reader.

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

## 5) Why Two Sources Exist

From a first-principles view, the two functions solve different construction modes:

- one for the global process config
- one for isolated temporary readers

That distinction is reasonable.

What is not ideal is that the defaults themselves are duplicated instead of sharing a single authority.

## 6) Current Problem

The current issue is not that there are two call paths.
The current issue is that both paths each define defaults directly.

That means whenever a new config key is added, especially under `tools.*`, there is a risk of:

- updating one defaults function but not the other
- drifting platform-specific behavior
- drifting Console Settings behavior from main runtime behavior

This is the structural reason the PowerShell work had to touch both files.

## 7) Recommended Direction

The likely cleanup direction is:

- keep both call paths
- reduce to one authority for shared defaults

Possible shape:

- `internal/configdefaults.Apply` remains the shared source of truth
- `integration.ApplyViperDefaults` calls into it first
- `integration.ApplyViperDefaults` only adds integration-specific defaults if it truly owns any

This keeps the useful construction split without duplicating the actual default values.

## 8) Recommendation For Scope

This should be treated as a follow-up cleanup, not part of the PowerShell feature delivery itself.

Reason:

- it changes configuration architecture
- it is broader than shell-tool support
- it deserves a refactor-only change with regression checks around settings loading
