---
title: Makora
description: Track Makora credits balance, monthly spend, and per-model usage in OpenUsage.
sidebar_label: Makora
keywords: [makora usage tracker, makora billing, makora cost tracking, makora credits, track makora spend locally]
---

# Makora

Full billing and usage visibility for [Makora](https://app.makora.com) inference. Shows the live credits balance, the monthly spend limit, and per-model token/cost breakdowns with a daily spend series.

## At a glance

- **Provider ID** — `makora`
- **Detection** — `MAKORA_SESSION_TOKEN`, `~/.config/makora-usage/state.json`, `MAKORA_EMAIL` + `MAKORA_PASSWORD`, or the `makora-usage` binary on `$PATH`
- **Auth** — Dashboard session JWT (see below)
- **Type** — API platform (full billing data)
- **Tracks**:
  - Live credits balance
  - Monthly spend limit (reference for the balance gauge)
  - Month-to-date spend (last 30 days, computed from usage + rate card)
  - Per-model tokens / cached tokens / requests / cost
  - Daily cost + token series (analytics + daily views)

## Setup

### Auto-detection

OpenUsage detects a Makora account when any credential signal is present:

| Signal | What it provides |
|---|---|
| `MAKORA_SESSION_TOKEN` env var | A dashboard session JWT (pasted manually) |
| `~/.config/makora-usage/state.json` | Shared token cache (read-only) — reused from the makora-usage helper |
| `MAKORA_EMAIL` + `MAKORA_PASSWORD` env vars | Password login path |
| `makora-usage` binary on `$PATH` | Presence signal (account registered) |

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "makora",
      "provider": "makora"
    }
  ]
}
```

## Auth model — the important gotcha

Makora's **main API key** (`MAKORA_API_KEY`) is Inference+Optimize scoped and returns `HTTP 403` on all credits/usage endpoints. The billing endpoints require a **dashboard session JWT** (24&nbsp;h, HS256, `aud: "a2labs:auth"`).

The provider resolves the JWT in precedence order:

1. `MAKORA_SESSION_TOKEN` env var — a manually pasted JWT.
2. `~/.config/makora-usage/state.json` token cache — read-only; the shared sibling cache written by the makora-usage helper.
3. `MAKORA_EMAIL` + `MAKORA_PASSWORD` env vars — password login to `POST https://be.prod.makora.com/api/v1/login/access-token`.

On an `HTTP 401` during a fetch, the provider re-authenticates **once** (preferring the refresh token, falling back to password login) and retries. The token file is read-only unless this flow itself performs the login, in which case it may rewrite `state.json` with `0600` permissions.

## Data sources & how each metric is computed

Each poll makes up to four calls. Data endpoints sit under `https://app.makora.com` with `Authorization: Bearer <jwt>`.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /api/v1/credits/status` | Live balance, currency, payment method |
| 2 | `GET /api/v1/credits/user` | Monthly spend limit + auto-recharge config |
| 3 | `GET /api/v1/credits/default-rates` | **Public** per-1M-token price card (no auth) |
| 4 | `GET /api/v1/usage/my-usage/between/{start}/{end}?paygo=true` | Per-model daily usage for the last 30 days |

### `credit_balance`

- Source: `balance` field of `GET /api/v1/credits/status`; `currency` is `"Credits"`.
- Transform: stored as `Remaining`. When the monthly spend limit is present it is wired in as the gauge `Limit`, so the tile shows remaining credits against the monthly budget.

### `spend_limit` / `monthly_spend`

- `spend_limit` — the monthly cap from `GET /api/v1/credits/user` (`monthly_prepaid_notification.amount`, e.g. 400).
- `monthly_spend` — total cost across the last 30 days of usage, computed with the rate card. Used = spent, so the gauge renders spend vs. limit.

### Per-model cost — cached tokens are a SUBSET of input tokens

The rate card is public and used for cost computation instead of hardcoding:

```text
cost = (input − cached) × prompt + completion × completion + cached × cache_read
```

where each rate is USD per 1M tokens. **`cached_tokens` is a subset of `input_tokens`** — non-cached input is `input − cached`. The provider never adds `cached` on top of `input` (that would double-count). Verified: token-cost with the rate card reconciles to the Makora billing ledger within ~4%.

### Per-model table & daily series

- `ModelUsage` rows: input / output / cached / total tokens, requests, and cost per model.
- `DailySeries["cost"]` and `DailySeries["tokens"]`: per-day points powering the analytics and daily views.
- Today aggregates (`today_input_tokens`, `today_output_tokens`, `today_cached_tokens`, `today_tokens`, `today_requests`) derive from the most recent day in the window.

### Auth status

- Missing credential → snapshot `auth` with a clear setup message.
- `HTTP 401`/`403` that persists after the one-shot re-auth → `auth`.
- `balance ≤ 0` → `limited` ("balance exhausted"), otherwise `ok`.

## What's NOT tracked

- **Exact ledger spend.** Month-to-date spend is computed from usage × rate card, which reconciles within ~4% of the billing ledger but may not match the ledger to the cent.
- **Notification preferences.** The spend-warning threshold (`spending_warning_notification.amount`) is not surfaced as a meter — only the monthly cap is used as the gauge reference.

## API endpoints used

- `GET https://app.makora.com/api/v1/credits/status`
- `GET https://app.makora.com/api/v1/credits/user`
- `GET https://app.makora.com/api/v1/credits/default-rates`
- `GET https://app.makora.com/api/v1/usage/my-usage/between/{start}/{end}?paygo=true`
- `POST https://be.prod.makora.com/api/v1/login/access-token?device_name=openusage`
- `POST https://be.prod.makora.com/api/v1/login/refresh` (refresh token)

## Caveats

:::warning
Set `MAKORA_SESSION_TOKEN` (or `MAKORA_EMAIL` + `MAKORA_PASSWORD`), **not** `MAKORA_API_KEY`. The API key returns `HTTP 403` on the billing endpoints.
:::

- The session JWT expires after ~24&nbsp;h; the provider refreshes automatically when a refresh token or login credentials are available.
- Balance is in `Credits`; per-model cost is reported in USD (from the rate card).

## Troubleshooting

- **`Authentication failed`** — no usable credential path; set `MAKORA_SESSION_TOKEN`, add email+password, or make sure `~/.config/makora-usage/state.json` exists with a valid token.
- **Wrong error on data endpoints** — confirm you're not using `MAKORA_API_KEY` for the billing endpoints.
