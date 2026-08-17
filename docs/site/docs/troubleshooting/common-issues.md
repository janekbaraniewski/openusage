---
title: Common issues
description: The four issues most users hit, with fast diagnosis steps for each.
---

This page is a triage guide. Match the symptom to a section, run through the checks, then jump to the deeper page for that area if needed.

## "No providers shown"

Symptoms: the dashboard launches but the tile grid is empty, or shows only "no accounts configured".

Checks, in order:

1. **Are any provider env vars set in this shell?**
   ```bash
   env | grep -E '(OPENAI|ANTHROPIC|OPENROUTER|GROQ|MISTRAL|DEEPSEEK|XAI|GEMINI|ALIBABA|MOONSHOT|ZAI|ZHIPUAI|OPENCODE|ZEN)_API_KEY'
   ```
   If nothing prints, auto-detection has nothing to find. Export at least one key in the same shell that runs `openusage`.

2. **Is auto-detection turned off?** Open `~/.config/openusage/settings.json` and verify `"auto_detect": true`. If you've set it to `false`, only manually declared `accounts` will load.

3. **Does any local agent have a config dir?** For coding agents, the binary alone isn't enough — the agent must have been run at least once.
   ```bash
   ls -d ~/.claude ~/.codex ~/.gemini ~/.copilot 2>/dev/null
   ```

4. **Is `OPENUSAGE_DEBUG=1` showing skipped detections?**
   ```bash
   OPENUSAGE_DEBUG=1 openusage 2> /tmp/usage.log
   ```
   Quit and read the log; missed providers print a reason.

If a specific provider is missing, see [provider not detected](provider-not-detected.md).

## "API key invalid" / `AUTH` badge

Symptoms: a tile renders but shows the `AUTH` (◈) badge.

Checks:

1. **Is the env var actually exported, or just shell-local?**
   ```bash
   echo "OPENAI_API_KEY=${OPENAI_API_KEY:+set}"
   ```
   `set` should print. If empty, the variable is not in the launched process's environment.

2. **Does the key still work?** Test directly:
   ```bash
   curl -sS https://api.openai.com/v1/models -H "Authorization: Bearer $OPENAI_API_KEY" | head -1
   ```
   A 401 means the key is revoked or wrong; rotate it.

3. **Does the key have access to the probe model?** OpenAI's default probe is `gpt-4.1-mini`. Restricted keys may 403 on that model — switch via `probe_model` in the account config.

4. **Is `base_url` correct?** A typo here makes every request 404 or 401. Restore the default by removing the field.

5. **For local-credential providers (Claude Code, Codex, Cursor, Gemini CLI):** the failure is in local auth files, not env vars. Re-login via the tool's own CLI.

## "Dashboard frozen"

Symptoms: numbers don't change, gauges don't update, status badges stay stale.

Checks:

1. **Press `r`.** Forces a refresh of every account. If numbers move, the poll ticker was just slow.

2. **What does the daemon say?**
   ```bash
   openusage telemetry daemon status
   ```
   A dead daemon means stale data forever — see [daemon issues](daemon-issues.md).

3. **Is the terminal too small?** Below ~80 columns the dashboard collapses to Stacked view, which can hide updates above the fold. Resize, then `r`.

4. **Are all providers in `WARN` or `ERR`?** `OPENUSAGE_DEBUG=1 openusage` prints fetch errors as they happen. A network outage or DNS issue can stall everything.

5. **Did you suspend and resume the laptop?** The poll ticker continues from where it stopped, which can mean ~30s of staleness post-wake. `r` to force.

## "Data is stale" (numbers behind reality)

Symptoms: spend or token counts are noticeably lower than what the vendor's own dashboard shows.

Checks:

1. **What does each provider actually expose?** Some providers (Anthropic, OpenAI) only expose rate-limit headers, not historical spend. Spend totals there come from local files (claude_code, codex) or cached provider state (cursor, openrouter). The daemon can only persist what the provider returns.

2. **Is the daemon running?**
   ```bash
   openusage telemetry daemon status
   ```
   If it's not running, the TUI is reading a stale read model. Restart:
   ```bash
   # macOS
   launchctl kickstart -k gui/$(id -u)/com.openusage.telemetryd
   # Linux
   systemctl --user restart openusage-telemetry.service
   ```

3. **Did you install integrations after the data accumulated?** Hooks only see future events. Polling fills in the past as far back as the provider lets it.

4. **For Claude Code:** the local stats files refresh after each conversation ends. A long-running conversation in progress is not yet reflected. Wait for it to complete or close the tab.

5. **Time window mismatch.** A `1d` window resets at local midnight. If you opened the dashboard at 23:59 and looked again at 00:01, the totals just rolled over. Cycle to `7d` or `30d` for context.

## "Error loading config: parsing config ..."

Symptoms: a command exits immediately with a parse error naming your
`settings.json`, for example `invalid character 's' after top-level value`.

The config file is not valid JSON. Only commands that read the config are
affected — `openusage version`, `openusage --help`, and the daemon
install/uninstall commands keep working, so you can still manage the daemon
while the file is broken.

One corruption shape heals itself: if the file holds a complete config object
followed by leftover bytes (the signature of a write that did not truncate, so
the tail of a longer previous version survives), OpenUsage recovers the config,
rewrites the file cleanly, and keeps the original as
`settings.json.corrupt` for inspection. You only see the parse error for damage
that leaves no complete config to recover, such as a truncated file.

To recover by hand, inspect the end of the file:

```bash
tail -20 ~/.config/openusage/settings.json
python3 -m json.tool ~/.config/openusage/settings.json
```

If it cannot be salvaged, move it aside and OpenUsage starts from defaults and
re-detects your tools:

```bash
mv ~/.config/openusage/settings.json ~/.config/openusage/settings.json.bad
```

Manually configured accounts and preferences (theme, widget layout) live in that
file and have to be re-entered. Collected usage history does not — it lives in
the telemetry database (see [paths](../reference/paths.md)).

## When to file an issue

If none of the above helps, capture a debug log:

```bash
OPENUSAGE_DEBUG=1 openusage 2> /tmp/usage-debug.log
```

Then redact any secrets and attach to a GitHub issue. See [debug mode](debug-mode.md) for the full bug-report recipe.

## See also

- [Provider not detected](provider-not-detected.md)
- [Daemon issues](daemon-issues.md)
- [Debug mode](debug-mode.md)
