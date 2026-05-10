---
title: Cursor IDE
description: Track Cursor IDE plan spend, billing cycle, composer sessions, and per-model usage in OpenUsage.
sidebar_label: Cursor
---

# Cursor IDE

Tracks plan spend and per-model usage from Cursor. Combines Cursor's billing API with the IDE's local SQLite databases for a complete picture of the current billing cycle.

## At a glance

- **Provider ID** — `cursor`
- **Detection** — Cursor application support directory on disk
- **Auth** — stored locally by the Cursor IDE; no API key needed
- **Type** — coding agent
- **Tracks**:
  - Billing cycle window
  - Plan spend: total, included, bonus, limit
  - Spend-limit usage gauge
  - Per-model aggregations: input/output tokens, cache write/read, cost in cents
  - Composer sessions
  - AI code score
  - Team members (if applicable)

## Setup

### Auto-detection

OpenUsage looks for Cursor's application support directory:

- macOS — `~/Library/Application Support/Cursor`
- Linux — `~/.config/Cursor`
- Windows — `%APPDATA%\Cursor`

If found, the provider registers automatically and reuses the credentials Cursor already stored.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "cursor",
      "provider": "cursor",
      "extra": {
        "tracking_db": "~/.cursor/ai-tracking/ai-code-tracking.db",
        "state_db": "~/Library/Application Support/Cursor/User/globalStorage/state.vscdb"
      }
    }
  ]
}
```

Override `tracking_db` and `state_db` only if you've moved Cursor's data dir.

## What you'll see

- Dashboard tile shows current cycle spend against the spend limit, with a gauge.
- Detail view lists per-model rows with token counts (input, output, cache write, cache read) and cost in cents.
- Composer sessions and AI code score appear as secondary metrics.
- Team accounts surface team-member count alongside individual usage.

## API endpoints used

All under `https://api2.cursor.sh`:

- `/billing/current-period-usage`
- `/billing/plan-info`
- `/billing/hard-limit`
- `/billing/aggregated-usage`
- `/stripe/profile`
- `/team/members`

## Files read

- Cursor state SQLite DB
- Cursor tracking SQLite DB

## Caveats

:::warning
This provider requires CGO because it reads SQLite directly. Pre-built binaries ship with CGO enabled; if you build from source, set `CGO_ENABLED=1`.
:::

- Composer cost is billable usage and counts against the plan limit.
- AI code scoring caches aggregate data; very recent activity may take a few minutes to appear.
- Team aggregation only kicks in when a team plan is detected on the account.

## Troubleshooting

- **Cursor not detected** — ensure the IDE has been launched at least once on this machine.
- **SQLite errors** — the build was likely produced without CGO. Use the official binary or rebuild with `CGO_ENABLED=1`.
- **Stale numbers** — Cursor's billing API caches aggregates; numbers refresh on the next poll cycle.
