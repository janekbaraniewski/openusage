---
title: Architecture
description: How OpenUsage discovers tools, polls providers via the daemon, and renders snapshots in the TUI.
---

OpenUsage is a single Go binary with one runtime: a background daemon that collects data, persists it to SQLite, and serves a unified read model to a thin TUI client. The TUI never talks to provider APIs directly — it always reads from the daemon.

## Mental model

At the highest level there are five moving parts:

1. **Detector** — scans your machine for installed AI tools and known API key environment variables.
2. **Providers** — one per AI service, each knows how to fetch a snapshot of usage for an account.
3. **Daemon** — long-running service that drives the polling loop, accepts hook events from agent integrations, and persists everything to SQLite.
4. **Snapshots** — a normalized data structure (`UsageSnapshot`) that captures spend, tokens, models, rate limits, and status for one account at one point in time. The daemon's `ReadModel` rebuilds these from stored events on each TUI request.
5. **TUI** — a Bubble Tea app that connects to the daemon over a Unix domain socket and renders snapshots into tiles, gauges, and detail views.

## Dataflow

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

Three input sources feed the pipeline:

- **Collectors** — driven by the daemon's polling loop. They call each provider's `Fetch()` on the configured interval and emit snapshots and derived events.
- **Hooks** — agent integrations (Claude Code, Codex, OpenCode) POST per-turn events to the daemon over its Unix socket as they happen.
- **Spool** — when the daemon is briefly unreachable, hook clients drop events into a disk queue (`~/.local/state/openusage/telemetry-spool/`) that is drained on next startup.

Trade-offs:

- Data survives across TUI sessions and machine reboots, capped by `data.retention_days` (default 30).
- Per-turn detail from agents is far richer than polling alone could see.
- One always-on process and a SQLite file (`~/.local/state/openusage/telemetry.db`).

For more on event flow and dedup, see [telemetry](telemetry.md).

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
- `Fetch()` is the only side-effecting call: it talks to an API, reads files, or shells out to a CLI. The daemon drives it; the TUI never calls it.
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

- The TUI never talks to an AI provider directly — only to the daemon over its Unix socket.
- API keys are referenced by env-var name in config (`api_key_env`), never stored.
- `AccountConfig.Token` has `json:"-"` so runtime tokens never persist.
- The daemon and the TUI communicate over a Unix domain socket only — no TCP, no remote attach.

## Where to read next

- [Auto-detection](auto-detection.md) — what gets discovered on first run.
- [Providers](providers.md) — what a provider is and the categories.
- [Snapshots](snapshots.md) — the data model the TUI renders.
- [Telemetry](telemetry.md) — events, sources, and dedup.
- [Daemon overview](/daemon) — install, run, troubleshoot.
