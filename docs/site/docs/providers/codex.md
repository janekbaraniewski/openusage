---
title: Codex CLI
description: Track OpenAI Codex CLI sessions, rate limits, and credit balance in OpenUsage.
sidebar_label: Codex
keywords: [codex cli usage tracker, codex cli quota tracking, codex cli cost tracking, codex cli token usage, track codex cli spend locally]
---

# Codex CLI

Local-file provider for the OpenAI Codex CLI. Reads session logs, auth state, and config to show today's activity, plan info, and rate-limit windows.

## At a glance

- **Provider ID** — `codex`
- **Detection** — `~/.codex` directory on disk
- **Auth** — token stored in `~/.codex/auth.json` by the Codex CLI; no env var needed
- **Type** — coding agent
- **Tracks**:
  - Latest session: tokens, model, client
  - Daily session counts
  - Model and client breakdowns
  - Rate-limit windows (primary and secondary)
  - Individual credit usage versus the current monthly limit
  - Credit burn rate and projected runout time
  - Plan and version
  - Patch stats

## Setup

### Auto-detection

OpenUsage registers the provider as soon as `~/.codex/` exists. Run the Codex CLI at least once to create it.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "codex",
      "provider": "codex",
      "extra": {
        "config_dir": "~/.codex",
        "sessions_dir": "~/.codex/sessions"
      }
    }
  ]
}
```

Override `config_dir` and `sessions_dir` only if the CLI uses non-default paths.

## Data sources & how each metric is computed

Codex has three data paths:

1. **Local files** — JSONL session transcripts and auth/config metadata under `~/.codex/`. Always available after a single Codex run.
2. **Live ChatGPT usage endpoint** — an authenticated POST to ChatGPT's backend, only attempted when `~/.codex/auth.json` contains a non-empty access token. Provides plan, credits, and rate-limit windows.
3. **Codex CLI app-server** — an authenticated local `codex app-server` JSON-RPC request to `account/rateLimits/read`. Provides the authoritative individual monthly credit limit and next reset when the live HTTP payload omits it.

The base URL for the live endpoint is, in order: `acct.BaseURL` → `extra.chatgpt_base_url` → the value parsed from `~/.codex/config.toml` (`chatgpt_base_url`) → `https://chatgpt.com/backend-api`. The path is `/wham/usage` for `chatgpt.com/backend-api` and `/api/codex/usage` otherwise.

### Latest session

- Source: the most recently modified `~/.codex/sessions/**/*.jsonl`. The provider parses the trailing turn's `Info.TotalTokenUsage` for tokens, plus `model` and `client` from the same payload.
- Transform: tokens stored as `latest_session_tokens`, model/client stored under `Raw["latest_session_model"]` and `Raw["latest_session_client"]`.

### Daily / model / client breakdowns

- Source: the same JSONL files, scanned per poll (with mtime + size caching to skip unchanged files).
- Transform: each turn becomes a usage record. Records are aggregated by model, by client, and by day. Outputs:
  - `sessions_today` — distinct sessions with at least one turn whose timestamp falls in today (local time).
  - Per-model rows with input/output/cached token totals.
  - Per-client rows with the same totals plus session count.

### Rate-limit windows (`rate_limit_primary`, `rate_limit_secondary`)

- Source: `rate_limit.primary` and `rate_limit.secondary` from the live usage endpoint. Each carries `used_percent`, `window_minutes`, `resets_at` (Unix seconds).
- Transform: `Used = used_percent`, `Limit = 100`. `Resets[…]` is set from `resets_at`. `Window` is `<minutes>m`. Each window is also exposed via a direct alias for the dashboard widget: `plan_auto_percent_used` aliases `rate_limit_primary`, `plan_api_percent_used` aliases `rate_limit_secondary`. A separate `plan_percent_used` metric reflects the greater of the two.

### Credit balance

- Source: `credits.balance` (or `credits.has_credits` boolean) from the same live response.
- Transform: stored as a metric `Remaining` in USD. `unlimited=true` is reflected as a special attribute.

### Individual credits and forecast

- Source: `individualLimit` from the Codex CLI app-server `account/rateLimits/read` response. The response provides the current-period `limit`, cumulative `used` credits (or a remaining percentage), and the next `resetsAt` timestamp.
- Transform: `codex_credit_limit` contains used/remaining/total credits, while `codex_credit_percent_used` drives the primary dashboard gauge.
- Forecast: when the next monthly reset is available, OpenUsage infers the preceding calendar-month boundary and calculates the average burn rate from cumulative current-period usage divided by elapsed time since that boundary. The dashboard shows the reset countdown and projected percentage at reset. Without a usable reset timestamp, it falls back to successive observed quota samples.
- Forecast source is recorded as `inferred_period_start` or `observed_usage` so the estimate is distinguishable from authoritative quota data.

### Plan, version, account email

- Source: `plan_type`, `email` from live response; CLI version from `~/.codex/version.json`; account ID from `auth.json` (`tokens.account_id` or top-level `account_id`).
- Transform: each stored as a snapshot attribute.

### Patch stats

- Source: scanning JSONL turns for tool-call entries that look like file edits.
- Transform: aggregated counts of patches/files-changed.

### Auth status

- Source: combination of HTTP status code on the live call and the presence of `auth.json`.
- Transform: `401`/`403` from the live endpoint sets `errLiveUsageAuth`; the provider then keeps the local-data-only path intact and surfaces the error as a diagnostic.

### What's NOT tracked

- **Per-token spend in dollars from local sessions.** Codex sessions don't carry pricing — only token counts. The credit balance is the only $ figure, and it comes from the live endpoint.
- **Hook-driven real-time events without the integration.** Install the `codex` integration (see [Daemon integrations](../daemon/integrations.md)) for per-turn events.

:::note Cost values hidden by default on Plus / Pro / Team / Enterprise
On a ChatGPT subscription plan (Plus, Pro, Team, Enterprise) the dollar number is misleading — usage is governed by rate-limit windows, not by per-call pricing. OpenUsage hides cost columns by default whenever the live `plan_type` reports a subscription tier; rate-limit windows, sessions, and tokens stay visible. Override with [`dashboard.hide_costs`](../reference/configuration.md#dashboardhide_costs) or the <kbd>c</kbd> keystroke.
:::

### How fresh is the data?

- Polling: every 30 s by default. JSONL files are re-parsed when their mtime/size changes; otherwise served from cache.
- Hook (when integration is installed): real-time per turn.

## API endpoints used

- Optional live usage endpoint:
  - `GET https://chatgpt.com/backend-api/wham/usage` (default), or
  - `GET <base>/api/codex/usage` for non-ChatGPT bases.
  - Headers: `Authorization: Bearer <auth.json access_token>` and `ChatGPT-Account-Id: <account_id>` when available.
- Optional local CLI quota endpoint: `codex -s read-only -a untrusted app-server`, using the standard JSON-RPC handshake followed by `account/rateLimits/read`.

## Files read

- `~/.codex/sessions/**/*.jsonl` — session transcripts
- `~/.codex/auth.json` — auth token (`tokens.access_token`, `tokens.account_id`)
- `~/.codex/config.toml` — CLI configuration (`chatgpt_base_url` if set)
- `~/.codex/version.json` — installed version

## Caveats

- Individual credit usage and the forecast require authenticated Codex quota data from the live endpoint or CLI app-server; offline sessions still show local activity.
- Rate-limit windows are reported by the API and may differ from documented limits during quota changes.
- The monthly period start is inferred from the next reset because Codex reports the reset boundary but not an explicit start timestamp.
- The provider has hooks-style integration with the daemon: see [Daemon integrations](../daemon/integrations.md).

## Troubleshooting

- **Tile is empty** — run `codex` once to populate `~/.codex/sessions/`.
- **No credit usage or forecast** — `~/.codex/auth.json` is missing or expired, or the CLI app-server quota request failed. Re-authenticate with the Codex CLI and wait for the next daemon poll.
- **Sessions missing** — confirm `sessions_dir` matches the path Codex writes to.

## Related

- [OpenAI](./openai.md) — direct API rate limits for the underlying models
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
