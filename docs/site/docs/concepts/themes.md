---
title: Themes
description: 18 bundled themes, how to cycle them, and where external theme files live.
---

OpenUsage ships with 18 built-in themes and supports user-supplied theme files that can override or extend the bundled set.

## Cycling themes

Press `t` in the dashboard to advance. The selection persists to `settings.json`.

## Bundled themes

Deep Space (default), Ayu Dark, Catppuccin Mocha, Dracula, Everforest, Grayscale, Gruvbox, Kanagawa, Midnight Iris, Monokai, Neon Dusk, Nightfox, Nord, One Dark, Rose Pine, Solarized Dark, Synthwave 84, Tokyo Night.

## External themes

Drop a JSON file with the same shape as a built-in theme into:

- `~/.config/openusage/themes/*.json` (macOS / Linux)
- `%APPDATA%\openusage\themes\*.json` (Windows)
- Any extra directory in `OPENUSAGE_THEME_DIR` (`:`-separated on Unix, `;` on Windows)

External files with the same `name` as a built-in theme override the built-in. Invalid files are silently skipped.

## Where to read next

- [Customization · Themes](/customization/themes/) — full color-token reference and structure of a theme JSON file.
- [Customization · External themes](/customization/external-themes/) — building, sharing, and distributing custom themes.
