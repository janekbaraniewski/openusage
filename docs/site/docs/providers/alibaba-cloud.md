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
  - Per-model RPM and TPM with usage and limits

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

## What you'll see

- Dashboard tile shows monthly spend against the spend limit and current balance.
- Detail view lists the billing period window, daily spend, total tokens, and total requests.
- Per-model rows show RPM/TPM usage against limits, so you can see which models you're saturating.

## API endpoints used

- `/api/v1/quotas`

## Caveats

- Billing is reported in USD even though the underlying account may be CNY-funded; reconcile against your Alibaba Cloud invoice.
- Per-model quotas vary by region and account tier; the dashboard shows whatever the API returns.
- The billing period is the calendar month.

## Troubleshooting

- **Account unavailable** — verify the DashScope service is enabled for your Alibaba Cloud account.
- **Empty per-model rows** — your key may have no model permissions; check DashScope's console.
- **Spend over limit** — Alibaba enforces hard limits at the account level; raise the limit in the console.
