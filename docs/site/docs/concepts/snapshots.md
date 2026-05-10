---
title: Snapshots
description: The UsageSnapshot data model, what metrics it carries, refresh cadence, and how time-window filtering works.
---

A `UsageSnapshot` is the unit of data the TUI renders. Each provider produces one snapshot per account per fetch. Everything you see on screen — tiles, gauges, detail tables, status badges — comes from a snapshot plus the provider's static widget definition.

## What a snapshot carries

A snapshot is a normalized container. Not every provider populates every field; what's missing simply isn't shown.

### Identity

- account ID and provider ID
- timestamp of the fetch
- status (`OK`, `WARN`, `LIMIT`, `AUTH`, `ERR`, `UNKNOWN`)

### Spend

- total spend in the provider's reported currency
- monthly / cycle spend
- spend limits (hard, soft, plan-included, plan-bonus)
- credit balance breakdown (cash, voucher, granted)

Currencies vary: most providers report USD, Mistral reports EUR, DeepSeek defaults to CNY. The detail view shows the provider's native currency without conversion.

### Tokens

- input / output / cache-read / cache-create / reasoning tokens
- per-model token counts
- tool-call counts (for agents that report them)

### Rate limits

Providers may expose any combination of:

- requests per minute (rpm)
- tokens per minute (tpm)
- requests per day (rpd)
- tokens per day (tpd)
- concurrency caps

For each, the snapshot can carry `limit`, `remaining`, and `reset` timestamps.

### Per-model breakdown

A list of per-model rows with input/output/cache tokens, request counts, and (where available) cost in the provider's currency.

### Provider-specific extras

A free-form key/value map for things that don't fit a standard field. Detail widgets can render these as their own sections (e.g. Claude Code billing blocks, Z.AI grants list).

## Refresh cadence

The poll interval is configurable and applies in both runtime modes.

- Default: **30 seconds** (`ui.refresh_interval_seconds` in `settings.json`, or `--interval` for the daemon).
- Direct mode: every account is refetched on every tick.
- Daemon mode: collectors poll on the configured interval; hooks deliver events between ticks.
- Manual refresh: press `r` in the TUI to force-fetch every account.

There is no streaming — every snapshot is a fresh full state, not a delta.

## Time-window filtering

The TUI exposes a window selector with `w`:

| Token | Meaning |
|---|---|
| `1d` | Today since local midnight |
| `3d` | Rolling 72 hours |
| `7d` | Rolling 7 days |
| `30d` | Rolling 30 days (default) |
| `all` | No filter |

What the window changes:

- Aggregations in the detail view (total spend, total tokens) are restricted to the window.
- Per-day bar charts in the Analytics screen scale to the window.
- Live "current" values (rate-limit gauges, balances) are not affected — those are always the latest snapshot.

The window only applies to data the runtime has actually seen. In direct mode, that means data accumulated since the TUI launched. In daemon mode, that's everything within `data.retention_days` (default 30) — see [telemetry](telemetry.md).

## Snapshot lifecycle

```
provider.Fetch()
   │
   ▼
UsageSnapshot
   │
   ├─ direct mode ──► tea.Program.Send(SnapshotsMsg) ─► render
   │
   └─ daemon mode ──► telemetry.Store
                              │
                              ▼
                         ReadModel
                              │
                              ▼
                       UsageSnapshot
                              │
                              ▼
                       UDS /v1/read-model ─► render
```

In daemon mode, the snapshot returned to the TUI is rebuilt from stored events on each request. That means historical data persists across TUI restarts and daemon restarts.

## When fields go missing

If a provider can't reach its source, the snapshot still renders, but with reduced fields and a non-OK status:

- `AUTH` — the configured env var or local credentials are missing or invalid.
- `ERR` — fetch failed (network, parse error, unexpected payload). The detail panel shows the error message.
- `UNKNOWN` — provider is registered but no data has been collected yet.

Tiles never disappear because of a transient failure; they just badge themselves and keep retrying on the next tick.
