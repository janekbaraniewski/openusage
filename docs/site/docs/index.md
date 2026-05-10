---
title: OpenUsage docs
description: Local-first terminal dashboard for AI tool spend, quotas, and rate limits across 18 providers.
slug: /
sidebar_position: 1
sidebar_label: Welcome
hide_table_of_contents: true
---

# OpenUsage

Local-first terminal dashboard for AI tool spend, quotas, and rate limits across **18 providers** — Claude Code, Codex CLI, Cursor, Copilot, OpenRouter, OpenAI, Anthropic, and more.

```bash
brew install janekbaraniewski/tap/openusage
openusage
```

That is the entire setup. OpenUsage auto-detects installed AI tools and API keys on your workstation and shows live data in your terminal.

![OpenUsage dashboard](/img/dashboard.png)

## Why OpenUsage

- **One view across every AI tool** — coding agents, API platforms, local runtimes, side by side
- **Local-first** — no cloud, no telemetry sent anywhere; your data never leaves the machine
- **Zero config** — run `openusage` and the dashboard fills itself in
- **Background tracking** — optional daemon collects data even when the dashboard is closed
- **Tool integrations** — opt-in hooks for Claude Code, Codex CLI, and OpenCode add per-session detail

## Where to start

<div className="card-grid">
  <div className="card">
    <a href="./getting-started/install/">
      <h3>Install</h3>
      <p>Homebrew, install script, or build from source. Two minutes.</p>
    </a>
  </div>
  <div className="card">
    <a href="./getting-started/quickstart/">
      <h3>Quickstart</h3>
      <p>Run the dashboard, navigate the UI, learn the keys you need.</p>
    </a>
  </div>
  <div className="card">
    <a href="./concepts/architecture/">
      <h3>How it works</h3>
      <p>Mental model: detection, providers, snapshots, daemon mode.</p>
    </a>
  </div>
  <div className="card">
    <a href="./providers/">
      <h3>Provider catalog</h3>
      <p>Setup notes for all 18 providers with detection details.</p>
    </a>
  </div>
  <div className="card">
    <a href="./daemon/overview/">
      <h3>Background daemon</h3>
      <p>Continuous data collection, hooks, integrations, persistence.</p>
    </a>
  </div>
  <div className="card">
    <a href="./reference/configuration/">
      <h3>Configuration</h3>
      <p>The full <code>settings.json</code> schema with examples.</p>
    </a>
  </div>
</div>

## What you can do with it

| Goal | Page |
|---|---|
| Track which AI tool is burning budget | [Cost attribution guide](./guides/cost-attribution.md) |
| Track multiple keys for the same provider | [Multi-account guide](./guides/multi-account.md) |
| Run on a headless server | [Headless servers guide](./guides/headless-servers.md) |
| Customize the look | [Themes](./customization/themes.md) |
| Add a provider that doesn't exist yet | [Contributing — add a provider](./contributing/add-provider.md) |

## Help

- [FAQ](./faq.md)
- [Troubleshooting](./troubleshooting/common-issues.md)
- [GitHub issues](https://github.com/janekbaraniewski/openusage/issues)

OpenUsage is open source under the [MIT license](https://github.com/janekbaraniewski/openusage/blob/main/LICENSE).
