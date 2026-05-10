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

## Data sources & how each metric is computed

OpenUsage sends one `GET https://api.groq.com/openai/v1/models` per poll cycle (default every 30 seconds in daemon mode). The response body (the model catalog) is discarded; the provider only consumes the rate-limit headers Groq attaches.

Request headers:

- `Authorization: Bearer $GROQ_API_KEY`

### `rpm` — requests per minute

- Source: response headers
  - `x-ratelimit-limit-requests`
  - `x-ratelimit-remaining-requests`
  - `x-ratelimit-reset-requests`

### `tpm` — tokens per minute

- Source: response headers
  - `x-ratelimit-limit-tokens`
  - `x-ratelimit-remaining-tokens`
  - `x-ratelimit-reset-tokens`

### `rpd` — requests per day

- Source: response headers
  - `x-ratelimit-limit-requests-day`
  - `x-ratelimit-remaining-requests-day`
  - `x-ratelimit-reset-requests-day`
- Window: 1 day. Resets at the UTC day boundary.

### `tpd` — tokens per day

- Source: response headers
  - `x-ratelimit-limit-tokens-day`
  - `x-ratelimit-remaining-tokens-day`
  - `x-ratelimit-reset-tokens-day`

### Status message

- After a successful poll the tile prints `Remaining: <X>/<Y> RPM, <X>/<Y> RPD`, derived from the parsed metrics. Not a separate field.

### Auth status

- Source: HTTP status code. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`.

### What's NOT tracked

- **Spend / balance.** Groq's API does not expose dollar figures or balance to API keys.
- **Per-model breakdown.** The probe is a single catalog request; the headers reflect per-key aggregate limits, not per-model.

### How fresh is the data?

- Polled every 30 s by default. One request per poll, no cache.

## API endpoints used

- `GET /v1/models` — header-only probe.

## Caveats

- Groq's API does not expose spend or balance data to API keys.
- Per-day limits roll over on UTC day boundaries.

## Troubleshooting

- **Auth failed** — verify `GROQ_API_KEY` is set.
- **Per-day gauges full** — Groq enforces RPD/TPD on free tiers; upgrade or wait for the daily reset.

### Why is there no $ spend?

Groq does not return billing data on rate-limit headers and offers no per-key billing endpoint. The four header dimensions (RPM/TPM/RPD/TPD) are the only signal a key can self-inspect.
