---
title: "\"Unmapped\" telemetry sources"
description: A telemetry event is flowing in but has no tile. This page explains why and how to fix it with provider_links.
sidebar_label: Unmapped telemetry sources
---

# "Unmapped" telemetry sources

You installed an integration (typically the OpenCode plugin), spend events are flowing in, the dashboard knows about them — but they appear under an **Unmapped** label instead of landing on the provider tile you expected. Or the events you can see in **Settings → Telemetry** don't match any of the tiles on your dashboard.

This is the single most common confusion point and it's because OpenUsage tracks two separate things that don't share a vocabulary.

## Two vocabularies

### Configured providers

Accounts OpenUsage knows about. Each has an internal ID like `claude_code`, `copilot`, `gemini_api`, `gemini_cli`, `cursor`, `openrouter`, `openai`. These are the IDs you see as **tiles** on the dashboard.

### Telemetry sources

Events flowing in from integrations — Claude Code hooks, Codex notify, the OpenCode plugin. Each event is tagged with whatever provider name the **source tool** uses internally.

OpenCode, for example, uses its own model-registry IDs:

| What OpenCode calls it | What OpenUsage calls it |
|---|---|
| `anthropic` | `claude_code` |
| `google` | `gemini_api` |
| `github-copilot` | `copilot` |
| `openai` | `openai` |
| `openrouter` | `openrouter` |
| `moonshot` | `moonshot` |

When the dashboard hydrates, it has to attribute each telemetry event to a configured provider so the spend lands on the right tile.

## What "Unmapped" means

The lookup is "exact ID match" plus a small set of built-in defaults:

```go
// internal/config/config.go — DefaultProviderLinks()
"anthropic"      → "claude_code"
"google"         → "gemini_api"
"github-copilot" → "copilot"
```

Anything that doesn't match either gets bucketed under **Unmapped**. The event is still stored in the SQLite telemetry store — it just doesn't render on a tile until you tell OpenUsage how to route it.

## The fix: `telemetry.provider_links`

Add an explicit mapping in `~/.config/openusage/settings.json`:

```json
{
  "telemetry": {
    "provider_links": {
      "google": "gemini_api",
      "github-copilot": "copilot"
    }
  }
}
```

The defaults above are already applied; you only need entries for sources that don't match by name.

After editing, restart the daemon so the new mapping takes effect:

```bash
launchctl kickstart -k "gui/$(id -u)/com.openusage.telemetryd"   # macOS
systemctl --user restart openusage-telemetry.service              # Linux
```

You can also configure mappings interactively: open settings with <kbd>,</kbd>, switch to the **Telemetry** tab, navigate to an unmapped source, and press <kbd>m</kbd> to pick a target tile from a list.

## Common scenarios

### "I installed the OpenCode plugin and now nothing makes sense"

The OpenCode plugin emits one event per turn, tagged with the upstream model provider's ID — not with `opencode`. So a Claude-via-OpenCode turn shows up as an `anthropic` event, a Gemini-via-OpenCode turn as `google`, and so on. The plugin doesn't aggregate everything under a single OpenCode bucket.

Two ways to interpret this:

- If you want each upstream provider to have its own tile, configure those providers normally (set the env var, install the integration if applicable) and add `provider_links` for any name mismatches above.
- If you want a single OpenCode-shaped view of your activity, link every source you care about to `opencode`:

  ```json
  {
    "telemetry": {
      "provider_links": {
        "anthropic":      "opencode",
        "google":         "opencode",
        "github-copilot": "opencode",
        "openai":         "opencode",
        "openrouter":     "opencode",
        "moonshot":       "opencode"
      }
    }
  }
  ```

### "I see `OPENCODE_API_KEY` is set but no OpenCode tile appears"

The OpenCode provider polls the OpenCode (Zen) API to verify the key and list models — it doesn't poll for spend. Spend only appears when the OpenCode plugin is installed AND the resulting telemetry events route to a tile (see the previous scenario).

If the tile is missing entirely, check:

1. The env var name is exactly `OPENCODE_API_KEY` (or `ZEN_API_KEY` — both are accepted).
2. The daemon is running: `openusage telemetry daemon status`.
3. Run `openusage` and open settings (<kbd>,</kbd>) → **Providers** tab. Confirm `opencode` is listed and enabled.

### "My env var is set, but the provider isn't even auto-detected"

OpenUsage only auto-detects providers that have a built-in Go integration. The 19 supported providers are listed in the [provider catalog](/providers/). Setting an env var for a provider that isn't in the catalog will not produce a tile, no matter what — there's no code that knows how to talk to that API.

If you want a new provider supported, open a request on [GitHub Issues](https://github.com/janekbaraniewski/openusage/issues), or implement it yourself following the [add-a-provider guide](/contributing/add-provider/).

## Related

- [Concepts — Telemetry pipeline](/concepts/telemetry/) — what flows from a hook into a tile
- [Configuration reference — `telemetry.provider_links`](/reference/configuration/) — the schema
- [Daemon — Integrations](/daemon/integrations/) — what each integration emits
- [Provider catalog](/providers/) — the full list of supported providers
