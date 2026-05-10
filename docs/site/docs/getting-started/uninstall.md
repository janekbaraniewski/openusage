---
title: Uninstall
description: Cleanly remove OpenUsage, its daemon, integrations, config, and data.
sidebar_position: 5
---

# Uninstall

OpenUsage is a single binary plus a few user-scoped files. This page covers everything to remove.

## 1. Remove integrations first

If you've installed any tool integrations, uninstall them so they don't leave dead hook scripts behind:

```bash
openusage integrations list           # see what's installed
openusage integrations uninstall claude_code
openusage integrations uninstall codex
openusage integrations uninstall opencode
```

Each `uninstall` patches the target tool's config file to remove its registered hook entry, then deletes the hook script. A `.bak` of the previous tool config is preserved.

## 2. Stop and remove the daemon

```bash
openusage telemetry daemon uninstall
```

This unloads the launchd agent (macOS) or disables and removes the systemd user unit (Linux), and deletes the service file.

If the command fails (binary already gone), remove the service files manually:

### macOS

```bash
launchctl bootout "gui/$(id -u)" ~/Library/LaunchAgents/com.openusage.telemetryd.plist 2>/dev/null
rm -f ~/Library/LaunchAgents/com.openusage.telemetryd.plist
```

### Linux

```bash
systemctl --user disable --now openusage-telemetry.service 2>/dev/null
rm -f ~/.config/systemd/user/openusage-telemetry.service
systemctl --user daemon-reload
```

## 3. Remove the binary

### Homebrew

```bash
brew uninstall openusage
brew untap janekbaraniewski/tap     # optional
```

### Manual

```bash
which openusage                     # find it
rm $(which openusage)
```

## 4. Remove user data (optional)

OpenUsage stores config, themes, hooks, and telemetry data in user directories. None of this is shared with other users on the system.

```bash
# Config
rm -rf ~/.config/openusage

# State (SQLite store, socket, logs, spool)
rm -rf ~/.local/state/openusage
```

On macOS, `~/.config` and `~/.local/state` are honored — OpenUsage uses XDG paths, not `~/Library/Application Support/`.

On Windows:

```powershell
Remove-Item -Recurse -Force "$env:APPDATA\openusage"
```

## 5. Confirm

```bash
which openusage              # should be empty
ls ~/.config/openusage 2>&1  # should say "No such file"
ls ~/.local/state/openusage 2>&1
```

That's it. OpenUsage is fully removed.
