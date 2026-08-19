---
title: Filesystem Roots
description: What workspace_dir, file_cache_dir, and file_state_dir mean, and how Mistermorph uses them.
---

# Filesystem Roots

Mistermorph uses three different filesystem roots for three different jobs:

- `workspace_dir`: the attached project directory for the current session or topic.
- `file_cache_dir`: rebuildable cache files.
- `file_state_dir`: persistent runtime state.

Keep them separate. The model behaves better when the project tree, temporary files, and runtime state do not blur together.

## The Three Roots

| Root | What it means | Typical contents | Persistence |
|---|---|---|---|
| `workspace_dir` | The active project tree the agent should treat as "the thing I am working on". This is runtime-scoped, not a global config key. | source code, docs, config files, project notes | usually user-managed |
| `file_cache_dir` | Rebuildable files created while the system runs | downloads, temporary conversions, fetched artifacts, generated media | safe to prune |
| `file_state_dir` | Files that should survive restarts and preserve agent state | tasks, contacts, skills, guard state, workspace attachments | keep persistent |

By default:

- `file_state_dir` is `~/.morph`
- `file_cache_dir` is `~/.cache/morph`

`workspace_dir` is different. It is attached by the current runtime instead of coming from one global config field.

## How `workspace_dir` Is Attached

In CLI chat:

```bash
mistermorph chat --workspace .
```

Current behavior:

- `mistermorph chat` attaches the current working directory by default.
- `mistermorph chat --workspace <dir>` attaches a specific directory.
- `mistermorph chat --no-workspace` starts without a workspace attachment.
- Inside chat, `/workspace`, `/workspace attach <dir>`, and `/workspace detach` inspect or change the attachment.

Other runtimes can provide `workspace_dir` in their own way. The important point is the same: `workspace_dir` describes the project tree for the current conversation, not the global state home.

## Path Aliases

These aliases make the target root explicit:

- `workspace_dir/...`
- `file_cache_dir/...`
- `file_state_dir/...`

Use them when you want to avoid ambiguity, especially in prompts, scripts, or tool parameters.

## How Tools Resolve Paths

### `read_file`

- Relative paths resolve under `workspace_dir` when one is attached.
- Without a workspace attachment, relative paths resolve under `file_cache_dir`.
- Aliases can force a specific root.

### `write_file`

- Relative paths resolve under `workspace_dir` when one is attached.
- Without a workspace attachment, relative paths resolve under `file_cache_dir`.
- Writes are restricted to `workspace_dir`, `file_cache_dir`, or `file_state_dir`.
- Aliases can force a specific root.

### `bash` and `powershell`

- Command text can use `workspace_dir`, `file_cache_dir`, and `file_state_dir` aliases.
- When shell `cwd` is omitted and `workspace_dir` exists, the shell starts there by default.

This is why `workspace_dir` matters even when you are not explicitly calling `read_file` or `write_file`.

## Practical Rules

- Put the repo or project you want the agent to edit in `workspace_dir`.
- Put temporary downloads and throwaway generated files in `file_cache_dir`.
- Put contacts, installed skills, task state, and other durable runtime files in `file_state_dir`.

## Related Pages

- [CLI Flags](/guide/cli-flags)
- [Config Fields](/guide/config-reference)
- [Built-in Tools](/guide/built-in-tools)
- [Config Patterns](/guide/config-patterns)
