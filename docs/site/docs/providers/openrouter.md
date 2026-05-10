---
title: OpenRouter
description: Track OpenRouter credits, daily/weekly/monthly usage, generation analytics, and BYOK breakdown in OpenUsage.
sidebar_label: OpenRouter
---

# OpenRouter

Full billing visibility for OpenRouter. OpenUsage pulls key info, credits, daily/weekly/monthly spend, generation analytics, and per-model and per-endpoint metrics.

## At a glance

- **Provider ID** — `openrouter`
- **Detection** — `OPENROUTER_API_KEY` environment variable
- **Auth** — API key (with optional management key for additional endpoints)
- **Type** — API platform (full billing data)
- **Tracks**:
  - Key info: name, label, tier, key type
  - Credit balance and limit
  - Daily, weekly, and monthly usage
  - BYOK breakdown
  - Generation analytics: model, provider, tokens, cost, latency, caching
  - Per-model and per-endpoint metrics
  - Rate limits

## Setup

### Auto-detection

Set `OPENROUTER_API_KEY`. A management key (also stored in the same env var if you use one) unlocks the `/keys` endpoint.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "openrouter",
      "provider": "openrouter",
      "api_key_env": "OPENROUTER_API_KEY"
    }
  ]
}
```

## Data sources & how each metric is computed

Each poll (default every 30 seconds in daemon mode) issues several authenticated GET requests under `https://openrouter.ai/api/v1`. All requests use `Authorization: Bearer $OPENROUTER_API_KEY`. OpenRouter is one of the few providers where a single API key returns enough data to render a fully-populated dashboard.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /key` (with `/auth/key` fallback) | Key info, tier, label, management-key flag |
| 2 | `GET /credits` | Balance and limit |
| 3 | `GET /keys?include_disabled=true&offset=…` | List of keys (management-key only) |
| 4 | `GET /activity` (and fallbacks) | 30-day analytics rollup |
| 5 | `GET /generation?limit=…&offset=…` then `GET /generation?id=…` | Per-generation drill-down (≤20 lookups per poll) |

### Key info

- Source: `/key` JSON. Fields: `data.label`, `data.name`, `data.tier`, `data.is_provisioning_key`, `data.is_free_tier`.
- Transform: each is stored under `Raw[…]`. The provisioning-key flag enables call 3.

### `credit_balance` / `credit_limit`

- Source: `/credits` JSON. Fields: `data.total_credits`, `data.total_usage`.
- Transform: `Used = total_usage`, `Limit = total_credits`, `Remaining = Limit - Used`. Currency: USD.

### Daily / weekly / monthly usage

- Source: the analytics rollup. The provider walks four candidate endpoints in order until one returns 200:
  - `/activity`
  - `/activity?date=<yesterday-UTC>`
  - `/analytics/user-activity`
  - `/api/internal/v1/transaction-analytics?window=1mo`
- Transform: per-day rows are summed into `daily_spend`, `weekly_spend`, `monthly_spend`. Tokens are summed into matching `*_tokens` metrics. Cache hits feed `cache_hit_rate`.

### Per-model & per-provider analytics

- Source: rows of the same analytics response, plus enrichment from `/generation?id=…`.
- Transform: each row is bucketed by `model` and `provider`. Up to 20 generation IDs per poll are followed up with `/generation?id=…` to backfill provider metadata that the rollup endpoint omits. Higher-volume rows are prioritized for enrichment.

### BYOK breakdown

- Source: a `byok` flag on per-generation rows.
- Transform: rows with `byok=true` are summed into a separate "BYOK" track so you can reconcile native OpenRouter spend vs your own upstream keys.

### Generation latency, caching

- Source: `latency_ms`, `cache_discount`, etc. on `/generation` rows.
- Transform: averaged across the enriched-generation set; rendered in the detail view.

### Rate limits

- Source: response headers on whichever calls return them (OpenRouter is selective).
- Transform: standard `x-ratelimit-*` parsing into `rpm` / `tpm` metrics. May be missing on a fresh poll.

### Auth status

- Source: HTTP status code on `/key`. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`. The `/keys` 403 (regular key) is non-fatal — every other call still runs.

### What's NOT tracked

- **Generations older than the 30-day analytics window.** OpenRouter's analytics rollups only cover the trailing 30 days.
- **Per-key spend on a regular key.** `/keys` only works with a management/provisioning key. Regular keys still see balance and analytics for themselves.

### How fresh is the data?

- Polled every 30 s by default. Analytics rollups are themselves cached server-side; the `cached_at` timestamp is stored in `Raw["activity_cached_at"]`. Per-generation enrichment is capped at 20 lookups per poll to avoid hammering OpenRouter's per-key limits.

## API endpoints used

- `GET /api/v1/key` (or `/api/v1/auth/key`)
- `GET /api/v1/credits`
- `GET /api/v1/keys` — only with a management key
- `GET /api/v1/activity` (and `/analytics/user-activity` / `/api/internal/v1/transaction-analytics` fallbacks)
- `GET /api/v1/generation?id=…` — up to 20 lookups per cycle

## Caveats

- Analytics window is 30 days; older data is not fetched.
- BYOK generations may overlap with native OpenRouter spend; the breakdown calls them out so you can reconcile.
- Rate limits come from response headers only.
- Generation lookups are capped at 20 per poll to avoid hitting OpenRouter's per-key limits.

## Troubleshooting

- **No keys list** — your API key is a regular key, not a management key. The rest of the data still appears.
- **Analytics empty** — no generations yet in the 30-day window. Use the API and recheck.
- **Rate-limit headers missing** — OpenRouter only emits them on certain endpoints; the gauge populates after a successful request.
