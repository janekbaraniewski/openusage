---
title: DeepSeek
description: Track DeepSeek balance breakdown and rate limits in OpenUsage.
sidebar_label: DeepSeek
---

# DeepSeek

Full balance visibility for DeepSeek. Splits the account balance into total, granted, and topped-up portions, and adds RPM/TPM rate limits.

## At a glance

- **Provider ID** — `deepseek`
- **Detection** — `DEEPSEEK_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Account availability
  - Balance breakdown: total, granted, topped-up
  - Currency (CNY by default)
  - RPM and TPM

## Setup

### Auto-detection

Set `DEEPSEEK_API_KEY`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "deepseek",
      "provider": "deepseek",
      "api_key_env": "DEEPSEEK_API_KEY",
      "base_url": "https://api.deepseek.com"
    }
  ]
}
```

## Data sources & how each metric is computed

Each poll (default every 30 seconds in daemon mode) makes two calls under `https://api.deepseek.com`. All requests use `Authorization: Bearer $DEEPSEEK_API_KEY`.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /user/balance` | Balance breakdown + currency |
| 2 | `GET /v1/models` | Rate-limit headers |

### `account_available` (status flag)

- Source: `is_available` field at the top of the `/user/balance` JSON.
- Transform: stored as `Raw["account_available"]`. When `false`, the snapshot is set to status `error` with message `DeepSeek account is not available`.

### `total_balance` / `granted_balance` / `topped_up_balance`

- Source: the **first** entry in the `balance_infos[]` array of `/user/balance`. Fields used:
  - `total_balance`
  - `granted_balance` (free credits)
  - `topped_up_balance` (paid balance)
  - `currency` (default `CNY` if absent)
- Transform: each string-encoded number is parsed with `strconv.ParseFloat` and stored as `Remaining` on the matching metric. The currency is propagated to each metric's `Unit`.

### `rpm` / `tpm` — rate limits

- Source: response headers on `GET /v1/models`
  - `x-ratelimit-limit-requests`, `x-ratelimit-remaining-requests`, `x-ratelimit-reset-requests`
  - `x-ratelimit-limit-tokens`, `x-ratelimit-remaining-tokens`, `x-ratelimit-reset-tokens`
- Transform: parsed verbatim.

### Auth status

- Source: HTTP status code. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok` (unless `account_available` is false, which forces `error`).

### What's NOT tracked

- **Spend / cost.** DeepSeek's API does not expose period-to-date spend. The granted-vs-topped-up split is the only signal of how credits are being consumed.
- **Grant expiry.** Granted credits typically have an expiry date but the API does not expose it.

### How fresh is the data?

- Polled every 30 s by default. The balance endpoint is updated by DeepSeek with a small ingestion delay (seconds to minutes).

## API endpoints used

- `GET /user/balance`
- `GET /v1/models`

## Caveats

:::warning
DeepSeek bills in **CNY** (Chinese Yuan) by default. The dashboard shows the currency as reported by the API; conversion is up to you.
:::

- Granted credits typically expire; the API does not expose expiry dates.
- Balance is updated near real-time but with a small ingestion delay.

## Troubleshooting

- **Account unavailable** — DeepSeek occasionally restricts new keys; check the console.
- **Wrong currency** — verify your account's region; the currency comes straight from the API.
