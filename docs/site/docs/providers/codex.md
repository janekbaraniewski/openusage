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

OpenUsage registers the provider once Codex has written data under `~/.codex/`. Run the Codex CLI at least once to create it.

Detection needs `~/.codex/sessions/` or `~/.codex/auth.json`; the `codex` binary is optional. The telemetry daemon runs under launchd or systemd with a minimal `PATH`, so OpenUsage also looks for `codex` in common per-user install directories (`~/.local/bin`, `~/.npm-global/bin`, `~/.nvm/versions/node/*/bin`, `~/.volta/bin`, `~/.bun/bin`, pnpm and Linuxbrew prefixes). Without the binary, quota comes from the live usage endpoint instead of `codex app-server`.

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

Codex has three data paths, checked in this order:

1. **Codex CLI app-server** — `codex app-server --stdio` reads `account/rateLimits/read` before the session scan. It supplies current-account quota windows, plan and credit data, including individual monthly credit limits when available.
2. **Live ChatGPT usage endpoint** — an authenticated GET, attempted only when the app-server returns no usable quota window and `auth.json` contains an access token.
3. **Local files** — JSONL session transcripts and auth/config metadata under `~/.codex/`. The latest usable session supplies token activity and fallback quota windows when neither live source supplies a usable window.

A live quota window replaces session quota windows. If the app-server returns only a weekly window for the main pool, OpenUsage reports the missing 5h window instead of filling it from an older session. Other metered pools show only windows they actually report. Session quota fallback is labeled as session data and may be older than a live response.

The base URL for the live endpoint is, in order: `acct.BaseURL` → `extra.chatgpt_base_url` → the value parsed from `~/.codex/config.toml` (`chatgpt_base_url`) → `https://chatgpt.com/backend-api`. The path is `/wham/usage` for `chatgpt.com/backend-api` and `/api/codex/usage` otherwise.

### Latest session

- Source: the newest usable `~/.codex/sessions/**/*.jsonl`, ordered by the rollout filename timestamp (mtime is a fallback for other names). The provider skips recent stub files without `token_count` events and parses the trailing turn's `Info.TotalTokenUsage` for tokens, plus `model` and `client` from the same payload.
- Transform: tokens stored as `latest_session_tokens`, model/client stored under `Raw["latest_session_model"]` and `Raw["latest_session_client"]`.

### Daily / model / client breakdowns

- Source: the same JSONL files, scanned per poll (with mtime + size caching to skip unchanged files).
- Transform: each turn becomes a usage record. Records are aggregated by model, by client, and by day. Outputs:
  - `sessions_today` — distinct sessions with at least one turn whose timestamp falls in today (local time).
  - Per-model rows with input/output/cached token totals.
  - Per-client rows with the same totals plus session count.

#### How the model is resolved

The model credited to each turn is resolved in this order, first match wins:

1. `model` / `model_id` on the `token_count` event itself (per-turn override).
2. `model` / `model_id` on the `turn_context` line, if present.
3. `model` / `model_id` in the `session_meta` header.
4. `base_instructions.provenance.model` in the `session_meta` header.

Step 4 matters on Codex CLI 0.147.0 and later, which stopped writing an
explicit model to the session header and no longer emits `turn_context` lines
at all. Without it every turn falls through to the `unknown` bucket, which
also zeroes that bucket's cost.

:::note Turns that report only a token total
Some Codex clients emit `token_count` events with `total_tokens` populated but
`input_tokens` and `output_tokens` both zero. Those turns still contribute to
`model_<name>_total_tokens`, but no cost is derived for them — input and output
price differently, so a total alone cannot be converted to spend.
:::

### Rate-limit windows (`rate_limit_primary`, `rate_limit_secondary`)

- Source: `primary`, `secondary`, and `rateLimitsByLimitId` from the app-server, then the HTTP usage response if the RPC has no valid windows, then the selected local session. Each window reports a used percentage, duration, and optional reset time.
- Transform: `Used = used_percent`, `Remaining = 100 - Used`, `Limit = 100`. The main pool keeps `rate_limit_primary` and `rate_limit_secondary`; other pools use `rate_limit_<id>_<slot>`. Reset times populate `Resets[…]`. The compatibility aliases `plan_auto_percent_used`, `plan_api_percent_used`, and `plan_percent_used` remain available.

### Credit balance

- Source: `credits.balance` (or `credits.has_credits` boolean) from the app-server, HTTP fallback, or local session.
- Transform: stored as a metric `Remaining` in USD. `unlimited=true` is reflected as a special attribute.

### Individual credits and forecast

- Source: `individualLimit` from the Codex CLI app-server `account/rateLimits/read` response. The response provides the current-period `limit`, cumulative `used` credits (or a remaining percentage), and the next `resetsAt` timestamp.
- Transform: `codex_credit_limit` contains used/remaining/total credits, while `codex_credit_percent_used` drives the primary dashboard gauge.
- Forecast: when the next monthly reset is available, OpenUsage infers the preceding calendar-month boundary and calculates the average burn rate from cumulative current-period usage divided by elapsed time since that boundary. The dashboard shows the reset countdown and projected percentage at reset. Without a usable reset timestamp, it falls back to successive observed quota samples.
- Forecast source is recorded as `inferred_period_start` or `observed_usage` so the estimate is distinguishable from authoritative quota data.

### Plan, version, account email

- Source: `plan_type` from the app-server, HTTP fallback, or local session; email from HTTP or auth metadata; CLI version from `~/.codex/version.json`; account ID from `auth.json` (`tokens.account_id` or top-level `account_id`).
- Transform: each stored as a snapshot attribute.

### Patch stats

- Source: scanning JSONL turns for tool-call entries that look like file edits.
- Transform: aggregated counts of patches/files-changed.

### Auth status

- Source: HTTP status code when the fallback call is attempted.
- Transform: `401`/`403` is recorded as a diagnostic. Without usable local or app-server data, the snapshot reports authentication required; otherwise the local-data path stays available.

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
- Preferred local CLI quota endpoint: `codex app-server --stdio`, using the JSON-RPC handshake followed by `account/rateLimits/read`.

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

- **No quota windows** — authenticate with `codex login` and check that the installed CLI supports `codex app-server --stdio`. Local session windows remain available when live requests fail.
- **No credit usage or forecast** — `~/.codex/auth.json` is missing or expired, or the CLI app-server quota request failed. Re-authenticate with the Codex CLI and wait for the next daemon poll.
- **Codex listed by `openusage detect` but missing from the dashboard** (older versions): the daemon could not find the `codex` binary on its service `PATH`. Upgrade, or set `binary` on a manual `codex` account to the full path from `which codex`.
- **Sessions missing** — confirm `sessions_dir` matches the path Codex writes to.

## Related

- [OpenAI](./openai.md) — direct API rate limits for the underlying models
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
