---
title: Antigravity CLI
description: Track Antigravity CLI context usage, model quotas, and session tokens in OpenUsage.
sidebar_label: Antigravity CLI
keywords: [antigravity cli usage tracker, agy quota tracking, antigravity token usage, antigravity status line]
---

# Antigravity CLI

Tracks the local Antigravity CLI (`agy`) through its documented custom status-line JSON feed. OpenUsage does not read Antigravity credentials or call a separate usage API.

## At a glance

- **Provider ID** — `antigravity`
- **Detection** — `agy` on `PATH` plus `~/.gemini/antigravity-cli/settings.json` or `brain/`
- **Auth** — local status-line feed; no API key required
- **Type** — coding agent
- **Dashboard color** — mauve
- **Tracks**:
  - model and workspace
  - context-window percentage
  - cumulative session input, output, and total tokens
  - current input, output, and cache token parts
  - per-bucket quota remaining and reset time
  - plan tier, agent state, CLI version, and session identity

## Setup

### Auto-detection

OpenUsage detects Antigravity when both conditions are true:

1. `agy` is available on `PATH`.
2. `~/.gemini/antigravity-cli/` contains `settings.json` or `brain/`.

### Install the status-line bridge

```bash
openusage integrations install antigravity
```

The installer backs up `settings.json` to `settings.json.bak`, registers `openusage antigravity statusline`, and enables `stack_with_default` so Antigravity's built-in line remains visible. If a different custom `statusLine.command` already exists, installation stops rather than replacing it.

Start or refresh an Antigravity session after installation. The command stores the latest payload at:

```text
${XDG_STATE_HOME:-~/.local/state}/openusage/antigravity-status.json
```

The state file is written atomically with `0600` permissions.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "antigravity",
      "provider": "antigravity",
      "auth": "local",
      "binary": "agy"
    }
  ]
}
```

For an alternate state location, add a provider path:

```json
{
  "provider_paths": {
    "status_file": "~/path/to/antigravity-status.json"
  }
}
```

## Status-line data

Antigravity runs the configured command whenever agent state changes, pipes a JSON document to its standard input, and renders the command's standard output below the prompt. OpenUsage accepts that same document through:

```bash
openusage antigravity statusline
```

The bridge prints a compact line such as `AGY · Gemini Pro · quota 94% · context 14%` while saving the structured payload for the dashboard and telemetry daemon.

## What is not tracked

- **Dollar spend.** The status-line contract exposes tokens, context, and quota fractions, not a reliable public price surface.
- **Full transcript content.** OpenUsage does not parse or store the Antigravity brain/transcript directory in this integration.
- **Credentials.** OAuth tokens, API keys, and cookies are never extracted.

## Troubleshooting

- **Provider missing:** run `which agy` and confirm `~/.gemini/antigravity-cli/` exists.
- **No status data:** run `openusage integrations list --all`, install the integration, then start an Antigravity session.
- **Existing status command:** back up or remove the custom `statusLine.command` yourself, then rerun the installer. OpenUsage refuses to overwrite it.
- **Stale data:** the dashboard reflects the most recent status-line invocation. Start an active session to refresh it.

## Related

- [Gemini CLI](./gemini-cli.md) — separate OAuth/session-file provider
- [Antigravity status-line documentation](https://antigravity.google/docs/cli/statusline/)
