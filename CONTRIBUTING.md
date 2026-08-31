# Contributing to OpenUsage

Thanks for helping improve OpenUsage. This project is a Go terminal dashboard and local telemetry daemon for tracking AI
coding tool usage, spend, quotas, and session activity.

## Before opening a pull request

- Search existing issues and pull requests before starting larger work.
- Open an issue or design note first for broad UI, telemetry, provider, storage, or workflow changes.
- Keep pull requests focused on one feature, fix, provider, or documentation change.
- Avoid committing credentials, local telemetry databases, API responses with sensitive headers, or user-specific config.

## Local setup

OpenUsage uses CGO because the Cursor and telemetry stores use SQLite through `mattn/go-sqlite3`.

```bash
make deps
CGO_ENABLED=1 go build ./cmd/openusage
```

Useful commands:

```bash
make build
make run
make demo
make test
make vet
make fmt
```

`make lint` runs `golangci-lint` when the binary is installed and skips with a warning otherwise.

## Development workflow

Use the existing package boundaries:

- CLI wiring lives in `cmd/openusage/`.
- Provider implementations live in `internal/providers/<provider>/`.
- Telemetry daemon, ingest, deduplication, and read models live in `internal/telemetry/` and `internal/daemon/`.
- Bubble Tea views and components live in `internal/tui/`.
- Integration scripts and templates live in `plugins/`.
- Website and docs-site work lives under `website/` and `docs/site/`.

Provider changes should follow `docs/skills/add-new-provider.md` and update registration, detection, examples, and tests where
applicable.

## Testing expectations

- Use the standard Go `testing` package.
- Prefer table-driven tests with `t.Run`.
- Use `httptest.NewServer` for provider HTTP tests.
- Use `t.TempDir` for filesystem tests.
- Isolate and clean up environment variables in each test.
- Run targeted tests while developing, then run the broad checks before review:

```bash
go test ./internal/providers/... -v
go test ./internal/telemetry/... -v
go test ./internal/tui/... -v
make test
make vet
```

## Code style

- Run `make fmt` before opening a pull request.
- Keep imports grouped as standard library, third-party, then internal packages.
- Use `tea` as the alias for `github.com/charmbracelet/bubbletea`.
- Use `snake_case` for provider IDs and JSON fields.
- Use pointer numerics for optional numeric values, such as `Limit *float64`.
- Return populated snapshots with `core.StatusAuth`, `core.StatusLimited`, or `core.StatusError` for handled provider states.
- Return an error for fatal execution failures.
- Prefix provider errors with the provider name, for example `fmt.Errorf("openai: ...: %w", err)`.

## Pull request checklist

- Describe the user-facing behavior change.
- Link related issues.
- Add or update tests for changed behavior.
- Update docs or example config when behavior changes.
- List validation commands you ran.
- Do not include raw API keys, tokens, cookies, or private telemetry data in logs, fixtures, screenshots, or diagnostics.
