---
title: GitHub Copilot
description: Track GitHub Copilot quotas, org seats, and rate limits in OpenUsage via the gh CLI.
sidebar_label: Copilot
---

# GitHub Copilot

Wraps the `gh` CLI (or the standalone `copilot` binary) to surface Copilot entitlements, quotas, and org metrics. No GitHub PAT is needed: OpenUsage shells out to commands you've already authorized.

## At a glance

- **Provider ID** — `copilot`
- **Detection** — `gh` CLI with the Copilot extension installed, **or** a standalone `copilot` binary plus `~/.copilot/`
- **Auth** — `gh auth login` (re-uses existing GitHub credentials), or local Copilot CLI state
- **Type** — coding agent
- **Tracks**:
  - User, plan, SKU
  - Chat, code, and premium quotas (entitlement, overage, remaining)
  - Org seats and feature toggles
  - Org metrics: active and engaged users by editor and model
  - Rate limits
  - Local session model and workspace info

## Setup

### Auto-detection

Two paths trigger detection:

1. **gh CLI** — `gh` on `PATH` with the Copilot extension installed
2. **Standalone CLI** — a `copilot` binary on `PATH` plus a `~/.copilot/` directory

Run `gh auth status` to confirm you're signed in.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "copilot",
      "provider": "copilot",
      "binary": "/usr/local/bin/gh",
      "extra": {
        "config_dir": "~/.copilot",
        "copilot_binary": "/usr/local/bin/copilot"
      }
    }
  ]
}
```

Set `binary` to the `gh` path; `copilot_binary` is only needed if the standalone CLI lives somewhere unusual.

## What you'll see

- Dashboard tile shows your plan and SKU plus headline quota usage (chat, code, premium).
- Detail view splits each quota into entitlement, used, overage, and remaining.
- Org admins see active/engaged user counts broken down by editor (VS Code, JetBrains, etc.) and model.
- Rate limits appear as a gauge.

## API endpoints used

All via `gh` subprocess; no direct HTTP calls:

- `gh auth status`
- `gh api user`
- `gh api graphql` for SKU status, rate limits, and org data

## Files read

- `~/.copilot/logs/**`
- `~/.copilot/session-state/`
- `~/.copilot/config.json`
- `~/.config/github-copilot/devices.json`

## Caveats

- Org metrics only appear if your account has admin access to the org.
- The standalone Copilot CLI is newer and exposes a different subset of data; the `gh` path is preferred when both are available.
- Premium quotas reset monthly per GitHub's billing cycle.

## Troubleshooting

- **No data** — run `gh auth login` and ensure the `copilot` extension is installed (`gh extension install github/gh-copilot`).
- **Org metrics missing** — your account isn't a Copilot Business/Enterprise admin; this is expected.
- **Stale rate limits** — the GraphQL query is rate-limited; OpenUsage respects the polling interval to avoid hammering it.
