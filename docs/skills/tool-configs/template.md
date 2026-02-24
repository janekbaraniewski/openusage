# {{TOOL_TITLE}} — OpenUsage

## Project Overview

OpenUsage is a Go terminal dashboard (TUI) for monitoring AI coding tool usage and spend.
Built with Bubble Tea. CGO required (`CGO_ENABLED=1`) for `mattn/go-sqlite3`.

## Key Commands

```bash
make build          # build binary
make test           # run all tests with -race
make vet            # go vet
go test ./internal/providers/<name>/ -v  # test single provider
```

## Code Style

- Standard `gofmt` with `goimports`. Tabs for indentation.
- Import groups (separated by blank lines): stdlib, third-party, internal.
- Bubble Tea aliased as `tea`.
- Errors wrapped with provider prefix: `fmt.Errorf("openai: creating request: %w", err)`.
- Pointer fields for optional numerics: `Limit *float64`.
- JSON tags use `snake_case` with `omitempty` for optional fields.
- No mocking frameworks — use `httptest.NewServer` and table-driven tests.

## Architecture

Every provider implements `core.UsageProvider` (ID, Describe, Spec, DashboardWidget, DetailWidget, Fetch).
Providers registered in `internal/providers/registry.go` via `AllProviders()`.
Auto-detection in `internal/detect/detect.go`.
Config: `~/.config/openusage/settings.json`.

## Skills

This project has structured workflow skills stored in `docs/skills/`. When asked to perform any of these tasks, read and follow the full specification from the linked file.

{{SKILLS_TABLE}}

Each skill has a mandatory quiz or intake phase. Do NOT skip any phase. Always read the full skill file first.
