---
title: Codex CLI
description: Track OpenAI Codex CLI sessions, rate limits, and credit balance in OpenUsage.
sidebar_label: Codex
---

# Codex CLI

Local-file provider for the OpenAI Codex CLI. Reads session logs, auth state, and config to show today's activity, plan info, and rate-limit windows.

## At a glance

- **Provider ID** — `codex`
- **Detection** — `~/.codex` directory on disk
- **Auth** — token stored in `~/.codex/auth.json` by the Codex CLI; no env var needed
- **Type** — coding agent
- **Tracks**:
  - Latest session: tokens, model, client
  - Daily session counts
  - Model and client breakdowns
  - Rate-limit windows (primary and secondary)
  - Credit balance
  - Plan and version
  - Patch stats

## Setup

### Auto-detection

OpenUsage registers the provider as soon as `~/.codex/` exists. Run the Codex CLI at least once to create it.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "codex",
      "provider": "codex",
      "extra": {
        "config_dir": "~/.codex",
        "sessions_dir": "~/.codex/sessions"
      }
    }
  ]
}
```

Override `config_dir` and `sessions_dir` only if the CLI uses non-default paths.

## What you'll see

- Dashboard tile shows the latest session model and today's session count.
- Detail view lists model and client breakdowns with token totals.
- Two rate-limit windows (primary and secondary) appear as gauges.
- If a token is present, credit balance and plan are pulled from the live usage endpoint.

## API endpoints used

- Optional live usage endpoint when `~/.codex/auth.json` contains a valid token

## Files read

- `~/.codex/sessions/**/*.jsonl` — session transcripts
- `~/.codex/auth.json` — auth token
- `~/.codex/config.toml` — CLI configuration
- `~/.codex/version.json` — installed version

## Caveats

- Credit balance only appears when the live endpoint is reachable; offline sessions still show local activity.
- Rate-limit windows are reported by the API and may differ from documented limits during quota changes.
- The provider has hooks-style integration with the daemon: see [Daemon integrations](../daemon/integrations.md).

## Troubleshooting

- **Tile is empty** — run `codex` once to populate `~/.codex/sessions/`.
- **No credit balance** — `~/.codex/auth.json` is missing or expired. Re-authenticate with the Codex CLI.
- **Sessions missing** — confirm `sessions_dir` matches the path Codex writes to.

## Related

- [OpenAI](./openai.md) — direct API rate limits for the underlying models
- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
