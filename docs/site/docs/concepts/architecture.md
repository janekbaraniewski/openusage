---
title: Architecture
description: How OpenUsage discovers tools, polls providers, and renders snapshots in the TUI in both direct and daemon modes.
---

OpenUsage is a single Go binary with two runtime modes. Both modes funnel through the same TUI and the same provider implementations; what differs is how data is collected and where it lives between runs.

## Mental model

At the highest level there are four moving parts:

1. **Detector** — scans your machine for installed AI tools and known API key environment variables.
2. **Providers** — one per AI service, each knows how to fetch a snapshot of usage for an account.
3. **Snapshots** — a normalized data structure (`UsageSnapshot`) that captures spend, tokens, models, rate limits, and status for one account at one point in time.
4. **TUI** — a Bubble Tea app that renders snapshots into tiles, gauges, and detail views.

The two runtime modes plug those pieces together differently.

## Direct mode (default)

When you run `openusage` with no daemon installed, the TUI itself owns the polling loop. Data only exists for as long as the process runs.

```
┌─────────────────────────────────────────────────────────────┐
│ openusage process                                           │
│                                                             │
│  config.Load()  ─►  detect.AutoDetect()                     │
│        │                    │                               │
│        └─► AccountConfigs ──┘                               │
│                    │                                        │
│                    ▼                                        │
│              providers.AllProviders()                       │
│                    │                                        │
│                    ▼                                        │
│         ┌─────────────────────┐                             │
│         │ poll ticker (~30s)  │ ─► provider.Fetch() per acc │
│         └─────────┬───────────┘                             │
│                   ▼                                         │
│             UsageSnapshot[]                                 │
│                   │                                         │
│                   ▼                                         │
│      tea.Program.Send(SnapshotsMsg)                         │
│                   │                                         │
│                   ▼                                         │
│             TUI re-renders                                  │
└─────────────────────────────────────────────────────────────┘
```

Trade-offs:

- No background process, no socket, no SQLite file.
- Closing the dashboard ends data collection.
- Each TUI launch starts from zero history (you only see what providers themselves remember).

## Daemon mode

When you install the daemon (`openusage telemetry daemon install`), polling moves out of the TUI and into a long-running service. The TUI becomes a thin client over a Unix domain socket.

```
┌──────────────────────────┐         ┌─────────────────────────┐
│ openusage telemetry      │         │ openusage (TUI)         │
│   daemon (background)    │         │                         │
│                          │         │ ViewRuntime client      │
│  Pipeline                │   UDS   │      ▲                  │
│   ├─ Collectors ─────────┤◄────────┤      │ /v1/read-model   │
│   │   poll providers     │  HTTP   │      │                  │
│   ├─ Hooks (POST)        │         │      ▼                  │
│   │   from agents        │         │  SnapshotsMsg → render  │
│   └─ Spool (disk queue)  │         └─────────────────────────┘
│         │                │
│         ▼                │
│   telemetry.Store        │
│   (SQLite, WAL)          │
│         │                │
│         ▼                │
│   ReadModel (builds      │
│   UsageSnapshot per      │
│   provider on request)   │
└──────────────────────────┘
```

Trade-offs:

- Data survives across TUI sessions.
- Hooks from Claude Code, Codex, and OpenCode can ship fine-grained per-turn events that polling alone could not see.
- Adds one always-on process and a SQLite file (`~/.local/state/openusage/telemetry.db`).

For the full comparison see [direct vs daemon](direct-vs-daemon.md).

## Core types

Every provider implements the same interface:

```go
type UsageProvider interface {
    ID() string
    Describe() ProviderInfo
    Spec() ProviderSpec
    DashboardWidget() DashboardWidget
    DetailWidget() DetailWidget
    Fetch(ctx context.Context, acct AccountConfig) (UsageSnapshot, error)
}
```

- `Spec()` declares auth/setup metadata and widget layouts.
- `Fetch()` is the only side-effecting call: it talks to an API, reads files, or shells out to a CLI.
- `UsageSnapshot` is the only thing the TUI knows about — all rendering is driven from it plus the static widget definitions.

## How the pieces meet

| Layer | Responsibility | Code |
|---|---|---|
| Config | Load `settings.json`, merge with detection | `internal/config/` |
| Detection | Find installed tools and env-var-backed keys | `internal/detect/` |
| Providers | Implement `UsageProvider` per service | `internal/providers/<name>/` |
| Daemon | Run pipeline, expose UDS endpoints | `internal/daemon/` |
| Telemetry | Store/query events, build read models | `internal/telemetry/` |
| TUI | Render snapshots, handle keys | `internal/tui/` |

## Key invariants

- The TUI never talks to an AI provider directly — only to providers (in direct mode) or the daemon (in daemon mode).
- API keys are referenced by env-var name in config (`api_key_env`), never stored.
- `AccountConfig.Token` has `json:"-"` so runtime tokens never persist.
- The daemon and the TUI communicate over a Unix domain socket only — no TCP, no remote attach.

## Where to read next

- [Auto-detection](auto-detection.md) — what gets discovered on first run.
- [Providers](providers.md) — what a provider is and the categories.
- [Snapshots](snapshots.md) — the data model the TUI renders.
- [Telemetry](telemetry.md) — events, sources, and dedup.
