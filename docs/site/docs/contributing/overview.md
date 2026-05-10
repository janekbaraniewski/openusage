---
title: Contributing
description: How OpenUsage is structured, the prerequisites, and the dev loop.
---

OpenUsage is a single-binary Go project. Contributions are welcome — bug fixes, new providers, theme files, docs, design feedback. This page is the entry point; deeper pages cover code style and the new-provider flow.

## Repository layout

```
openusage/
├── cmd/openusage/        # main package — cobra CLI
├── internal/
│   ├── core/             # UsageProvider interface, snapshot types
│   ├── config/           # settings.json load/save
│   ├── detect/           # auto-detection
│   ├── parsers/          # shared rate-limit header parsers
│   ├── providers/        # one package per provider, plus registry.go
│   ├── daemon/           # background server, UDS endpoints
│   ├── telemetry/        # SQLite store, pipeline, read model
│   └── tui/              # Bubble Tea screens, widgets, themes
├── configs/              # example settings, bundled themes
├── docs/                 # markdown reference + skills
├── docs/site/            # Docusaurus website (this site)
└── Makefile
```

The TUI never imports the daemon directly; it talks to it over the socket. The providers never talk to the TUI; they return a `UsageSnapshot`. These boundaries make most changes easy to scope.

## Prerequisites

- **Go 1.25+**.
- **CGO enabled** (`CGO_ENABLED=1`). The Cursor provider and the telemetry store both depend on `github.com/mattn/go-sqlite3`. This means you need a C toolchain — `xcode-select --install` on macOS, `build-essential` on Debian/Ubuntu.
- **Optional**: `golangci-lint` for linting. The Makefile skips lint gracefully if it's not installed.

## Dev loop

```bash
make build          # build to ./bin/openusage with version ldflags
make run            # go run cmd/openusage/main.go
make test           # all tests with -race and coverage
make test-verbose   # same, verbose
make lint           # golangci-lint if installed
make fmt            # gofmt + goimports
make vet            # go vet
make demo           # build and run with synthetic data, no API keys needed
make sync-tools     # regenerate AI tool integration templates from canonical
```

Run a single test:

```bash
go test ./internal/providers/openai/ -run TestFetch -v
```

Run all provider tests:

```bash
go test ./internal/providers/...
```

## Demo mode

`make demo` is the fastest way to look at the dashboard without configuring anything:

- Builds a `demo` binary that ships eight synthetic accounts (Claude Code, Cursor, Gemini CLI, Codex, Copilot, OpenRouter, Ollama, etc).
- Scenarios advance every 5 seconds.
- Flags: `-interval 10s`, `-loop`.

Use this for screenshots, theme testing, and iterating on widget layouts without touching real provider APIs.

## What the dev loop does not do

- Cross-compile easily. CGO + sqlite3 means a C toolchain for the target. For releases, GoReleaser handles this; for local builds, target your own machine.
- Run end-to-end against real providers in CI. CI runs unit tests with mock HTTP servers; integration testing against real keys is left to maintainers locally.

## How to contribute

1. **Open an issue first** for non-trivial changes. We'd rather agree on shape before you write code.
2. **Branch off `main`**. Conventional commit messages are appreciated but not strictly required.
3. **Run `make fmt vet test`** before pushing.
4. **Add tests** for new behavior. The test conventions are documented in [development](development.md).
5. **Open a PR** with a description that mentions which provider / area it touches.

For brand-new providers, follow the dedicated guide: [add-provider](add-provider.md).

## Useful links

- GitHub: [github.com/janekbaraniewski/openusage](https://github.com/janekbaraniewski/openusage)
- Issues: [github.com/janekbaraniewski/openusage/issues](https://github.com/janekbaraniewski/openusage/issues)
- Releases: [github.com/janekbaraniewski/openusage/releases](https://github.com/janekbaraniewski/openusage/releases)

## See also

- [Development conventions](development.md) — code style, error wrapping, test patterns.
- [Add a provider](add-provider.md) — the seven-phase provider flow.
