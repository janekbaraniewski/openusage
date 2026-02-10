# Repository Guidelines

## Project Structure & Module Organization
- `cmd/agentusage/` contains the CLI entry point (`main.go`).
- `internal/` holds the application code: `core/` (engine/types), `config/`, `providers/` (vendor adapters), `tui/` (Bubble Tea UI), `detect/` (auto-detection), and `parsers/` helpers.
- `configs/example.toml` is the reference configuration file.
- `agentusage.md` is the design doc with architecture and product notes.
- Tests live alongside code as `*_test.go` under `internal/`.

## Build, Test, and Development Commands
- `go build ./cmd/agentusage` — build the `agentusage` binary.
- `go run ./cmd/agentusage` — run the TUI locally.
- `go test ./...` — run all unit tests.
- `go test ./internal/providers/...` — run provider-specific tests only.

## Coding Style & Naming Conventions
- Use `gofmt` (tabs for indentation, standard Go formatting). Example: `gofmt -w internal/...`.
- Go naming conventions apply: exported identifiers are `CamelCase`, unexported are `camelCase`.
- Test files are named `*_test.go` and test functions follow `TestXxx`.

## Testing Guidelines
- Tests use the standard Go testing package; keep them deterministic and local.
- Provider tests focus on header/response parsing; add fixtures rather than real network calls.
- If you add new provider behavior, add or update tests in the corresponding `internal/providers/<provider>/` package.

## Commit & Pull Request Guidelines
- No commit history exists yet, so there is no established commit message convention. Use short, imperative subjects (e.g., “Add Gemini CLI parser”).
- PRs should include a concise summary, test command results (at least `go test ./...`), and config changes when relevant.
- UI changes should include a brief description of the visual impact; include screenshots if the TUI layout changes.

## Security & Configuration Tips
- Default config path is `~/.config/agentusage/config.toml` (see `configs/example.toml`).
- Avoid logging secrets; API keys should be provided via env vars and referenced with `api_key_env` in config.
