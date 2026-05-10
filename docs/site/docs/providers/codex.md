---
title: Codex CLI
description: Track OpenAI Codex CLI sessions, rate limits, and credit balance in OpenUsage.
sidebar_label: Codex
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
  - Credit balance
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

Codex has two data paths:

1. **Local files** — JSONL session transcripts and auth/config metadata under `~/.codex/`. Always available after a single Codex run.
2. **Live ChatGPT usage endpoint** — an authenticated POST to ChatGPT's backend, only attempted when `~/.codex/auth.json` contains a non-empty access token. Provides plan, credits, and rate-limit windows.

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

### How fresh is the data?

- Polling: every 30 s by default. JSONL files are re-parsed when their mtime/size changes; otherwise served from cache.
- Hook (when integration is installed): real-time per turn.

## API endpoints used

- Optional live usage endpoint:
  - `GET https://chatgpt.com/backend-api/wham/usage` (default), or
  - `GET <base>/api/codex/usage` for non-ChatGPT bases.
  - Headers: `Authorization: Bearer <auth.json access_token>` and `ChatGPT-Account-Id: <account_id>` when available.

## Files read

- `~/.codex/sessions/**/*.jsonl` — session transcripts
- `~/.codex/auth.json` — auth token (`tokens.access_token`, `tokens.account_id`)
- `~/.codex/config.toml` — CLI configuration (`chatgpt_base_url` if set)
- `~/.codex/version.json` — installed version

## Caveats

- Credit balance only appears when the live endpoint is reachable; offline sessions still show local activity.
- Rate-limit windows are reported by the API and may differ from documented limits during quota changes.
- The provider has hooks-style integration with the daemon: see [Daemon integrations](../daemon/integrations.md).

## Troubleshooting

- **Tile is empty** — run `codex` once to populate `~/.codex/sessions/`.
- **No credit balance** — `~/.codex/auth.json` is missing or expired. Re-authenticate with the Codex CLI.
- **Sessions missing** — confirm `sessions_dir` matches the path Codex writes to.

## Related

- [OpenAI](./openai.md) — direct API rate limits for the underlying models
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
