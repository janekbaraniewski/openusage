# AGENTS.md — OpenUsage

Guidelines for coding agents working in this repository.

## Project Overview

OpenUsage is a Go terminal dashboard (Bubble Tea) for monitoring AI coding tool usage and spend.
It supports two runtime paths:

- Dashboard mode (`openusage`) reads usage snapshots through the local telemetry daemon runtime.
- Daemon mode (`openusage telemetry ...`) collects provider data + hook events into SQLite.

CGO is required (`CGO_ENABLED=1`) due to `mattn/go-sqlite3` usage (Cursor + telemetry store).

## Build, Run, Test, Lint

```bash
# Build
make build                    # binary -> ./bin/openusage (with version ldflags)
go build ./cmd/openusage      # quick build without ldflags

# Run
make run                      # go run cmd/openusage/main.go
OPENUSAGE_DEBUG=1 make run    # enable debug logging to stderr
make demo                     # run demo binary with simulated data

# Telemetry daemon
go run ./cmd/openusage telemetry daemon
go run ./cmd/openusage telemetry daemon status
go run ./cmd/openusage telemetry hook codex < /tmp/codex-hook.json

# Test
make test                     # go test -race -coverprofile=coverage.out -covermode=atomic ./...
make test-verbose             # go test -v -race ./...
go test ./internal/providers/... -v
go test ./internal/telemetry/... -v
go test ./internal/tui/... -v

# Lint/format
make lint                     # golangci-lint run ./... (skips if binary missing)
make vet                      # go vet ./...
make fmt                      # go fmt ./...

# Dependencies
make deps                     # go mod download && go mod verify
make tidy                     # go mod tidy
```

## Current Structure

```text
cmd/openusage/          cobra CLI entrypoint
  main.go               root command
  dashboard.go          Bubble Tea runtime wiring
  telemetry.go          telemetry daemon / hook subcommands
cmd/demo/               demo dashboard runner with synthetic snapshots
internal/
  config/               settings + credentials JSON persistence
  core/                 shared types (UsageSnapshot, Metric, ProviderSpec, widgets, time windows)
  daemon/               daemon server/client, socket runtime, service install/status
  detect/               local tool + env key auto-detection
  integrations/         Codex/OpenCode/Claude hook/plugin install + version checks
  parsers/              shared HTTP header parsing helpers
  providers/            provider implementations + registry (16 providers)
  telemetry/            SQLite store, ingest pipeline, dedup, read model
  tui/                  Bubble Tea views/components/settings UX
  version/              build metadata injected by ldflags
plugins/                integration install scripts/templates
configs/                example settings
docs/skills/            feature and provider implementation workflows
```

## Provider Model

All providers implement `core.UsageProvider` from `internal/core/provider.go`:

- `ID()`, `Describe()`, `Spec()`
- `DashboardWidget()`, `DetailWidget()`
- `Fetch(ctx, acct) (core.UsageSnapshot, error)`

Providers are registered in `internal/providers/registry.go` via `AllProviders()`.
Current provider set: `openai`, `anthropic`, `alibaba_cloud`, `openrouter`, `groq`,
`mistral`, `deepseek`, `xai`, `opencode`, `gemini_api`, `gemini_cli`,
`ollama`, `copilot`, `cursor`, `claude_code`, `codex`.

## Code Style

- Format with `gofmt` and keep imports grouped: stdlib, third-party, internal.
- Bubble Tea import alias: `tea "github.com/charmbracelet/bubbletea"`.
- Provider package naming: snake_case IDs (for example `claude_code`, `gemini_api`).
- Use pointer numerics for optional values (`Limit *float64`, `Remaining *float64`).
- Initialize maps explicitly (`make(map[string]core.Metric)`).
- JSON fields use `snake_case`; runtime-only fields use `json:"-"`.
- Prefer provider-specific error prefixes, for example `fmt.Errorf("openai: ...: %w", err)`.

## Error and Status Conventions

- Non-fatal provider states should return populated snapshots with status:
  - `core.StatusAuth`
  - `core.StatusLimited`
  - `core.StatusError` (when provider returns a handled API error state)
- Fatal execution failures should return `(core.UsageSnapshot{}, error)`.
- Missing auth should degrade gracefully to auth-required snapshots where possible.
- `UsageSnapshot` is the canonical payload (not `QuotaSnapshot`).

## Concurrency Expectations

- Daemon service uses mutex-protected shared state (`pipelineMu`, `ingestMu`, read-model cache locks).
- Provider polling, collectors, retention, and spool maintenance run in background goroutines.
- `ViewRuntime` throttles reconnect attempts and read-model error logs.
- Config and credential read-modify-write operations are guarded by package-level mutexes (`saveMu`, `credMu`).

## Testing Patterns

- Use standard `testing` package only (no assertion/mocking libraries).
- Prefer table-driven tests with `t.Run`.
- Provider HTTP tests should use `httptest.NewServer`.
- File/dir tests should use `t.TempDir`.
- Keep env var setup isolated and cleaned up in each test.
- `internal/tui/`, `internal/telemetry/`, `internal/daemon/`, and providers all have active test suites.

## Security and Data Handling

- Never print raw API keys in logs.
- Settings are stored at `~/.config/openusage/settings.json` (or `%APPDATA%\openusage\settings.json` on Windows).
- User-entered API keys are stored in `credentials.json` with `0600` permissions.
- `AccountConfig.Token` and `AccountConfig.ExtraData` are runtime-only (`json:"-"`).
- Redact or avoid storing sensitive headers/body data in snapshot diagnostics/raw metadata.

## Adding a Provider

For complete workflow and checklists, follow:
[`docs/skills/add-new-provider.md`](docs/skills/add-new-provider.md)

Minimum sequence:

1. Quiz requirements and auth/data model.
2. Research API/docs/error semantics.
3. Implement `internal/providers/<name>/` with tests.
4. Add widget/detail config (`Spec`, optional `widget.go`).
5. Register in `registry.go`, detection in `internal/detect/`, and example config.
6. Verify with `go build`, `go test`, `make vet`.

## Repo Skills

For larger changes, use these workflow docs in `docs/skills/`:

- `develop-feature`
- `design-feature`
- `review-design`
- `implement-feature`
- `validate-feature`
- `iterate-feature`
- `finalize-feature`
- `openusage-provider` (provider-specific guidance)

## Commit Style

- Use short imperative subjects (for example: `Refresh telemetry provider mapping docs`).
- Include the validation commands you ran in PR descriptions.
