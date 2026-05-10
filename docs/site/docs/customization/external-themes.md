---
title: External themes
description: Author custom OpenUsage themes as JSON files, where to put them, and the full color-token schema.
---

# External themes

OpenUsage loads custom themes from JSON files alongside the bundled set. Drop a file in the right directory, restart the TUI (or press <kbd>r</kbd>), and your theme appears in the Theme tab.

## File schema

Every theme file is a single JSON object with **24 color fields, a name, and an optional icon**. All 24 color fields and `name` are required; `icon` is optional. Invalid or incomplete files are silently skipped at load time.

| Field | Type | Purpose |
|---|---|---|
| `name` | string | Display name. Must be unique within the merged set; same-name external themes override built-ins. |
| `icon` | string | Optional emoji or single grapheme shown next to the name. |
| `base` | hex color | Page background (the darkest layer). |
| `mantle` | hex color | One step above `base` — header strips. |
| `surface0` | hex color | Tile / card background. |
| `surface1` | hex color | Slight elevation above `surface0`. |
| `surface2` | hex color | Highlights and selected rows. |
| `overlay` | hex color | Modal backdrops and tooltips. |
| `text` | hex color | Primary foreground. |
| `subtext` | hex color | Secondary foreground (labels, helper text). |
| `dim` | hex color | Tertiary foreground (timestamps, hints). |
| `accent` | hex color | Brand accent — used for active selections and highlights. |
| `blue` | hex color | Status / chart color. |
| `sapphire` | hex color | Status / chart color. |
| `green` | hex color | Healthy gauge fill. |
| `yellow` | hex color | Warning gauge fill. |
| `red` | hex color | Critical gauge fill, error states. |
| `peach` | hex color | Status / chart color. |
| `teal` | hex color | Status / chart color. |
| `flamingo` | hex color | Status / chart color. |
| `rosewater` | hex color | Status / chart color. |
| `lavender` | hex color | Status / chart color. |
| `sky` | hex color | Status / chart color. |
| `maroon` | hex color | Status / chart color. |

Hex values may be 3-digit (`#abc`) or 6-digit (`#aabbcc`). Alpha is not supported.

## Where to put the file

Two locations are scanned, in order:

1. `<config_dir>/themes/*.json` — typically `~/.config/openusage/themes/` (Linux/macOS) or `%APPDATA%\openusage\themes\` (Windows).
2. Each path in the `OPENUSAGE_THEME_DIR` environment variable, separated by `:` on Unix or `;` on Windows.

Built-in themes load first, then external paths in the order above. A later file with the same `name` replaces an earlier one.

```bash
export OPENUSAGE_THEME_DIR=~/dotfiles/openusage-themes:~/work/themes
```

## Authoring workflow

1. Copy a built-in close to what you want as a starting point.
2. Save your edits as `~/.config/openusage/themes/my-theme.json`.
3. Press <kbd>r</kbd> in the dashboard, or restart `openusage`.
4. Open the Theme tab (<kbd>,</kbd> then <kbd>3</kbd>) and select your theme.

Source examples in the repo:

- [`configs/themes/grayscale.json`](https://github.com/janekbaraniewski/openusage/blob/main/configs/themes/grayscale.json)
- [`configs/themes/tokyo-night.json`](https://github.com/janekbaraniewski/openusage/blob/main/configs/themes/tokyo-night.json)
- [`configs/themes/dracula.json`](https://github.com/janekbaraniewski/openusage/blob/main/configs/themes/dracula.json)

## Complete example

A minimal high-contrast theme suitable for accessibility testing:

```json
{
  "name": "Hi-Contrast",
  "icon": "◆",
  "base": "#000000",
  "mantle": "#0A0A0A",
  "surface0": "#181818",
  "surface1": "#2A2A2A",
  "surface2": "#3E3E3E",
  "overlay": "#2A2A2A",
  "text": "#FFFFFF",
  "subtext": "#E0E0E0",
  "dim": "#A0A0A0",
  "accent": "#FFCC00",
  "blue": "#3B82F6",
  "sapphire": "#0EA5E9",
  "green": "#22C55E",
  "yellow": "#EAB308",
  "red": "#EF4444",
  "peach": "#FB923C",
  "teal": "#14B8A6",
  "flamingo": "#F472B6",
  "rosewater": "#FECACA",
  "lavender": "#C4B5FD",
  "sky": "#7DD3FC",
  "maroon": "#9F1239"
}
```

## Tips

:::tip Live iteration
With `OPENUSAGE_DEBUG=1`, the theme loader prints which files were considered and which were skipped — useful when a file isn't showing up.
:::

:::warning Strict parsing
Unknown extra keys are tolerated, but missing required fields cause silent skip. If your theme doesn't appear, run with `OPENUSAGE_DEBUG=1` and look for `theme: skipping <path>: <reason>`.
:::

:::note No reload watcher
The TUI loads themes at startup. After editing a JSON file, press <kbd>r</kbd> to refresh, or restart the binary.
:::

## Override a bundled theme

To customize a built-in without forking the source, save a file with the same `name`:

```json
{
  "name": "Tokyo Night",
  "icon": "🗼",
  "accent": "#FF6600",
  "...": "rest of the fields"
}
```

The bundled "Tokyo Night" disappears and yours takes its place in the Theme tab.

## Related

- [Bundled themes list](./themes.md)
- [Configuration reference](../reference/configuration.md) — pinning a theme in `settings.json`
- [Environment variables](../reference/env-vars.md) — `OPENUSAGE_THEME_DIR`
