---
title: xAI (Grok)
description: Track xAI Grok credits, rate limits, and allowed models in OpenUsage.
sidebar_label: xAI
---

# xAI (Grok)

Full account visibility for xAI. Surfaces key metadata, credit breakdown, rate limits, and the list of models the key is allowed to call.

## At a glance

- **Provider ID** — `xai`
- **Detection** — `XAI_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Key info: name, ID, team
  - Credits: remaining, spent, granted (USD)
  - RPM and TPM
  - Allowed models

## Setup

### Auto-detection

Set `XAI_API_KEY`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "xai",
      "provider": "xai",
      "api_key_env": "XAI_API_KEY",
      "base_url": "https://api.x.ai/v1"
    }
  ]
}
```

## What you'll see

- Dashboard tile shows credits remaining against credits granted in USD.
- Detail view lists key name, team, and the spend-vs-granted breakdown.
- Allowed models appear as a list; calls to other models will fail.
- Rate limits (RPM, TPM) appear as gauges.

## API endpoints used

- `/v1/api-key`
- `/v1/models`

## Caveats

- Granted credits include both promotional and paid; the API does not split them further.
- Allowed models reflect the key's scope, not the team's full catalog.
- Currency is USD.

## Troubleshooting

- **Empty allowed models** — the key has no model permissions; create a new key with model access in the xAI console.
- **Spend higher than expected** — xAI charges for both successful and certain failed requests; check the console for itemized billing.
