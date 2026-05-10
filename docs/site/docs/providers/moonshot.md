---
title: Moonshot
description: Track Moonshot organization, balance breakdown, quotas, and peak usage in OpenUsage.
sidebar_label: Moonshot
---

# Moonshot

Full account visibility for Moonshot. Surfaces org/project metadata, balance breakdown, request and token quotas, and high-water-mark peaks per balance dimension.

## At a glance

- **Provider ID** — `moonshot`
- **Detection** — `MOONSHOT_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Org, project, key suffix, state, tier
  - RPM, TPM, max concurrency, total token quota
  - Balance breakdown: available, voucher, cash
  - High-water-mark gauges per balance dimension

## Setup

### Auto-detection

Set `MOONSHOT_API_KEY`. OpenUsage routes to the correct regional endpoint based on `base_url`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "moonshot",
      "provider": "moonshot",
      "api_key_env": "MOONSHOT_API_KEY",
      "base_url": "https://api.moonshot.ai"
    }
  ]
}
```

## Regional endpoints

Moonshot operates two distinct regions with different billing currencies:

| Region | `base_url` | Currency |
|--------|------------|----------|
| Global | `https://api.moonshot.ai` | USD |
| China | `https://api.moonshot.cn` | CNY |

Pick the one matching your account; an API key from one region won't authenticate on the other.

## Data sources & how each metric is computed

Each poll (default every 30 seconds in daemon mode) makes two calls. The base URL determines the region: `api.moonshot.ai` (USD) or `api.moonshot.cn` (CNY). All requests use `Authorization: Bearer $MOONSHOT_API_KEY`.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /v1/users/me` | Org/project IDs, tier, RPM/TPM limits, concurrency cap, total token quota |
| 2 | `GET /v1/users/me/balance` | Available, voucher, and cash balance |

### Region & currency

- Source: `base_url`. The provider compares it against `.moonshot.cn` and sets `Attributes["currency"]` to `CNY`; otherwise `USD`. The choice is reflected on every balance metric.

### Org / project / key metadata

- Source: top-level `data` block on `/v1/users/me`:
  - `organization.id`, `project.id`, `access_key.id` (last 4 chars stored as `access_key_suffix`)
  - `user.user_state`, `user.user_group_id` (or `user_group_id`)
- Transform: each is stored as a snapshot attribute (`org_id`, `project_id`, `access_key_suffix`, `user_state`, `account_tier`).

### `rpm`, `tpm`, `concurrency_max`, `total_token_quota`

- Source: `data.organization.max_request_per_minute`, `max_token_per_minute`, `max_concurrency`, `max_token_quota` on `/v1/users/me`.
- Transform: each is stored as a metric `Limit`. These are caps, not live counters.

### `available_balance` / `cash_balance` / `voucher_balance` (with peak tracking)

- Source: the `data` block of `/v1/users/me/balance`:
  - `available_balance`
  - `cash_balance` (paid)
  - `voucher_balance` (free credits)
- Transform: Moonshot's API returns only the **currently remaining** value with no lifetime-deposit field. To render gauges, the provider stores a per-account high-water mark for each dimension on disk and uses it as `Limit`. A new top-up bumps the peak; spend-down then fills the gauge between `Limit` and `Remaining`. The implicit `Used = Limit - Remaining`. Currency from the region detection above.

### Status

- Source: HTTP status code first. Then derived from `available_balance`:
  - `available <= 0` → `limited` (`balance exhausted`)
  - `available < 1.0` → `near_limit` (`Low balance: …`)
  - otherwise → `ok` (`Balance: <amount> <currency>`)

### What's NOT tracked

- **Spend over time.** Moonshot's API returns only a snapshot of the remaining balance. Without a lifetime-deposit field there's no proper denominator beyond our own peak tracking.
- **Voucher expiry dates.** The API does not expose them.
- **Per-model usage.** Not exposed by either endpoint.

### How fresh is the data?

- Polled every 30 s by default. Peak tracking persists in the user state file and survives daemon restarts.

## API endpoints used

- `GET /v1/users/me`
- `GET /v1/users/me/balance`

## Caveats

:::warning
The currency depends on the region. Global accounts (`api.moonshot.ai`) bill in USD; China accounts (`api.moonshot.cn`) bill in CNY.
:::

- The peak-tracking high-water mark is per-account and persisted to disk. A balance that has only ever been observed full will show 100% remaining until a poll catches a lower value or a top-up bumps the peak.
- Voucher credits are typically time-limited; the API does not expose expiry dates.

## Troubleshooting

- **Auth failed** — confirm the `base_url` matches the region your key was issued for.
- **Wrong currency** — switch `base_url` between `api.moonshot.ai` and `api.moonshot.cn`.

### "no package" error or wrong currency on the tile

You are pointing at the wrong region. An `api.moonshot.ai` (USD) key will not authenticate against `api.moonshot.cn` (CNY) and vice versa. Update `base_url` to match the console where the key was issued.
