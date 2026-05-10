---
title: Quickstart
description: Get OpenUsage running and see live data from your AI tools in under five minutes.
sidebar_position: 2
---

# Quickstart

You should reach a useful dashboard with **zero configuration**. This page shows the happy path and the keys you need to know.

## 1. Start the daemon

The daemon is the background process that polls providers, ingests agent hooks, and persists data to SQLite. The TUI reads from it.

```bash
openusage telemetry daemon install
```

This takes about five seconds. It registers a launchd agent (macOS) or a systemd user unit (Linux) and starts the service. Verify with:

```bash
openusage telemetry daemon status
```

## 2. Run the dashboard

```bash
openusage
```

That's it. OpenUsage:

1. Scans your environment for AI-tool API keys (e.g. `OPENAI_API_KEY`, `OPENROUTER_API_KEY`)
2. Looks for installed binaries and config dirs (e.g. `claude`, `cursor`, `~/.codex`)
3. Registers a provider account for each thing it finds
4. Connects to the daemon over its Unix socket and renders the read model

If a provider doesn't show up, it's almost always because the env var or binary isn't where OpenUsage looks. See [Provider not detected](../troubleshooting/provider-not-detected.md).

## 3. Move around

The defaults you'll use most often:

| Key | Action |
|---|---|
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Switch screens (Dashboard ↔ Analytics) |
| <kbd>↑</kbd> <kbd>↓</kbd> or <kbd>j</kbd> <kbd>k</kbd> | Move cursor |
| <kbd>←</kbd> <kbd>→</kbd> or <kbd>h</kbd> <kbd>l</kbd> | Navigate panels / sections |
| <kbd>Enter</kbd> | Open a provider's detail view |
| <kbd>Esc</kbd> | Back / clear filter |
| <kbd>r</kbd> | Refresh all providers |
| <kbd>/</kbd> | Filter providers |
| <kbd>v</kbd> | Cycle dashboard view (Grid → Stacked → Tabs → Split → Compare) |
| <kbd>w</kbd> | Cycle time window (today / 3d / 7d / 30d / all) |
| <kbd>t</kbd> | Cycle theme |
| <kbd>,</kbd> | Open settings |
| <kbd>?</kbd> | Help overlay |
| <kbd>q</kbd> | Quit |

Full list: [Keybindings reference](../reference/keybindings.md).

## 4. Read a tile

Each tile shows:

- A **status badge** in the corner — `OK ●`, `WARN ◐`, `LIMIT ◌`, `AUTH ◈`, `ERR ✗`, `UNKNOWN ◇`
- The **provider name** and account ID
- The **primary metric** (spend, credits, or quota)
- A **gauge bar** colored green → yellow → red as you approach a limit
- **Tokens** and **model mix** when the provider exposes them
- A **sparkline** of recent activity

Press <kbd>Enter</kbd> on a tile to open the full detail view: per-model breakdowns, charts, billing periods, and trends.

## 5. Add an API key

Most cloud providers need an env var. The catalog in [Providers](../providers/index.md) lists each one. For example:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
export OPENROUTER_API_KEY=sk-or-...
openusage
```

You can also paste keys interactively from the **API Keys** tab in the settings modal (<kbd>,</kbd>) — OpenUsage stores them as plain values that get loaded next session.

## 6. Install agent integrations

For richer per-session detail from Claude Code, Codex, and OpenCode, install their hooks. They post each turn directly to the daemon, giving you per-message data that polling alone cannot see.

```bash
openusage integrations install claude_code   # if you use Claude Code
openusage integrations install codex          # if you use Codex CLI
openusage integrations install opencode       # if you use OpenCode
```

See the [Daemon overview](../daemon/overview.md) for what each integration captures.

## What's next

- [First-run walkthrough](./first-run.md) — annotated tour of the UI
- [Concepts](../concepts/architecture.md) — mental model
- [Customization](../customization/themes.md) — themes, keybindings, widget layout
