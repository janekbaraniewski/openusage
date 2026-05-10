---
title: Time windows
description: How OpenUsage filters aggregations by time, the difference between calendar 1d and rolling windows, and how retention bounds what you can query.
---

The time-window selector controls how much history aggregations cover. It applies to spend totals, token totals, and per-day charts; it does not affect "current state" values like rate-limit gauges or current balances.

## The five windows

| Token | Span | Boundary |
|---|---|---|
| `1d` | Since today's local midnight | Calendar |
| `3d` | Last 72 hours | Rolling |
| `7d` | Last 7 days | Rolling |
| `30d` | Last 30 days | Rolling (default) |
| `all` | Everything in the store | — |

`1d` is the only calendar-aligned window. The others are rolling: a `3d` window at 09:30 today goes back to 09:30 three days ago.

## Cycling windows

Press `w` in the dashboard to cycle forward. The selection persists to `settings.json` so the next launch starts where you left off.

In the Settings modal (`,`), the Telemetry tab also exposes `w` for changing the active window without leaving the tab.

## What changes when you cycle

Affected:

- Total spend and token figures in the detail panel.
- Per-day bar charts in the Analytics screen.
- Window-scoped status badges (e.g. "spend this period").

Not affected:

- Current rate-limit remaining/limit numbers — always the latest snapshot.
- Current balance / credit values — always the latest snapshot.
- Provider auth status.

This means a `1d` window can still show a `LIMIT` badge even if the limit only flipped seconds ago — limits are real-time, totals are scoped.

## Interaction with retention

The window can never reach further back than the data the runtime has actually seen.

- **Direct mode.** History only goes back to when the TUI launched. If you start the dashboard at 14:00 and pick `7d`, you get four hours of data, not seven days.
- **Daemon mode.** History goes back to the oldest event in the SQLite store, capped by `data.retention_days` (default 30).

Set `30d` against a 7-day-old daemon install and you'll only see seven days of data. Querying further back than retention is silently truncated; OpenUsage does not warn.

If you need longer-term data, raise `data.retention_days` in `settings.json` **before** the data ages out:

```json
{
  "data": { "retention_days": 90 }
}
```

Lowering it later prunes older events at the next pass.

## Calendar 1d vs rolling 3d

A common gotcha:

- At 23:59 local, `1d` shows almost a full day's worth of activity.
- One minute later at 00:00, `1d` resets to zero.
- `3d` does not reset on midnight; it just slides the 72-hour window forward.

Pick `1d` when you care about "did I cross my daily limit"; pick `3d` or `7d` when you care about a smooth trend.

## Where the window lives

The active window is part of `settings.json` under the UI section. Editing it manually works but is rarely necessary — the `w` key is the canonical entry point.

## Window scoping in the daemon

Internally the daemon's `ReadModel` accepts a window when the TUI requests `/v1/read-model`. The same `UsageSnapshot` shape comes back, with all aggregate fields recomputed for the chosen window. Switching windows therefore costs one round-trip, not a re-poll.
