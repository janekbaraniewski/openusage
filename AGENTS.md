# AGENTS.md — OpenUsage

Guidelines for AI coding agents working in this repository.

## Project Overview

OpenUsage is a Go terminal dashboard (TUI) for monitoring AI coding tool usage and spend.
It auto-detects installed AI tools and API keys, then displays live data via a Bubble Tea TUI.
CGO is required (`CGO_ENABLED=1`) because the Cursor provider uses `mattn/go-sqlite3`.

## Build, Test, and Lint Commands

```bash
# Build
make build                    # binary → ./bin/openusage (includes version ldflags)
go build ./cmd/openusage      # quick build without ldflags

# Run
make run                      # go run cmd/openusage/main.go
OPENUSAGE_DEBUG=1 make run    # with debug logging to stderr

# Test — all
make test                     # go test -race -coverprofile=coverage.out -covermode=atomic ./...
make test-verbose             # go test -v -race ./...

# Test — single test
go test ./internal/providers/openai/ -run TestFetch_ParsesHeaders -v

# Test — single package
go test ./internal/providers/openai/ -v
go test ./internal/config/ -v

# Test — all providers
go test ./internal/providers/... -v

# Lint
make lint                     # golangci-lint run ./... (skips if not installed)
make vet                      # go vet ./...
make fmt                      # go fmt ./...

# Dependencies
make deps                     # go mod download && go mod verify
make tidy                     # go mod tidy
```

## Project Structure

```
cmd/openusage/          CLI entry point (main.go)
internal/
  config/               JSON config loading, defaults, save (settings.json)
  core/                 Engine, QuotaProvider interface, types (Metric, QuotaSnapshot, Status)
  detect/               Auto-detection of tools & API keys
  parsers/              Shared HTTP rate-limit header parsing helpers
  providers/            One sub-package per vendor (13 total)
    registry.go         AllProviders() — registers all provider adapters
    openai/             HTTP header probing pattern
    anthropic/          HTTP header probing pattern
    groq/               HTTP header probing pattern
    mistral/            REST API pattern
    deepseek/           REST API pattern
    xai/                REST API pattern
    gemini_api/         REST API pattern
    gemini_cli/         Local file reader + OAuth
    openrouter/         Rich REST API pattern
    cursor/             REST API + local SQLite fallback
    claude_code/        Local file reader (most complex, ~900 lines)
    codex/              Local file reader
    copilot/            CLI subprocess (gh CLI)
  tui/                  Bubble Tea UI (views, themes, charts, gauges)
  version/              Build-time version metadata (ldflags)
configs/                Example configuration file
```

## Code Style

### Formatting and Imports

- **Formatter**: `gofmt` with `goimports` (enforced by golangci-lint). Tabs for indentation.
- **Import groups** (separated by blank lines, in this order):
  1. Standard library (`context`, `fmt`, `net/http`, etc.)
  2. Third-party (`github.com/charmbracelet/bubbletea`, etc.)
  3. Internal (`github.com/janekbaraniewski/openusage/internal/...`)
- Bubble Tea is aliased as `tea`: `tea "github.com/charmbracelet/bubbletea"`

### Naming Conventions

- **Exported**: `CamelCase` — `QuotaProvider`, `NewEngine`, `StatusOK`
- **Unexported**: `camelCase` — `defaultBaseURL`, `saveMu`, `mergeAccounts`
- **Constants**: grouped in `const` blocks. Status values are typed strings: `StatusOK`, `StatusAuth`, etc.
- **Provider packages**: named after the vendor in `snake_case` (`claude_code`, `gemini_api`, `gemini_cli`).
  Each exports a `Provider` struct with a `New()` constructor.
- **Test functions**: `TestXxx` or `TestXxx_SubCase` (underscore for scenario variants).

### Types and Patterns

- **Pointer fields for optional numerics**: `Limit *float64`, `Remaining *float64` — nil means "not available".
  Use helper functions like `float64Ptr(v float64) *float64` in tests.
- **JSON tags**: `json:"field_name"` with `snake_case`. Use `omitempty` for optional fields.
  Fields that must never be serialized use `json:"-"` (e.g., `Token`, `ExtraData`).
- **Maps initialized with `make`**: `Metrics: make(map[string]core.Metric)`.
- **Core interface**: All providers implement `core.QuotaProvider` (ID, Describe, Fetch).
- **Config**: JSON-based (`settings.json`). `DefaultConfig()` provides zero-value defaults.

### Error Handling

- **Wrap errors with context**: `fmt.Errorf("openai: creating request: %w", err)` — prefix with provider name.
- **Return typed snapshots for non-fatal errors**: Auth failures and rate limits return a valid `QuotaSnapshot`
  with `Status: core.StatusAuth` or `core.StatusLimited` and `err == nil`.
- **Return `(QuotaSnapshot{}, error)` for fatal errors**: Network failures, request creation failures.
- **Graceful degradation**: Missing API keys produce `StatusAuth` snapshots, not errors.
- **Log warnings, don't crash**: `log.Printf("Warning: ...")` for non-critical issues.
  Debug logging goes to stderr and is gated by `OPENUSAGE_DEBUG=1`.

### Concurrency

- **Engine uses `sync.RWMutex`** to protect providers, accounts, and snapshots maps.
- **Config file writes** are guarded by a package-level `sync.Mutex` (`saveMu`).
- **Provider fetches run concurrently** via goroutines in `Engine.RefreshAll()`.
  Each fetch gets its own `context.WithTimeout` (5s default).

## Testing Patterns

- **Standard `testing` package only** — no testify, no mocking frameworks.
- **Table-driven tests**: Use `[]struct{ name string; ... }` with `t.Run(tt.name, ...)`.
  See `core/types_test.go` for the canonical example.
- **HTTP mocking**: Use `net/http/httptest.NewServer` with controlled headers/responses.
  See `providers/openai/openai_test.go`.
- **Temp files**: Use `t.TempDir()` for filesystem tests. See `config/config_test.go`.
- **Env vars in tests**: `os.Setenv` / `os.Unsetenv` with `defer` cleanup.
  Use `TEST_` prefixed env var names (e.g., `TEST_OPENAI_KEY`).
- **Assertions**: Direct `if got != want` with `t.Errorf` / `t.Fatalf`. No assertion libraries.
- **No TUI tests** exist — the `internal/tui/` package is untested.

## Adding a New Provider

For the **full specification** — including a mandatory user quiz, research phase,
implementation templates, widget design guidelines, test templates, and verification
checklist — see: **[`docs/skills/add-new-provider.md`](docs/skills/add-new-provider.md)**

Quick summary of the phases:

1. **Phase 0 — Quiz**: Gather provider name, auth method, env var, data sources, metrics, docs URL, color role.
2. **Phase 1 — Research**: Study the provider's API docs, headers, JSON schemas, error codes.
3. **Phase 2 — Create package**: `internal/providers/<name>/` with `<name>.go`, `<name>_test.go`, optional `widget.go`.
4. **Phase 3 — Widget**: Configure dashboard tile (gauges, compact rows, color role, label overrides).
5. **Phase 4 — Register**: Add to `registry.go`, `detect.go`, `example_settings.json`.
6. **Phase 5 — Tests**: Minimum 3 tests (success, auth-required, rate-limited) using `httptest.NewServer`.
7. **Phase 6 — Verify**: `go build`, `go test`, `make vet`.

## Security

- **Never log or persist API keys.** Keys are referenced by env var name (`api_key_env`), resolved at runtime.
- **Redact sensitive headers**: Use `parsers.RedactHeaders()` when storing raw response data.
- Config path: `~/.config/openusage/settings.json`.

## Commit Style

- Short, imperative subjects (e.g., "Add Gemini CLI parser", "Fix rate limit header parsing").
- Include test results (`go test ./...`) in PR descriptions.


## Skills
A skill is a set of local instructions to follow that is stored in a `SKILL.md` file. Below is the list of skills that can be used. Each entry includes a name, description, and file path so you can open the source for full instructions when using a specific skill.
### Available skills
- skill-creator: Guide for creating effective skills. This skill should be used when users want to create a new skill (or update an existing skill) that extends Codex's capabilities with specialized knowledge, workflows, or tool integrations. (file: /Users/janekbaraniewski/.codex/skills/.system/skill-creator/SKILL.md)
- skill-installer: Install Codex skills into $CODEX_HOME/skills from a curated list or a GitHub repo path. Use when a user asks to list installable skills, install a curated skill, or install a skill from another repo (including private repos). (file: /Users/janekbaraniewski/.codex/skills/.system/skill-installer/SKILL.md)
### How to use skills
- Discovery: The list above is the skills available in this session (name + description + file path). Skill bodies live on disk at the listed paths.
- Trigger rules: If the user names a skill (with `$SkillName` or plain text) OR the task clearly matches a skill's description shown above, you must use that skill for that turn. Multiple mentions mean use them all. Do not carry skills across turns unless re-mentioned.
- Missing/blocked: If a named skill isn't in the list or the path can't be read, say so briefly and continue with the best fallback.
- How to use a skill (progressive disclosure):
  1) After deciding to use a skill, open its `SKILL.md`. Read only enough to follow the workflow.
  2) When `SKILL.md` references relative paths (e.g., `scripts/foo.py`), resolve them relative to the skill directory listed above first, and only consider other paths if needed.
  3) If `SKILL.md` points to extra folders such as `references/`, load only the specific files needed for the request; don't bulk-load everything.
  4) If `scripts/` exist, prefer running or patching them instead of retyping large code blocks.
  5) If `assets/` or templates exist, reuse them instead of recreating from scratch.
- Coordination and sequencing:
  - If multiple skills apply, choose the minimal set that covers the request and state the order you'll use them.
  - Announce which skill(s) you're using and why (one short line). If you skip an obvious skill, say why.
- Context hygiene:
  - Keep context small: summarize long sections instead of pasting them; only load extra files when needed.
  - Avoid deep reference-chasing: prefer opening only files directly linked from `SKILL.md` unless you're blocked.
  - When variants exist (frameworks, providers, domains), pick only the relevant reference file(s) and note that choice.
- Safety and fallback: If a skill can't be applied cleanly (missing files, unclear instructions), state the issue, pick the next-best approach, and continue.
