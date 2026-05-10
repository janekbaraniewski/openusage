---
title: Install
description: Install OpenUsage on macOS, Linux, or Windows via Homebrew, script, or Go.
sidebar_position: 1
---

# Install

OpenUsage is a single Go binary. CGO is required (it links SQLite for the telemetry store), so all distribution channels ship pre-built binaries.

## macOS — Homebrew (recommended)

```bash
brew install janekbaraniewski/tap/openusage
```

Upgrade later with:

```bash
brew upgrade openusage
```

## All platforms — install script

```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash
```

The script picks the right binary for your OS/arch and drops it into `/usr/local/bin/openusage` (or another writable directory in your `PATH`).

:::tip
Read the script first if you prefer:
```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | less
```
:::

## Pre-built binaries

Download a release archive directly from the [GitHub releases page](https://github.com/janekbaraniewski/openusage/releases) and put `openusage` somewhere on your `PATH`.

Available targets:

- `darwin-amd64`, `darwin-arm64`
- `linux-amd64`, `linux-arm64`
- `windows-amd64`

## From source (Go 1.25+)

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

`CGO_ENABLED=1` must be on (it is by default on macOS and most Linux distros). On systems without a C toolchain, install one first:

- macOS: `xcode-select --install`
- Debian/Ubuntu: `sudo apt install build-essential`
- Fedora: `sudo dnf install gcc gcc-c++`
- Arch: `sudo pacman -S base-devel`
- Windows: install MinGW or MSYS2

## Verify

```bash
openusage version
```

You should see the version number, the commit, and the build date. If the command isn't found, make sure the install location is on your `PATH`.

## Start the daemon

The daemon is the runtime that polls providers, ingests hooks, and persists data to SQLite. The TUI reads from it. Install it once with:

```bash
openusage telemetry daemon install
```

This sets up a launchd agent (macOS) or a systemd user unit (Linux) and starts the service. See the [Daemon overview](../daemon/overview.md) for what it does and how to manage it.

## What's next

- [Quickstart](./quickstart.md) — run the dashboard for the first time
- [First-run walkthrough](./first-run.md) — what auto-detection picks up and how to read the dashboard

:::note CGO and cross-compilation
OpenUsage embeds [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) for the telemetry store, which requires CGO. Cross-compiling needs a target-specific C toolchain; most users should grab the pre-built binaries from the release page instead.
:::
