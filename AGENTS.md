# Repository Guidelines

## Project Structure & Module Organization

- `cmd/mistermorph/`: CLI entrypoint and subcommands.
- `agent/`: agent engine (loop, parsing, prompts, logging).
- `llm/`: shared LLM types/interfaces used by the agent and providers.
- `providers/`: LLM backends (currently `providers/uniai/`).
- `tools/` and `tools/builtin/`: tool registry and built-ins (`web_search`, `url_fetch`, `bash`, `read_file`, `echo`).
- `skills/`: skill discovery and selection logic for `SKILL.md`.
- `demo/`: embedding examples (`demo/embed-go/`, `demo/embed-cli/`).
- Root configs: `config.example.yaml` (template) and `config.yaml` (local). `mistermorph` in repo root is a build artifact.

## Build, Test, and Development Commands

- Build: `go build -o ./bin/mistermorph ./cmd/mistermorph`
- Run (no build): `go run ./cmd/mistermorph --help`
- Test: `go test ./...` (currently no `*_test.go` files)
- Static checks: `go vet ./...`
- Example run:
  - `./bin/mistermorph run --task "Summarize this repo" --provider openai --model gpt-4o-mini --api-key "$OPENAI_API_KEY"`

## Demos (Embedding)

- Go library demo: `cd demo/embed-go && OPENAI_API_KEY="..." go run . --task "List files and summarize."`
- CLI subprocess demo: `go build -o ./bin/mistermorph ./cmd/mistermorph`, then `cd demo/embed-cli && MISTER_MORPH_BIN=../../bin/mistermorph OPENAI_API_KEY="..." go run . --task "Search for OpenAI and fetch the first result"`
- Both demos require network access (OpenAI-compatible provider).

## Coding Style & Naming Conventions

- Format Go code with `gofmt` (`go fmt ./...` is fine for day-to-day).
- Package names are lowercase; exported identifiers use `PascalCase`; locals use `camelCase`.
- Keep provider-specific behavior in `providers/<name>/` and avoid leaking it into `agent/`.

## Testing Guidelines

- Use the standard `testing` package; colocate tests as `*_test.go` next to the code they cover.
- Prefer table-driven tests and subtests (`t.Run("case", ...)`) for tool/provider edge cases.

## Agent Hooks & Webhooks

- There’s no built-in inbound webhook/HTTP server.
- The engine supports step hooks via `agent.Hook` + `agent.WithHook(...)` (runs once per step, before the LLM call).
- For outgoing webhooks and complex HTTP needs, implement it in a hook or use a tool; built-in `url_fetch` supports GET/POST/PUT/DELETE with optional headers/body.

## Security & Configuration Tips

- Prefer `MISTER_MORPH_API_KEY` over committing `api_key` in config; treat logs as sensitive when enabling debug/thought output.
- When adding/changing config keys, update `config.example.yaml` (the template used for docs and examples).

## Commit & Pull Request Guidelines

- This checkout may not include Git history; if it does, match existing conventions. Otherwise, use Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`).
- PRs should include: a clear description of the behavior change, how to run it locally, and any config/flag updates.
