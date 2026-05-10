---
title: OpenAI
description: Track OpenAI API rate limits and quotas in OpenUsage.
sidebar_label: OpenAI
---

# OpenAI

Lightweight rate-limit probe for the OpenAI API. OpenUsage issues a single header-only request and parses RPM and TPM limits — no billing data, no token counts.

## At a glance

- **Provider ID** — `openai`
- **Detection** — `OPENAI_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (header-only rate limits)
- **Tracks**:
  - RPM and TPM rate limits (limit, remaining, reset)
  - Auth status

## Setup

### Auto-detection

Set `OPENAI_API_KEY`. OpenUsage registers the provider on next start.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "openai",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "base_url": "https://api.openai.com",
      "extra": {
        "probe_model": "gpt-4.1-mini"
      }
    }
  ]
}
```

`probe_model` defaults to `gpt-4.1-mini`. Override `base_url` for proxies or Azure-style gateways.

## Data sources & how each metric is computed

OpenUsage sends one `GET https://api.openai.com/v1/models/{probe_model}` per poll cycle (default every 30 seconds in daemon mode). The probe model is `gpt-4.1-mini` unless `extra.probe_model` is set. The endpoint is read-only, returns a small JSON body that the provider discards, and is not billable.

Request headers:

- `Authorization: Bearer $OPENAI_API_KEY`

### `rpm` — requests per minute

- Source: response headers
  - `x-ratelimit-limit-requests`
  - `x-ratelimit-remaining-requests`
  - `x-ratelimit-reset-requests`
- Transform: copied verbatim into `Limit` / `Remaining`. Reset is decoded into `Resets["rpm"]`.
- Window: 1 minute. **Scope: the probe model only** — different models can have different per-minute limits on the same key.

### `tpm` — tokens per minute

- Source: response headers
  - `x-ratelimit-limit-tokens`
  - `x-ratelimit-remaining-tokens`
  - `x-ratelimit-reset-tokens`
- Transform: same shape as `rpm` but for tokens.

### Auth status

- Source: HTTP status code.
- Transform: `401`/`403` → `auth`; `429` → `limited` (with `retry_after` from `Retry-After` if present); otherwise `ok`.

### What's NOT tracked

- **Spend / cost.** OpenAI's API does not expose dollar figures or token usage to API keys. The Usage page on `platform.openai.com` is a session-cookie surface and is not polled by this provider.
- **Account-wide rate limits.** The numbers are scoped to the probe model.

### How fresh is the data?

- Polled every 30 s by default. One request per poll, no cache.

## API endpoints used

- `GET /v1/models/{probe_model}` — header-only probe (default `gpt-4.1-mini`).

## Caveats

:::note
OpenAI's API does not expose billing or token-usage data to API keys. OpenUsage cannot show spend for OpenAI; use [Codex CLI](./codex.md) or [OpenRouter](./openrouter.md) to see actual usage data.
:::

- Rate limits come from response headers; they reflect the probe model's quota, not your account-wide spend.
- The probe is a single request per poll cycle — negligible cost.

## Troubleshooting

- **Auth failed** — verify `OPENAI_API_KEY` is set and valid; rotate if leaked.
- **No data** — the probe model may be unavailable on your tier. Set `probe_model` to a model your key can access.

### Why is there no $ spend?

OpenAI does not return billing or usage figures on its rate-limit headers, and the Usage and Billing pages are session-cookie surfaces, not API endpoints accessible with a key. Codex (for ChatGPT Pro/Plus accounts) and OpenRouter (when proxying OpenAI) both expose actual usage; either provider gives you a real dollar tile.

### Why are my RPM/TPM different from the OpenAI dashboard?

The numbers come from headers attached to a request for `probe_model`. Different models share different rate-limit pools on the same account. Set `extra.probe_model` to the model you actually call most.

## Related

- [Codex CLI](./codex.md) — OpenAI's coding agent with local session and credit data
- [OpenRouter](./openrouter.md) — proxy with full billing visibility for OpenAI models
