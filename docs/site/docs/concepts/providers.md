---
title: Providers
description: What a provider is in OpenUsage, the three categories, and how each one shapes its own dashboard and detail widgets.
---

A **provider** in OpenUsage is a single Go package that knows how to talk to one AI service and produce a normalized `UsageSnapshot`. There are 18 providers shipped in the binary, and each one declares both how it fetches data and how it should look in the TUI.

## The provider contract

Every provider implements the same interface:

```go
type UsageProvider interface {
    ID() string
    Describe() ProviderInfo
    Spec() ProviderSpec
    DashboardWidget() DashboardWidget
    DetailWidget() DetailWidget
    Fetch(ctx context.Context, acct AccountConfig) (UsageSnapshot, error)
}
```

- **`ID()`** — short stable string like `openai`, `claude_code`, `openrouter`. Used in config and as the URL key in telemetry.
- **`Describe()`** — display name, vendor, brief description.
- **`Spec()`** — bundles auth metadata, setup hints, and the dashboard/detail widget definitions.
- **`Fetch()`** — the only side-effecting method. Given an `AccountConfig`, returns one `UsageSnapshot`.

## Categories

Providers fall into three buckets based on how they collect data.

### API platforms

Providers that hit a vendor REST API with the user's key. Most of these probe rate-limit headers cheaply; some pull rich JSON about credits and per-model usage.

Examples: `openai`, `anthropic`, `openrouter`, `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`, `alibaba_cloud`, `moonshot`, `zai`.

Detection signal: an env var holding the key.

### Coding agents

Providers backed by a local CLI or IDE. They usually read on-disk session files, optionally combined with a vendor API.

Examples: `claude_code`, `cursor`, `codex`, `copilot`, `gemini_cli`, `opencode`.

Detection signal: a binary on `$PATH` plus a config directory.

### Local runtimes

Providers that talk to a process running on your own machine.

Examples: `ollama`.

Detection signal: a reachable local server, optionally with a cloud key.

## What a provider declares

The `ProviderSpec` returned from `Spec()` is the static metadata that drives both setup and rendering. It typically includes:

- **Auth method** — API key, OAuth, local credentials, or none.
- **Required env var or path** — how detection finds it.
- **Setup hints** — links and copy used in the Settings modal.
- **DashboardWidget** — the small tile shown in the grid (label, primary gauge, status badge layout).
- **DetailWidget** — the larger panel shown when the tile is selected (sections, tabs, tables).

Because rendering is data-driven, adding a new metric to a provider is usually a matter of adding a field to `UsageSnapshot` and a row to `DetailWidget` — no TUI changes required.

## What `Fetch()` produces

A `UsageSnapshot` carries every metric a provider can express:

- account identity and timestamp
- spend in the provider's reported currency
- token counts (input, output, cache, reasoning)
- per-model breakdown
- rate-limit windows (rpm, tpm, rpd, tpd)
- status (`OK`, `WARN`, `LIMIT`, `AUTH`, `ERR`)
- arbitrary key/value extras for provider-specific detail

For more detail on the snapshot model see [snapshots](snapshots.md).

## How a provider becomes active

1. The provider package is registered in `internal/providers/registry.go` via `AllProviders()`.
2. Detection or manual config produces an `AccountConfig` whose `provider` field matches the provider's `ID()`.
3. The runtime calls `Fetch()` on a ticker (direct mode) or via the daemon's pipeline (daemon mode).
4. The latest snapshot is rendered through the provider's widget definitions.

## The 18 providers at a glance

| Category | Providers |
|---|---|
| API platforms | openai, anthropic, openrouter, groq, mistral, deepseek, xai, gemini_api, alibaba_cloud, moonshot, zai |
| Coding agents | claude_code, cursor, codex, copilot, gemini_cli, opencode |
| Local runtimes | ollama |

For the full per-provider reference (auth, endpoints, fields tracked, caveats), see the [provider catalog](/providers).

## Adding your own

The contract is small and stable. The full step-by-step lives at [contributing/add-provider](../contributing/add-provider.md).
