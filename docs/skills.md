---
title: Skills
---

# Skills

`mistermorph` supports “skills”: small, self-contained folders that contain a `SKILL.md` file (required) plus optional scripts/resources. Skills are discovered from a set of root directories and can be loaded automatically or referenced explicitly for one run.

Important: a skill is **not automatically a tool**. Skills add prompt context; tools are registered separately (e.g. `url_fetch`, `web_search`). If a skill includes scripts that you want the agent to execute, you must enable the `bash` tool (or implement a dedicated tool).

## Discovery and priority (dedupe)

Skills are discovered by scanning roots recursively for `SKILL.md`.

Default root:

1. `file_state_dir/skills` (usually `~/.morph/skills`)

You can add custom roots via `--skills-dir` when listing or running.

## Listing skills

- List: `mistermorph skills list`

## How skills are chosen

Skill loading is controlled by `skills.enabled`:

- `false`: do not load skills automatically or from config/flags; an explicit `$name` task reference still loads that skill for the run
- `true`: load skills requested by config/flags, or by an explicit `$name` task reference

You can request skills via config:

- `skills.load: ["some-skill-id", "some-skill-name"]`
- `skills.load: []` means load all discovered skills
- Unknown skill names in `skills.load` are ignored

## Installing / updating built-in skills

`mistermorph` ships some built-in skills under `assets/skills/`. To install (or update) them into your user skills directory:

- `mistermorph skills install`

By default this writes to `~/.morph/skills`.

Useful flags:

- `--dry-run`: print what would be written
- `--clean`: remove an existing skill directory before copying (destructive)
- `--dest <dir>`: install somewhere else (useful for testing)

After installation, the built-in skills are picked up automatically via the default roots.

## Installing a remote SKILL.md (single-file)

If you have a URL that points directly to a `SKILL.md` file, you can install/update it into `~/.morph/skills`:

- `mistermorph skills install "https://example.com/skill.md"`

Notes:

- The installer first prints the remote `SKILL.md` and asks for confirmation.
- Then it uses the configured LLM to review the file (treating it as untrusted) and extract any explicitly required additional downloads (e.g. `scripts/...`).
- Before writing anything, it prints a file plan + potential risks and asks for confirmation again.
- Safety: downloaded files are only written to disk; they are **not executed** during install.
- The destination folder name is exactly the `name:` in the YAML frontmatter (must match `[A-Za-z0-9_.-]+`).
- All downloaded files are written under `~/.morph/skills/<name>/` (no paths outside the skills directory).

## Using a skill

- Use `--skill <name-or-id>` for one run.
- Mention `$skill-name` or `$skill-id` in the task text to trigger that skill for the run. Explicit task references also work when `skills.enabled=false`.
- Or add it to `skills.load` for always-on behavior (`[]` means all).
- If `$name` does not match any skill or tool, it remains ordinary user text. It does not produce an error or a "not found" message.
