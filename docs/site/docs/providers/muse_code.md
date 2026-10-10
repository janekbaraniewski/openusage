---
title: Muse Code
description: Track Muse Code session spend and subscription quota in OpenUsage via local logs and the CLI keychain.
sidebar_label: Muse Code
keywords: [muse code usage tracker, muse spark quota tracking, muse code cost tracking, track muse spend locally]
---

# Muse Code

Tracks the `muse` CLI from local session logs plus live subscription quota. Secrets are never written to disk by OpenUsage: quota reads the CLI's macOS keychain entry (or Linux auth file) at fetch time.

## At a glance

- **Provider ID** — `muse_code`
- **Detection** — `muse` binary on `PATH`, a sessions dir, or a CLI auth file
- **Auth** — `muse login` (re-uses existing CLI credentials); `META_API_KEY` or `~/.config/openusage/muse.json` for the probe fallback
- **Type** — coding agent
- **Tracks**:
  - Session tokens, per-model breakdown, estimated cost
  - Subscription quota: session and weekly windows with resets
  - Quota-blocked state with reset time

## Setup

### Auto-detection

Triggers when any of these exist: the `muse` binary, `$XDG_DATA_HOME/muse/sessions` (or `~/.local/share/muse/sessions`), or the CLI auth file. Run `muse login` and complete at least one session.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "muse-code",
      "provider": "muse_code"
    }
  ]
}
```

Set `provider_paths.plan_name` (`Everyday Usage`, `High Usage`, or `Power Usage`) to name the plan on the tile — the quota probe returns an opaque tier ID.

## Credentials and the keychain prompt

Quota is fetched in this order: OAuth account endpoint (CLI OAuth token), Responses probe (Model API key). On macOS both secrets live in the CLI's keychain entry, so the first fetch of each launch pops one system approval prompt; reads are then memoized in-process and never re-prompt. On Linux the CLI auth file (`~/.config/muse/auth.json`) is the source of truth — no prompt. OpenUsage never writes these secrets anywhere: no credential file is created as a side effect.

## Data sources & how each metric is computed

Two data paths:

1. **Local session logs.** Token counts per session from the CLI's rollout files, priced at Meta's published Muse Spark rates (estimated).
2. **Live quota.** Session/weekly `used_percent` with resets. A 429 names no window, so blocks are attributed session-vs-weekly from the last remembered payload.

### Quota-blocked state

When quota is exhausted the tile header shows `LIMIT` with the reset time instead of fabricated percentages.

### What's NOT tracked

- **Plan display name.** The probe returns an opaque tier ID; set `provider_paths.plan_name`.
- **Browser quota.** No browser session is read; everything comes from CLI credentials.

### How fresh is the data?

- Polled every 30 s by default. Quota reflects the last successful fetch; blocks persist across restarts via shared quota memory.

## API endpoints used

- `POST api.meta.ai/muse-code/key` (OAuth account endpoint)
- `POST api.meta.ai/v1/responses` (Model API probe)

## Files read

- `$XDG_DATA_HOME/muse/sessions/**` (or `~/.local/share/muse/sessions/**`)
- `~/.config/muse/auth.json` (Linux credentials)
- `~/.config/openusage/muse.json` (only if you saved a key there yourself)
