---
title: First-run walkthrough
description: A tour of the OpenUsage dashboard on first launch, from auto-detection output to keybindings.
sidebar_position: 3
---

# First-run walkthrough

This page walks through what happens the first time you start OpenUsage, what you'll see, and how to get value from each pane.

## Before you start

You don't need a config file. OpenUsage will create `~/.config/openusage/settings.json` (or `%APPDATA%\openusage\settings.json` on Windows) the first time it persists state — but the dashboard works fine without one.

The more of the following you have on your machine, the more populated the dashboard will be:

- **Coding tools**: `claude` CLI, `cursor`, `codex`, `gemini`, `gh` (with Copilot extension), `ollama`, `aider`
- **API keys** — set as env vars in your shell (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`, `DEEPSEEK_API_KEY`, `MOONSHOT_API_KEY`, `XAI_API_KEY`, `ZAI_API_KEY`, `GEMINI_API_KEY`, `ALIBABA_CLOUD_API_KEY`), exported in your shell rc files (`~/.zshrc`, `~/.bashrc`, `~/.config/fish/config.fish`, modular `~/.zshrc.d/*`), or stored by Aider/OpenCode/Codex in their config files. macOS keychain entries from the Claude Code CLI are also picked up.

A complete list of env-var names lives in [Environment variables](../reference/env-vars.md). To preview what will be detected before launch, run `openusage detect`.

## Step 1 — Launch

```bash
openusage
```

OpenUsage opens full-screen. The first frame may show partial data because the daemon is still polling providers and ingesting any pending hook events.

You'll see:

- **Top bar** — current screen (Dashboard or Analytics), time window, status indicators
- **Main pane** — provider tiles in a grid (or list, depending on terminal width)
- **Bottom hint bar** — context-relevant keybindings

If your terminal is narrow, OpenUsage automatically switches to **Stacked** view. Resize larger and press <kbd>v</kbd> to cycle through other layouts.

## Step 2 — Read the tiles

A tile shows the most useful number per provider, plus a status. Examples of what fills each tile:

| Provider | Primary metric | What's interesting |
|---|---|---|
| Claude Code | Cost (estimated) | Per-model token mix, current 5h billing block, burn rate |
| Cursor | Plan spend | Used vs included, plus AI code score |
| Copilot | Quota remaining | Chat / completions / premium interactions |
| OpenRouter | Credits | Daily/weekly/monthly usage, model mix |
| OpenAI | Rate limits | rpm/tpm limit and remaining (header probe only) |
| Anthropic | Rate limits | rpm/tpm limit and remaining (header probe only) |
| Mistral | Monthly spend (EUR) | Calendar-month spend, token totals |
| Moonshot | Balance breakdown | Cash + voucher (USD region) or CNY region |
| Ollama | Local models | Loaded models, VRAM, request rate from logs |

The full per-provider breakdown is in the [Provider catalog](../providers/index.md).

## Step 3 — Drill into a provider

Press <kbd>Enter</kbd> on a tile to open its detail view. You'll see:

- A **header** with status, account, plan, and last update time
- **Cards** for spend, quotas, token totals
- **Charts** — gauges, horizontal bars, sparklines
- **Per-model breakdown** when available
- **Activity heatmap** (hour-of-day) when there's enough data

Use <kbd>j</kbd>/<kbd>k</kbd> to scroll, <kbd>Tab</kbd>/<kbd>Shift+Tab</kbd> to jump between sections, <kbd>Esc</kbd> to go back.

## Step 4 — Try the Analytics screen

Press <kbd>Tab</kbd> (or <kbd>Shift+Tab</kbd>) to switch to the **Analytics** screen.

:::note Opt-in
Analytics is gated behind `experimental.analytics` in your settings. If <kbd>Tab</kbd> doesn't seem to do anything, enable it:

```json
{ "experimental": { "analytics": true } }
```
:::

Analytics aggregates across providers:

- **Metric strip** — window spend, token volume, spend/active day, spend trend
- **Cost trend chart** — daily spend over the window
- **Provider / model leaderboards** — top spenders
- **Insights** — anomalies and highlights
- **Budget pressure** — limit utilization with burn-rate forecasts
- **Activity heatmap** — when you actually use these tools

Sort the leaderboards with <kbd>s</kbd>. Filter with <kbd>/</kbd>.

## Step 5 — Customize

Press <kbd>,</kbd> to open the settings modal. Tabs:

1. **Providers** — enable/disable, reorder
2. **Widget Sections** — choose which cards show on tiles and detail views
3. **Theme** — pick from 18 bundled themes
4. **View** — Grid / Stacked / Tabs / Split / Compare
5. **API Keys** — paste keys interactively
6. **Telemetry** — link unmapped telemetry sources to providers
7. **Integrations** — install hooks for Claude Code, Codex, OpenCode

Move around with <kbd>j</kbd>/<kbd>k</kbd>, toggle/apply with <kbd>Space</kbd> or <kbd>Enter</kbd>, reorder with <kbd>Shift+J</kbd>/<kbd>Shift+K</kbd>. Close with <kbd>,</kbd> or <kbd>Esc</kbd>.

## Step 6 — Install agent integrations

For per-turn detail from agents you actually use (Claude Code, Codex, OpenCode), install the matching hook. Each one posts every turn directly to the daemon, capturing detail polling alone cannot see:

```bash
openusage integrations install claude_code
openusage integrations install codex
openusage integrations install opencode
```

Read the [Daemon overview](../daemon/overview.md) for what gets captured.

## Where to go next

- [Concepts](../concepts/architecture.md) — how the pieces fit together
- [Provider catalog](../providers/index.md) — setup notes per provider
- [Customization](../customization/themes.md) — themes, widgets, keybindings
- [Configuration reference](../reference/configuration.md) — every `settings.json` field
