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

## Data sources & how each metric is computed

OpenUsage sends one `GET https://generativelanguage.googleapis.com/v1beta/models?key=$GEMINI_API_KEY` per poll cycle (default every 30 seconds in daemon mode). Auth is the API key passed as a query parameter; Gemini does not accept Bearer auth on this surface. The JSON body is parsed for model metadata and rate-limit headers are read when present.

### `available_models` — number of generative models

- Source: filtered count of entries in the response array `models[]` whose `supportedGenerationMethods` includes `generateContent`. Embedding-only and other non-chat models are excluded.
- Transform: `len(filtered)`.

### Sample model list

- Up to 5 filtered model names (with the `models/` prefix stripped) are stored in `Raw["models_sample"]` and rendered in the detail view.

### `input_token_limit` / `output_token_limit` — per-model context window

- Source: the first matching entry in `models[]` whose `name` contains `gemini-2.5-flash` or `gemini-2.0-flash`. Fields used: `inputTokenLimit`, `outputTokenLimit`, and `displayName` (stored as `Raw["model_name"]`).
- Transform: copied verbatim.
- Window: per-request (this is the model's max context size, not a rate limit).

### `rpm` — requests per minute

- Source: response headers
  - `x-ratelimit-limit`
  - `x-ratelimit-remaining`
  - `x-ratelimit-reset`
- Note: Gemini only emits these on some surfaces; on a free-tier key they are often absent and the metric is omitted entirely.

### Auth status

- Source: HTTP status code.
- Transform: `400`/`401`/`403` → `auth` (Gemini returns 400 for invalid keys); `429` → `limited` (and `Raw["retry_delay"]` is filled from the `retryDelay` metadata in the JSON error body); otherwise `ok`.

### What's NOT tracked

- **Spend / cost.** The API does not expose billing or cumulative token usage to API keys.
- **Account-wide usage.** No per-key request counter exists on the v1beta surface.

### How fresh is the data?

- Polled every 30 s by default. One request per poll, no cache.

## API endpoints used

- `GET /v1beta/models?key=$GEMINI_API_KEY`

## Caveats

- The Gemini API does not expose spend or quota usage. For session-level token data install [Gemini CLI](./gemini-cli.md) and authenticate with OAuth.
- The model sample is intentionally capped at 5 to keep the detail view readable; the full count is shown on the tile.

## Troubleshooting

- **Auth failed** — verify `GEMINI_API_KEY`; rotate via Google AI Studio if needed.
- **Empty model list** — the key may not have access to `v1beta`. Check your project's API enablement.

### Why is there no $ spend?

The `generativelanguage.googleapis.com` surface does not expose billing or per-key usage to API keys. Use the [Gemini CLI](./gemini-cli.md) provider for OAuth-backed quota data and local session token counts.

## Related

- [Gemini CLI](./gemini-cli.md) — OAuth-based local provider with session token data
