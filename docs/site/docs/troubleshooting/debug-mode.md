---
title: Debug mode
description: Turning on verbose logging and capturing a useful bug report.
---

When something is misbehaving and the dashboard isn't telling you why, debug mode is the first knob to turn.

## Enabling

Set `OPENUSAGE_DEBUG=1` in the environment that launches the binary:

```bash
OPENUSAGE_DEBUG=1 openusage 2> /tmp/openusage-debug.log
```

Effects:

- Theme loader prints which files it considered and why some were skipped.
- Daemon connection logs the socket path and any handshake errors.
- Integration installer logs each step (template render, backup, patch).
- Auto-detection logs why each provider was kept or skipped.
- Provider `Fetch()` errors include the wrapped error chain.

## Where logs go

| Mode | Where |
|---|---|
| TUI in direct mode | stderr (redirect to a file as above) |
| Daemon (foreground) | stderr |
| Daemon (installed service) | `~/.local/state/openusage/daemon.{stdout,stderr}.log`; Linux also `journalctl --user-unit openusage-telemetry.service` |
| Hook scripts | the agent's own logs (e.g. Claude Code session log) |

## Capturing a useful bug report

If you're filing an issue, include:

1. **OpenUsage version**
   ```bash
   openusage version
   ```

2. **Platform**
   ```bash
   uname -a
   echo "$TERM, $(tput colors) colors, $(tput cols)x$(tput lines)"
   ```

3. **Mode** — direct or daemon, and (if daemon) `openusage telemetry daemon status` output.

4. **Debug log** from a fresh reproduction. Reproduce the issue, quit, attach the file:
   ```bash
   OPENUSAGE_DEBUG=1 openusage 2> /tmp/openusage-debug.log
   # ... reproduce ...
   ```

5. **Redacted `settings.json`** — replace any tokens or hostnames you don't want public. Most importantly, **do not include API keys**; they shouldn't be in the file anyway because OpenUsage stores only env-var names.

6. **The provider involved**, if applicable. Provider-specific bugs are easier to triage with the provider ID and a snippet of the detail panel.

## What not to share

- Raw `telemetry.db`. It contains your usage history. If forensic detail is needed, the maintainer will ask for specific event types.
- API keys. They should never be in any log; if you see one, that's its own bug and worth reporting.
- Hook payloads with sensitive prompts. Set `OPENUSAGE_DEBUG=1` only briefly when reproducing.

## Disabling

Unset the variable or just don't pass it:

```bash
unset OPENUSAGE_DEBUG
openusage
```

## See also

- [Common issues](common-issues.md)
- [Daemon issues](daemon-issues.md)
- [Provider not detected](provider-not-detected.md)
