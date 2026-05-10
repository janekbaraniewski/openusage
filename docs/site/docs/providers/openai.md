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

## What you'll see

- Dashboard tile shows auth status and the most-constrained rate-limit gauge.
- Detail view splits RPM and TPM into limit, remaining, and reset time.

## API endpoints used

- `GET /v1/models/{probe_model}` — header-only probe

## Caveats

:::note
OpenAI's API does not expose billing or token-usage data to API keys. OpenUsage cannot show spend for OpenAI; use [Codex CLI](./codex.md) or [OpenRouter](./openrouter.md) to see actual usage data.
:::

- Rate limits come from response headers; they reflect the probe model's quota, not your account-wide spend.
- The probe is a single request per poll cycle — negligible cost.

## Troubleshooting

- **Auth failed** — verify `OPENAI_API_KEY` is set and valid; rotate if leaked.
- **No data** — the probe model may be unavailable on your tier. Set `probe_model` to a model your key can access.

## Related

- [Codex CLI](./codex.md) — OpenAI's coding agent with local session and credit data
- [OpenRouter](./openrouter.md) — proxy with full billing visibility for OpenAI models
