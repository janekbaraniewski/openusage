---
title: Upgrade
description: Upgrade an existing OpenUsage install and refresh integrations.
sidebar_position: 4
---

# Upgrade

OpenUsage versions are backward-compatible with the on-disk SQLite store and `settings.json`. Upgrading is safe.

## Upgrade the binary

### Homebrew

```bash
brew update
brew upgrade openusage
```

### Install script

Re-running the script downloads the latest release and overwrites the binary in place:

```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash
```

### Go install

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

### Manual

Download the new release archive from [GitHub releases](https://github.com/janekbaraniewski/openusage/releases) and replace the binary on your `PATH`.

## Upgrade integrations

If you installed any tool integrations (Claude Code hook, Codex notify hook, OpenCode plugin), upgrade them so the embedded scripts match the new binary's expected protocol:

```bash
openusage integrations upgrade --all
```

To upgrade a single integration:

```bash
openusage integrations upgrade claude_code
```

The upgrade re-renders the embedded template, replaces the previous hook script (a `.bak` of the old one is kept), and bumps the version recorded in `~/.config/openusage/settings.json`.

## Restart the daemon

If you have the daemon installed as a service, the new binary will be picked up on the next service restart:

### macOS

```bash
launchctl kickstart -k "gui/$(id -u)/com.openusage.telemetryd"
```

### Linux

```bash
systemctl --user restart openusage-telemetry.service
```

Check status:

```bash
openusage telemetry daemon status
```

## Verify

```bash
openusage version
```

The version, commit, and build date should reflect the new release.

## What's next

- [Daemon install](../daemon/install.md)
- [Integrations](../daemon/integrations.md)
- [Uninstall](./uninstall.md)
