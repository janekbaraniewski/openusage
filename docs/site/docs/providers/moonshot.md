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

## What you'll see

- Dashboard tile shows the available balance and current tier.
- Detail view splits balance into available, voucher (free credits), and cash (paid).
- Quota gauges cover RPM, TPM, max concurrency, and total token quota.
- A peak-tracking row shows the high-water mark for each balance dimension since the daemon started.

## API endpoints used

- `/v1/users/me`
- `/v1/users/me/balance`

## Caveats

:::warning
The currency depends on the region. Global accounts (`api.moonshot.ai`) bill in USD; China accounts (`api.moonshot.cn`) bill in CNY.
:::

- High-water-mark gauges reset when the daemon restarts.
- Voucher credits are typically time-limited; the API does not expose expiry dates.

## Troubleshooting

- **Auth failed** — confirm the `base_url` matches the region your key was issued for.
- **Wrong currency** — switch `base_url` between `api.moonshot.ai` and `api.moonshot.cn`.
