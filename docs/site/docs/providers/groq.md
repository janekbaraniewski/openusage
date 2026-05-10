---
title: Groq
description: Track Groq API rate limits (RPM, TPM, RPD, TPD) in OpenUsage.
sidebar_label: Groq
---

# Groq

Header-only rate-limit probe for the Groq API. Surfaces all four Groq rate-limit dimensions: RPM, TPM, RPD, and TPD.

## At a glance

- **Provider ID** — `groq`
- **Detection** — `GROQ_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (header-only rate limits)
- **Tracks**:
  - Requests per minute (RPM)
  - Tokens per minute (TPM)
  - Requests per day (RPD)
  - Tokens per day (TPD)
  - Auth status

## Setup

### Auto-detection

Set `GROQ_API_KEY`. OpenUsage registers the provider on next start.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "groq",
      "provider": "groq",
      "api_key_env": "GROQ_API_KEY",
      "base_url": "https://api.groq.com/openai/v1"
    }
  ]
}
```

## What you'll see

- Dashboard tile shows auth status and the most-constrained gauge across the four limit dimensions.
- Detail view splits each dimension (RPM, TPM, RPD, TPD) into limit, remaining, and reset time.

## API endpoints used

- `GET /v1/models` — header-only probe

## Caveats

- Groq's API does not expose spend or balance data to API keys.
- Per-day limits roll over on UTC day boundaries.

## Troubleshooting

- **Auth failed** — verify `GROQ_API_KEY` is set.
- **Per-day gauges full** — Groq enforces RPD/TPD on free tiers; upgrade or wait for the daily reset.
