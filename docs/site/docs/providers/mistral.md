---
title: Mistral AI
description: Track Mistral monthly budget, credit balance, spend, and tokens in OpenUsage.
sidebar_label: Mistral AI
---

# Mistral AI

Full billing visibility for Mistral AI. Surfaces the monthly budget, credit balance, monthly spend, token totals, and rate limits — all in EUR.

## At a glance

- **Provider ID** — `mistral`
- **Detection** — `MISTRAL_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Plan
  - Monthly budget (EUR)
  - Credit balance (EUR)
  - Monthly spend
  - Monthly tokens (input and output)
  - RPM and TPM

## Setup

### Auto-detection

Set `MISTRAL_API_KEY`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "mistral",
      "provider": "mistral",
      "api_key_env": "MISTRAL_API_KEY"
    }
  ]
}
```

## What you'll see

- Dashboard tile shows monthly spend against budget in EUR.
- Detail view lists credit balance, plan name, and input/output token totals for the calendar month.
- Rate-limit gauges (RPM, TPM) come from header probes.

## API endpoints used

- `/v1/billing/subscription`
- `/v1/billing/usage`
- `/v1/models`

## Caveats

:::warning
Mistral bills in **EUR**. Mixing it with USD-billed providers in a single dashboard requires you to convert manually.
:::

- The billing period is the calendar month; numbers reset at midnight UTC on the 1st.
- Rate-limit headers come from `/v1/models`.

## Troubleshooting

- **No spend data** — verify the API key has billing scope; check Mistral's console.
- **Currency confusion** — Mistral always reports EUR; OpenUsage displays whatever the API returns.
