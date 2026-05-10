---
title: Gemini API
description: Track Google Gemini API model catalog and per-model token limits in OpenUsage.
sidebar_label: Gemini API
---

# Gemini API

Surfaces the Google Gemini API's model catalog and per-model rate limits. The API does not expose billing data, so OpenUsage focuses on capabilities and limits.

## At a glance

- **Provider ID** — `gemini_api`
- **Detection** — `GEMINI_API_KEY` environment variable (also `GOOGLE_API_KEY` as an alias)
- **Auth** — API key
- **Type** — API platform (header-only / catalog data)
- **Tracks**:
  - Model count
  - Sample of up to 5 models
  - Per-model input and output token limits
  - Per-model RPM

## Setup

### Auto-detection

Set `GEMINI_API_KEY`. OpenUsage also detects `GOOGLE_API_KEY` and aliases it to this provider, so either variable works (the corresponding account IDs are `gemini-api` and `gemini-google` respectively).

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "gemini_api",
      "provider": "gemini_api",
      "api_key_env": "GEMINI_API_KEY",
      "base_url": "https://generativelanguage.googleapis.com/v1beta"
    }
  ]
}
```

## What you'll see

- Dashboard tile shows the count of available models.
- Detail view lists up to 5 sample models with their input and output token limits and RPM.

## API endpoints used

- `GET /v1/models?key=…`

## Caveats

- The Gemini API does not expose spend or quota usage. For session-level token data install [Gemini CLI](./gemini-cli.md) and authenticate with OAuth.
- The model sample is intentionally capped at 5 to keep the detail view readable; the full count is shown on the tile.

## Troubleshooting

- **Auth failed** — verify `GEMINI_API_KEY`; rotate via Google AI Studio if needed.
- **Empty model list** — the key may not have access to `v1beta`. Check your project's API enablement.

## Related

- [Gemini CLI](./gemini-cli.md) — OAuth-based local provider with session token data
