---
title: SketchyBar integration — active AI provider usage
description: Show the active AI provider and quota position in SketchyBar with OpenUsage, including a detail popup and provider switcher.
keywords: [sketchybar ai usage, sketchybar quota, openusage sketchybar, macOS AI status bar]
sidebar_label: SketchyBar integration
---

# SketchyBar integration

OpenUsage can add three small pieces to a [SketchyBar](https://github.com/FelixKratz/SketchyBar) setup:

- an `ai` item showing the active provider and its quota label;
- a detail popup with the selected provider's metric rows;
- an `ai_switcher` popup that pins a provider account or returns to automatic selection.

The data path is deliberately local. The scripts call `openusage active` over
the telemetry daemon's Unix socket, and the CLI gives up after two seconds so
SketchyBar never waits for a network request. If the daemon is down, the bar
uses the CLI's degraded local detector and paints the last successful value as
stale after ten minutes.

## Requirements

- macOS with SketchyBar installed and running;
- `openusage` on `PATH`, or a binary path passed with `--binary`;
- `jq` on `PATH` for the generated shell scripts;
- the OpenUsage telemetry daemon if you want event-backed active-provider selection.

Start the daemon if it is not already installed:

```bash
openusage telemetry daemon install
openusage telemetry daemon status
```

## Install

The installer writes generated scripts to
`~/.local/share/openusage/sketchybar/` and inserts one sentinel block into
`~/.config/sketchybar/sketchybarrc`:

```bash
openusage sketchybar install --write
sketchybar --reload
```

Re-running the command replaces only the OpenUsage block and creates a
`sketchybarrc.bak` backup when the config already exists. It never writes into
`~/.config/sketchybar/plugins/`; that directory is often a set of symlinks into
a dotfiles repository, and OpenUsage has no business rummaging around in it.

Use an explicit path when your config lives elsewhere:

```bash
openusage sketchybar install --write \
  --config ~/dotfiles/sketchybar/sketchybarrc \
  --binary ~/bin/openusage
```

Without `--write`, the command prints the complete managed block so you can
review it or paste it into a config by hand:

```bash
openusage sketchybar
```

## Choose click or hover

:::caution The default changed
Earlier versions always opened the usage popup on **hover**, with no way to
configure it. Both items now default to **click**, so reinstalling the managed
block changes how the usage popup behaves. Set `usage_trigger` to `"hover"` to
keep the old behavior.
:::

The OpenUsage items read their interaction preferences from
`~/.config/openusage/settings.json`. The default is click-to-toggle:

```json
{
  "sketchybar": {
    "usage_trigger": "click",
    "switcher_trigger": "click"
  }
}
```

Set either value to `hover` if that item should open on pointer entry. The two
items can use different gestures, and they close differently on purpose:

- The **usage popup** is read-only, so it closes as soon as the pointer leaves
  the item.
- The **provider picker** is a menu you have to click, so it stays open until
  the pointer leaves the bar entirely. Closing it on item exit would dismiss it
  while you were still reaching for a row.

After editing the file, regenerate the managed block and reload the bar:

```bash
openusage sketchybar install --write
sketchybar --reload
```

Editing `settings.json` does not rewrite the already-generated scripts, so
`openusage sketchybar doctor` compares the configured gesture against the one
baked into each installed script and warns when they have drifted apart. Both
gestures can also be set per-invocation:

```bash
openusage sketchybar install --write --usage-trigger hover --switcher-trigger click
```

### Closing popups

When either item needs to dismiss a popup it first looks for a close helper,
falling back to closing its own two popups directly. The helper is resolved in
this order:

1. `$OPENUSAGE_SKETCHYBAR_CLOSE_SCRIPT`, if set and executable.
2. `$CONFIG_DIR/plugins/popup_close.sh`, if executable — `CONFIG_DIR` is
   exported by SketchyBar itself.
3. `~/.config/sketchybar/plugins/popup_close.sh`, if executable.

The `popup_close.sh` convention comes from the common SketchyBar starter
configs, where it closes every popup on the bar. If you already have one,
OpenUsage will use it, which is usually what you want — picking an OpenUsage
item should dismiss your other popups too. Point
`OPENUSAGE_SKETCHYBAR_CLOSE_SCRIPT` somewhere else, or at a no-op script, if
you would rather OpenUsage did not call it.

## Full managed snippet

Run the installer once so the three scripts exist, then this is the complete
block the generated config contains. The sentinel comments are intentional;
leave them in place so future installs can replace the block safely.

```bash
# >>> openusage sketchybar >>> (managed; do not edit between sentinels)
# Generated scripts live outside ~/.config/sketchybar/plugins.
OPENUSAGE_SKETCHYBAR_DIR='$HOME/.local/share/openusage/sketchybar'
OPENUSAGE_BIN='openusage'
OPENUSAGE_SKETCHYBAR_CACHE_DIR="${OPENUSAGE_SKETCHYBAR_CACHE_DIR:-$HOME/.cache/openusage/sketchybar}"
OPENUSAGE_SKETCHYBAR_ICON='󰚩'
OPENUSAGE_SKETCHYBAR_GOOD_COLOR='0xffa6da95'
OPENUSAGE_SKETCHYBAR_WARN_COLOR='0xffeed49f'
OPENUSAGE_SKETCHYBAR_BAD_COLOR='0xffed8796'
OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR='0xffcad3f5'
OPENUSAGE_SKETCHYBAR_TEXT_COLOR='0xffcad3f5'
OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER='click'
OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER='click'
export OPENUSAGE_SKETCHYBAR_DIR OPENUSAGE_BIN OPENUSAGE_SKETCHYBAR_CACHE_DIR OPENUSAGE_SKETCHYBAR_USAGE_TRIGGER OPENUSAGE_SKETCHYBAR_SWITCHER_TRIGGER
export OPENUSAGE_SKETCHYBAR_ICON OPENUSAGE_SKETCHYBAR_GOOD_COLOR OPENUSAGE_SKETCHYBAR_WARN_COLOR OPENUSAGE_SKETCHYBAR_BAD_COLOR OPENUSAGE_SKETCHYBAR_UNKNOWN_COLOR OPENUSAGE_SKETCHYBAR_TEXT_COLOR
sketchybar --add item 'ai' 'right' >/dev/null 2>&1 || true
sketchybar --subscribe 'ai' mouse.clicked
sketchybar --set 'ai' update_freq=60 padding_left=10 popup.drawing=off popup.background.color='0xff1e2030' popup.background.border_color='0xff494d64' popup.background.border_width=2 popup.background.corner_radius=6 popup.align=right popup.y_offset=2 script="$OPENUSAGE_SKETCHYBAR_DIR/ai-usage.sh"
sketchybar --add item 'ai_switcher' 'right' >/dev/null 2>&1 || true
sketchybar --subscribe 'ai_switcher' mouse.clicked
sketchybar --set 'ai_switcher' padding_left=2 padding_right=2 icon='⇄' icon.font="Hack Nerd Font:Regular:13.0" icon.color='0xffcad3f5' label.drawing=off popup.drawing=off popup.background.color='0xff1e2030' popup.background.border_color='0xff494d64' popup.background.border_width=2 popup.background.corner_radius=6 popup.horizontal=on popup.align=right popup.y_offset=2 script="$OPENUSAGE_SKETCHYBAR_DIR/provider-select.sh"
sketchybar --update
# <<< openusage sketchybar <<<
```

Both items use deliberate click behavior. The `ai` item opens its detail popup
on **click** (`mouse.clicked`) and closes it on a second click; the
`ai_switcher` item opens the provider picker on the same event and also closes
it on a second click. Hovering over either item is inert.

The switcher script uses the stable `provider:account` key from
`openusage active list --json`, so account labels and display text are never
parsed back into selection state.

:::note Upgrading from a hover-driven switcher

Earlier versions subscribed `ai_switcher` to `mouse.entered`. The picker now
ignores anything other than `mouse.clicked`, so an old managed block leaves the
switcher unresponsive. Re-run the installer to replace it:

```bash
openusage sketchybar install --write
sketchybar --reload
```

:::

### Tuning the detail popup

The popup renders only the rows the CLI marks as primary, capped so it stays
glanceable rather than becoming a dashboard.

| Variable | Default | Effect |
| --- | --- | --- |
| `OPENUSAGE_SKETCHYBAR_MAX_ROWS` | `8` | Maximum metric rows in the detail popup, excluding the provider and message header rows. Non-numeric values fall back to the default. |

## Presets

The default preset is `catppuccin-macchiato`, matching the reference
configuration's right-side items and palette. List or inspect presets with:

```bash
openusage sketchybar presets
openusage sketchybar presets --show catppuccin-macchiato
openusage sketchybar install --write --preset catppuccin-macchiato
```

Presets control the item names, position, refresh interval, icon, popup shape,
and severity colors. The quota thresholds and selection rules remain in
OpenUsage's shared active-provider core.

## Inspect and control the data

These commands are useful when the bar looks wrong:

```bash
openusage active --json
openusage active detail --json
openusage active list --json
openusage active --explain
openusage active pin codex:default
openusage active unpin
```

The `active` command prefers a live pin, then the newest telemetry event. With
no telemetry events it uses configured provider priority. A pin releases when
another provider records a newer user-activity event.

## Doctor and uninstall

```bash
openusage sketchybar doctor
openusage sketchybar uninstall
sketchybar --reload
```

`doctor` reports whether the managed block is present, whether the three
generated scripts exist and are executable, whether the `openusage` and
`sketchybar` binaries resolve, and whether the installed click/hover gestures
still match `settings.json`:

```
[ OK ] trigger: usage=click in ai-usage.sh
[WARN] trigger: provider-select.sh is installed click but configured hover — run `openusage sketchybar install --write` and reload
```

Uninstall removes the sentinel block and leaves the neutral generated scripts
in place. That is deliberate: it makes rollback and a later reinstall safe,
without deleting files a user may have inspected or customized.

## Troubleshooting

### The item says “AI unavailable”

Check the executable and daemon first:

```bash
openusage sketchybar doctor
openusage telemetry daemon status
openusage active --json
```

If `active --json` reports `source: "local"`, the daemon was unreachable and
OpenUsage is using its degraded local detector. That path cannot identify
API-key-only providers; start the daemon and install the relevant hook/plugin
integration for event-backed selection.

### The popup is empty

Install `jq`, then reload SketchyBar. The popup and switcher scripts are plain
Bash plus `jq`; they do not require Python or a package manager.

```bash
brew install jq
sketchybar --reload
```

### The config is symlinked into dotfiles

That is supported. The installer follows the config symlink and edits its
target while preserving the symlink itself. It still writes generated scripts
only under OpenUsage's neutral data directory.
