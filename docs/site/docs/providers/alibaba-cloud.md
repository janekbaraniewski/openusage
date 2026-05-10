---
title: Alibaba Cloud Model Studios
description: Track Alibaba Cloud DashScope billing period, balance, spend, and per-model quotas in OpenUsage.
sidebar_label: Alibaba Cloud
---

# Alibaba Cloud Model Studios

Full billing visibility for Alibaba Cloud's Model Studios (DashScope). Surfaces the billing period, balance, daily/monthly spend, request and token usage, and per-model quotas.

## At a glance

- **Provider ID** — `alibaba_cloud`
- **Detection** — `ALIBABA_CLOUD_API_KEY` (DashScope key)
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Account availability
  - Billing period dates
  - Balance, credit, spend limit (USD)
  - Daily and monthly spend
  - Tokens used
  - Requests used
  - RPM and TPM
  - Per-model usage with `used / limit` gauges

## Setup

### Auto-detection

Set `ALIBABA_CLOUD_API_KEY` to your DashScope API key.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "alibaba_cloud",
      "provider": "alibaba_cloud",
      "api_key_env": "ALIBABA_CLOUD_API_KEY",
      "base_url": "https://dashscope.aliyuncs.com/api/v1"
    }
  ]
}
```

## Data sources & how each metric is computed

OpenUsage sends one `GET https://dashscope.aliyuncs.com/api/v1/quotas` per poll cycle (default every 30 seconds in daemon mode). All other metrics are derived from the single response. Auth: `Authorization: Bearer $ALIBABA_CLOUD_API_KEY`.

The response shape is `{ "code": "Success", "data": { … } }`. A non-`Success` `code` is treated as an error.

### `rpm` / `tpm` — account-wide rate limits

- Source: `data.rate_limit.rpm` and `data.rate_limit.tpm`.
- Transform: each integer is stored as a metric `Limit`. These are caps; live counters are not exposed at the account level.

### `credit_balance` — available credit

- Source: `data.credits`.
- Transform: stored as `Limit` of `credit_balance` (USD).

### `available_balance`

- Source: `data.available`.
- Transform: stored as `Limit` of `available_balance` (USD).

### `spend_limit` — hard cap

- Source: `data.spend_limit`.
- Transform: stored as `Limit` of `spend_limit` (USD).

### `daily_spend` / `monthly_spend`

- Source: `data.daily_spend` and `data.monthly_spend`.
- Transform: stored as `Used`. Window is `1d` and `30d` respectively.

### `tokens_used` / `requests_used`

- Source: `data.tokens_used`, `data.requests_used`.
- Transform: copied verbatim into `Used` (units `tokens`, `requests`).

### Billing period

- Source: `data.billing_period.start` and `data.billing_period.end`.
- Transform: stored as `Attributes["billing_cycle_start"]` and `Attributes["billing_cycle_end"]`.

### Per-model rows

- Source: `data.models[]` array. Each row carries a model name with `used` and `limit` values.
- Transform: each model produces two metrics — `model_<name>_usage_pct` (percentage) and `model_<name>_used` (raw `used / limit` gauge in `units`).

### Auth status

- Source: HTTP status code first. `401`/`403` → `auth` (`Invalid or expired API key`); `429` → `limited`; non-200 → `error`. After that, a non-`Success` `code` in the body promotes the snapshot to `error`.

### What's NOT tracked

- **Day-by-day breakdown.** The endpoint returns totals; no time series is produced.
- **Per-model spend.** The per-model rows expose rate-limit usage but not dollar cost.

### How fresh is the data?

- Polled every 30 s by default. DashScope's `/quotas` is a near-real-time aggregate.

## API endpoints used

- `GET /api/v1/quotas`

## Caveats

- Billing is reported in USD even though the underlying account may be CNY-funded; reconcile against your Alibaba Cloud invoice.
- Per-model quotas vary by region and account tier; the dashboard shows whatever the API returns.
- The billing period is the calendar month.

## Troubleshooting

- **Account unavailable** — verify the DashScope service is enabled for your Alibaba Cloud account.
- **Empty per-model rows** — your key may have no model permissions; check DashScope's console.
- **Spend over limit** — Alibaba enforces hard limits at the account level; raise the limit in the console.
