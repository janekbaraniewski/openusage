---
title: FAQ
description: Frequently asked questions about OpenUsage — privacy, cost, platform support, accuracy, and how it compares to other tools.
---

## Privacy and data

### Is my data sent anywhere?

No. OpenUsage is local-first. The only network calls it makes are to the AI provider APIs you've already authenticated to (OpenAI, Anthropic, OpenRouter, etc) — using **your own** keys to read **your own** usage data. There is no telemetry server, no analytics SDK, no phone-home.

The component called the "telemetry daemon" is named for event-sourced **collection**, not external **reporting**. It listens on a Unix domain socket on your machine; nothing on it is reachable over the network.

### Where is my data stored?

In two places, both on your machine:

- `~/.config/openusage/settings.json` — configuration (no secrets, just env-var **names**).
- `~/.local/state/openusage/telemetry.db` — SQLite store, only present in daemon mode.

Logs go to `~/.local/state/openusage/daemon.{stdout,stderr}.log`.

### Are my API keys stored anywhere?

No. Keys are referenced by env-var name in the config file (`api_key_env`). The actual value is read from your shell environment at fetch time and never written to disk.

### What about the integration hooks?

Hooks (Claude Code, Codex, OpenCode) post events from those tools to the local daemon socket. The data goes from the tool → daemon → SQLite → TUI. Nothing leaves your machine.

## Cost

### Does it cost money to run?

No. Provider rate-limit and billing endpoints are free to query. OpenUsage typically makes one or two requests per provider per poll cycle (default 30s). The cost on your account is rounding error.

### Will polling eat my rate limit?

In practice, no. Most providers serve rate-limit info in headers, so a single header-only request per poll is enough. For richer providers, OpenUsage caches what it can and re-polls only what changes.

If you're on a tight rate limit, raise the poll interval:

```json
{ "ui": { "refresh_interval_seconds": 120 } }
```

## Platform support

### Can I run it on Windows?

Yes. Pre-built Windows binaries are released; settings live at `%APPDATA%\openusage\settings.json`. The CGO requirement still applies if you build from source — you'll need a working MSVC or MinGW toolchain.

The daemon's service install (launchd / systemd) is Unix-only. On Windows, run the daemon manually as needed:

```
openusage telemetry daemon
```

### Can I run it on Linux?

Yes. Daemon installs to a systemd user unit (`~/.config/systemd/user/openusage-telemetry.service`).

### Can I run it on macOS?

Yes — this is the most-tested platform. Daemon installs as a launchd agent (`~/Library/LaunchAgents/com.openusage.telemetryd.plist`).

### Can I run it on a server / over SSH?

Yes. The TUI works in any ANSI terminal, including over SSH. For background collection without a UI, run daemon-only. See [headless servers](guides/headless-servers.md).

### Can I run it on multiple machines?

Yes — each runs independently. There is no built-in aggregation across machines. If you need cross-machine roll-up, copy each machine's `telemetry.db` and inspect them one at a time.

## Accuracy

### How accurate are the cost estimates?

Depends on the provider:

- **Direct API providers** (OpenAI, Anthropic, OpenRouter, Mistral, etc): the spend, balance, and credit numbers come straight from the vendor's API. They match the vendor's own dashboard.
- **Claude Code**: cost is an **API-equivalent estimate** computed from local pricing tables and local conversation files. It is **not** your subscription charge. Use it for relative attribution and trend tracking, not invoice reconciliation.
- **Cursor**: aggregated from the Cursor billing API. Composer cost is billable; AI code scoring is cached.
- **Codex / Gemini CLI / Copilot**: a mix of vendor APIs and local session files. Counts match what the vendor reports.

When in doubt, the per-provider page in the [provider catalog](/providers) lists exactly what each integration tracks and what it estimates.

### Why doesn't a balance match the vendor dashboard exactly?

A few reasons:

- Different time windows. Toggle with `w`.
- Caching on the provider side (e.g. OpenRouter rolls up analytics with a slight delay).
- BYOK vs platform credit overlap (most visible on OpenRouter).
- The vendor's own dashboard sometimes shows pending vs settled differently.

Numbers are accurate in the same sense the vendor's API is accurate — small lags and rounding are normal.

## Subscriptions and self-hosted

### Does it support self-hosted LLMs?

Yes for Ollama. The Ollama provider talks to the local server on `127.0.0.1:11434` and surfaces models, running processes, daily request counts, and (if cloud-authenticated) cloud credits.

For other self-hosted runtimes, the OpenAI-compatible providers can usually be pointed at a self-hosted endpoint with a `base_url` override. The provider doesn't know it's not OpenAI.

### Does it work with Anthropic Claude subscriptions?

Indirectly. The Claude Code provider reads local stats from `~/.claude/` and computes 5-hour billing blocks that mirror the subscription quota concept. The dollar values shown are **API-equivalent estimates**, not your subscription bill.

### Does it work with OpenAI ChatGPT (web)?

No. OpenUsage tracks API usage. ChatGPT web subscriptions are billed separately and have no public usage API.

## Comparisons

### How is this different from langfuse / helicone / openllmetry?

Those are **app-side observability platforms**: you instrument an LLM-powered application you build, send traces to a backend, and analyze them with a team UI. They're great when you're shipping an AI product.

OpenUsage is the inverse — **end-user spend monitoring for the human running coding tools**. You don't instrument anything; OpenUsage reads what your tools already record. There's no backend, no team dashboard, no SDK.

For the longer comparison see [openusage.sh vs openusage.ai](https://openusage.sh/docs/openusage-sh-vs-openusage-ai/) on the marketing site.

### How is this different from Cursor's built-in usage view?

Native dashboards show one provider at a time and only what that vendor exposes. OpenUsage shows **all your providers at once**, with consistent gauges and a unified detail panel. If you only ever use one tool, the native view is fine. If you mix Claude Code with Cursor with OpenRouter, OpenUsage is the unified view.

## Build and runtime

### Why does it require CGO?

Two parts of the codebase need a C SQLite library:

- The **Cursor provider** reads Cursor's local SQLite databases.
- The **telemetry store** uses SQLite for the daemon's event store.

Both link `github.com/mattn/go-sqlite3`, which is a CGO package. This is why pure-Go cross-compilation doesn't work out of the box and why you need a C toolchain to build from source.

### How does the daemon survive reboots?

On install, the daemon registers with the platform's service manager:

- macOS: launchd agent with `KeepAlive=true`, `RunAtLoad=true`.
- Linux: systemd user unit with `Restart=always`, `RestartSec=2`.

The unit file points at the binary's path on disk. If you move or delete that binary, reinstall after putting the new one in place.

### Why can't I install the daemon from `go run`?

`go run` builds to a temporary directory and the resulting binary is deleted when the command exits. The service manager's unit file would point at a missing path. Build a permanent binary first (`make build` → install to `/usr/local/bin/openusage` or similar), then run `openusage telemetry daemon install`.

### How do I uninstall completely?

```bash
openusage telemetry daemon uninstall
rm -rf ~/.config/openusage ~/.local/state/openusage
# macOS only:
brew uninstall openusage
```

If you installed integration hooks, remove them too:

```bash
openusage integrations uninstall claude_code
openusage integrations uninstall codex
openusage integrations uninstall opencode
```

(Backup files at `*.bak` next to each tool's config restore the pre-OpenUsage state if needed.)

## Customization

### Can I add my own theme?

Yes. Drop a JSON file with the same color tokens as a built-in theme into `~/.config/openusage/themes/`. The format is documented in [customization/external-themes](customization/external-themes.md).

### Can I rearrange the dashboard?

- Cycle layouts with `v` / `V` (Grid, Stacked, Tabs, Split, Compare).
- Reorder providers with Shift+J / Shift+K (or Ctrl+↑/↓, Alt+↑/↓).
- Toggle providers on and off in Settings (`,`).
- Hide widget sections per provider in Settings → Widget Sections.

### Can I add custom keybindings?

Not yet. The bindings shown with `?` are the canonical set. If there's one you'd want to remap, open an issue.

## Troubleshooting

### My provider doesn't show up

See [provider not detected](troubleshooting/provider-not-detected.md).

### The daemon won't start

See [daemon issues](troubleshooting/daemon-issues.md).

### Numbers look wrong

See the accuracy questions above and [common issues](troubleshooting/common-issues.md).

### How do I file a bug?

Capture a debug log:

```bash
OPENUSAGE_DEBUG=1 openusage 2> /tmp/openusage-debug.log
```

Then open an issue at [github.com/janekbaraniewski/openusage/issues](https://github.com/janekbaraniewski/openusage/issues) with the log, your platform, the version (`openusage version`), and which provider is involved.

## Project

### Is this open source?

Yes — MIT licensed. See [LICENSE](https://github.com/janekbaraniewski/openusage/blob/main/LICENSE).

### Who maintains it?

Jan Baraniewski with community contributors. PRs welcome — see [contributing](contributing/overview.md).

### Where does the roadmap live?

In GitHub issues. There's no separate roadmap document; what's planned is what's filed.
