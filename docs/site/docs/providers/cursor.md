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

## Data sources & how each metric is computed

Cursor combines two distinct data paths. Most $ figures come from the API; per-commit and per-suggestion telemetry comes from the local SQLite DBs.

1. **Dashboard API** at `https://api2.cursor.sh`. Authenticated POST/GET calls to the `aiserver.v1.DashboardService` RPC and a few REST endpoints. The Bearer token is read from Cursor's local state DB — no API key is needed.
2. **Local SQLite databases (read-only).**
   - **Tracking DB** — `~/.cursor/ai-tracking/ai-code-tracking.db`. Contains `ai_code_hashes` (per-suggestion log) and `scored_commits` (one row per commit Cursor has scored).
   - **State DB** — Cursor's `state.vscdb` (a SQLite-backed key-value store). Path is platform-specific:
     - macOS: `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
     - Linux: `~/.config/Cursor/User/globalStorage/state.vscdb`
     - Windows: `%APPDATA%\Cursor\User\globalStorage\state.vscdb`

### Billing cycle window

- Source: `GetCurrentPeriodUsage` returns `billingCycleStart` / `billingCycleEnd` (RFC3339).
- Transform: stored as `Raw["billing_cycle_start"]`, `Raw["billing_cycle_end"]`, `Resets["billing_cycle_end"]`. A `billing_cycle_progress` metric is computed as `(now - start) / (end - start) × 100`.

### `plan_spend` — current cycle dollars

- Source: `GetCurrentPeriodUsage.planUsage`. Fields used: `totalSpend`, `includedSpend`, `bonusSpend`, `limit` — all in **cents**.
- Transform: each is divided by 100 to get USD. Mapped to:
  - `plan_spend.Used = totalSpend/100`
  - `plan_spend.Limit = limit/100`
  - `plan_included.Used = includedSpend/100`
  - `plan_bonus.Used = bonusSpend/100`
- The dollar number on the tile matches what Cursor's billing dashboard shows.

### `plan_percent_used` (auto / api / total)

- Source: `planUsage.totalPercentUsed`, `autoPercentUsed`, `apiPercentUsed`.
- Transform: stored as `Used` against `Limit = 100`; `Remaining = 100 - Used`. Status auto-promotes:
  - `>= 80%` → `near_limit`
  - `>= 100%` → `limited`

### `spend_limit` — pooled / individual

- Source: `GetCurrentPeriodUsage.spendLimitUsage`. Fields: `pooledLimit`, `pooledUsed`, `pooledRemaining`, `individualUsed`. All in cents.
- Transform: divided by 100. `spend_limit.Limit = pooledLimit`, `Used = pooledUsed`, `Remaining = pooledRemaining`. `individual_spend` is split out separately for team plans.

### Plan name and price

- Source: `GetPlanInfo` returns `planInfo.{planName, price, billingCycleEnd, includedAmountCents}`.
- Transform: stored as attributes. When `limit` is 0 on `GetCurrentPeriodUsage` but `includedAmountCents` is set, it is used as the `plan_spend` denominator (USD).

### Per-model aggregation

- Source: `GetAggregatedUsageEvents` returns an array `aggregations[]`. Each row has `modelIntent`, `inputTokens`, `outputTokens`, `cacheWriteTokens`, `cacheReadTokens`, `totalCents`, `tier`.
- Transform: each row becomes a detail row. Token strings are parsed as integers; `totalCents` is divided by 100 for the cost column. Aggregations are cached per (account, billing-cycle-start) and used as a fallback when the live call returns empty.

### `usage_based_billing`

- Source: `GetHardLimit.noUsageBasedAllowed`.
- Transform: stored as `Raw["usage_based_billing"]` = `enabled` / `disabled`.

### Membership type, team ID

- Source: `GET /auth/full_stripe_profile` (REST, not the DashboardService). Fields: `membershipType`, `isTeamMember`, `teamId`, `teamMembershipType`, `individualMembershipType`.
- Transform: stored as snapshot attributes.

### Spend-limit policy

- Source: `GetUsageLimitPolicyStatus.{canConfigureSpendLimit, limitType}`.
- Transform: stored as attributes.

### Team members (team plans only)

- Source: `GetTeamMembers` with body `{"teamId": "<id>"}`. Returned `teamMembers[]` carry `name`, `id`, `role`, `email`, `isRemoved`.
- Transform: active members counted; owner count tracked; member list rendered in the detail view.

### `scored_commits` and `ai_code_percentage` (local)

- Source: `scored_commits` table in the tracking DB. Each row has columns including `aiPercentage` (string).
- Transform: full table scan, then **cached** by row count — the next poll skips re-aggregation if the row count has not changed. Outputs:
  - `scored_commits` metric — total rows.
  - `ai_code_percentage` — average of parsed `aiPercentage` values (filtered to non-zero).
  - `composer_lines_added` / `composer_lines_removed` / `tab_lines_added` etc. summed across all commits.

### Per-suggestion log (local)

- Source: `ai_code_hashes` table. Each row records a single AI suggestion (composer, tab, CLI) with `source`, `model`, `createdAt`.
- Transform: rows are read incrementally (tracked by max RowID). Used to feed daily breakdowns and telemetry events.

### Composer sessions, bubble messages

- Source: state DB's `cursorDiskKV` table. Composer session blobs and bubble (chat) messages are decoded from the JSON values.
- Transform: incremental read by composer key; each new key → one composer session record. Used for session counts and per-message detail.

### Auth status

- Source: HTTP status code on the dashboard calls. `401`/`403` → `auth`. Failures on individual endpoints don't fail the snapshot — the rest of the data still renders, with errors stored under `Raw[<name>_error]`.

### What's NOT tracked

- **Spend in your local timezone.** Cursor reports per-cycle totals; the cycle boundaries come from the API in UTC.
- **Per-IDE breakdown.** `ai_code_hashes.source` only distinguishes composer/tab/cli, not the editor.

### How fresh is the data?

- Polled every 30 s by default.
- The dashboard API caches aggregates server-side, so the same poll may return identical numbers for a few cycles.
- Local SQLite reads are incremental — only new rows are scanned.

## API endpoints used

All under `https://api2.cursor.sh`:

- `POST /aiserver.v1.DashboardService/GetCurrentPeriodUsage`
- `POST /aiserver.v1.DashboardService/GetPlanInfo`
- `POST /aiserver.v1.DashboardService/GetHardLimit`
- `POST /aiserver.v1.DashboardService/GetAggregatedUsageEvents`
- `POST /aiserver.v1.DashboardService/GetUsageLimitPolicyStatus`
- `POST /aiserver.v1.DashboardService/GetTeamMembers` (team plans only)
- `GET /auth/full_stripe_profile`

## Files read

- Tracking DB — `~/.cursor/ai-tracking/ai-code-tracking.db` (`ai_code_hashes`, `scored_commits`)
- State DB — `state.vscdb` at the platform-specific path above (`cursorDiskKV`)

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

### Why is "AI code score" different from the dollar total?

The AI code score is the average `aiPercentage` across `scored_commits` — a lines-of-code statistic from local commits, not a billing figure. It has no cost component. The dollar total (`plan_spend`) is independent and comes from `GetCurrentPeriodUsage`.
