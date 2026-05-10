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

## What you'll see

- Dashboard tile shows total balance and account availability.
- Detail view splits balance into granted (free credits) and topped-up (paid) portions.
- RPM and TPM gauges come from header probes.

## API endpoints used

- `/user/balance`
- `/v1/models`

## Caveats

:::warning
DeepSeek bills in **CNY** (Chinese Yuan) by default. The dashboard shows the currency as reported by the API; conversion is up to you.
:::

- Granted credits typically expire; the API does not expose expiry dates.
- Balance is updated near real-time but with a small ingestion delay.

## Troubleshooting

- **Account unavailable** — DeepSeek occasionally restricts new keys; check the console.
- **Wrong currency** — verify your account's region; the currency comes straight from the API.
