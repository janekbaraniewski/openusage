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

## What you'll see

- Dashboard tile shows credit balance against limit and today's spend.
- Detail view lists per-model and per-endpoint rows with token counts, cost, latency, and cache hit rate.
- A 30-day analytics chart breaks down generations by model and provider.
- BYOK rows are flagged separately so you can see what's billed to OpenRouter vs to your own keys.

## API endpoints used

- `GET /api/v1/auth/key`
- `GET /api/v1/billing/credits/details`
- `GET /api/v1/keys` — only with a management key
- `GET /api/v1/analytics/generations`
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
