# CODEX.md — OpenUsage

Instructions for Codex when working in this repository.

## Project Overview

OpenUsage is a Go terminal dashboard for monitoring AI coding tool usage/spend.
It uses Bubble Tea for UI and a local telemetry daemon for ongoing data collection.
CGO is required (`CGO_ENABLED=1`) due to `mattn/go-sqlite3` (Cursor + telemetry store).

## Commands

```bash
# Build / run
make build
make run
make demo

# Quality checks
make fmt
make vet
make lint
make test
make test-verbose

# Focused tests
go test ./internal/providers/... -v
go test ./internal/telemetry/... -v
go test ./internal/tui/... -v

# Daemon / telemetry
go run ./cmd/openusage telemetry daemon
go run ./cmd/openusage telemetry daemon status
go run ./cmd/openusage telemetry hook codex < /tmp/payload.json
```

## Architecture Snapshot

- CLI entrypoint: `cmd/openusage/main.go`
- Dashboard runtime wiring: `cmd/openusage/dashboard.go`
- Telemetry subcommands: `cmd/openusage/telemetry.go`
- Core interface: `core.UsageProvider` in `internal/core/provider.go`
- Provider registry: `internal/providers/registry.go` (16 providers)
- Auto-detection: `internal/detect/`
- Telemetry/daemon pipeline: `internal/daemon/` + `internal/telemetry/`
- TUI rendering: `internal/tui/`
- Config: `~/.config/openusage/settings.json`
- Credentials: `~/.config/openusage/credentials.json`

## Coding Conventions

- Use `gofmt` style and grouped imports (stdlib, third-party, internal).
- Alias Bubble Tea as `tea`.
- Wrap errors with provider/context prefixes (`fmt.Errorf("openai: ...: %w", err)`).
- Use pointer numerics for optional metrics (`*float64`).
- Keep runtime-only fields non-serializable (`json:"-"`).
- Tests: standard `testing`, table-driven patterns, `httptest.NewServer`, `t.TempDir`.

## Provider Contract

Each provider must implement:

- `ID()`
- `Describe()`
- `Spec()`
- `DashboardWidget()`
- `DetailWidget()`
- `Fetch(ctx, acct) (core.UsageSnapshot, error)`

Status-first behavior is preferred for non-fatal failures (`StatusAuth`, `StatusLimited`) with a usable `UsageSnapshot`.

## Skills and Workflows

- New provider workflow: `docs/skills/add-new-provider.md` (mandatory quiz first).
- End-to-end feature lifecycle:
  - `docs/skills/develop-feature/SKILL.md`
  - `docs/skills/design-feature/SKILL.md`
  - `docs/skills/review-design/SKILL.md`
  - `docs/skills/implement-feature/SKILL.md`
  - `docs/skills/validate-feature/SKILL.md`
  - `docs/skills/iterate-feature/SKILL.md`
  - `docs/skills/finalize-feature/SKILL.md`
