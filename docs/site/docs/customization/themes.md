---
title: Themes
description: All 18 bundled OpenUsage themes and how to switch between them from the TUI or settings.
---

# Themes

OpenUsage ships with 18 bundled color themes. You can cycle them live, pin one in settings, or load your own — see [External themes](./external-themes.md) for that.

## Bundled themes

| Theme | Notes |
|---|---|
| Gruvbox | Default. Retro warm contrast. |
| Deep Space | Cool blues over a near-black base (built-in, hardcoded; not a JSON file). |
| Ayu Dark | Warm orange accents on slate. |
| Catppuccin Mocha | The popular pastel-on-dark palette. |
| Dracula | Classic vivid purple/cyan/pink. |
| Everforest | Muted green forest tones. |
| Grayscale | Pure achromatic — useful for screenshots and accessibility tests. |
| Kanagawa | Soft Japanese woodblock palette. |
| Midnight Iris | Deep blue-purple with iris accent. |
| Monokai | Bright magenta and lime on dark. |
| Neon Dusk | High-saturation cyberpunk feel. |
| Nightfox | Cool desaturated blue/teal. |
| Nord | Frost-cool blues and greys. |
| One Dark | Atom-inspired balanced palette. |
| Rose Pine | Muted rose and pine. |
| Solarized Dark | The Solarized base16 dark variant. |
| Synthwave 84 | Magenta and cyan retrowave. |
| Tokyo Night | Deep navy with neon accents. |

## Switching themes

### From the dashboard

Press <kbd>t</kbd> to cycle to the next theme.

The change is immediate and persisted to `~/.config/openusage/settings.json` automatically.

### From the settings modal

1. Open settings with <kbd>,</kbd> (or <kbd>Shift+S</kbd>).
2. Switch to the **Theme** tab — press <kbd>3</kbd>, or use <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd>.
3. Use <kbd>↑</kbd> / <kbd>↓</kbd> to highlight a theme.
4. Press <kbd>Space</kbd> or <kbd>Enter</kbd> to apply.
5. Press <kbd>Esc</kbd> to close.

### From settings.json

```json
{
  "theme": "Tokyo Night"
}
```

The name match is case-sensitive and must equal the theme's `name` field.

## Same-name precedence

If you place an external theme with the same `name` as a built-in, the **external version wins**. This lets you tweak a built-in (say, swap the accent on Tokyo Night) without forking the source.

## Color tokens

Each theme defines 24 named color tokens that map to UI elements: `base`, `mantle`, `surface0..2`, `overlay`, `text`, `subtext`, `dim`, `accent`, `blue`, `sapphire`, `green`, `yellow`, `red`, `peach`, `teal`, `flamingo`, `rosewater`, `lavender`, `sky`, `maroon`, `mauve`. See [External themes](./external-themes.md) for the full schema and how to author your own.

## Related

- [External themes](./external-themes.md) — load custom JSON theme files
- [Keybindings reference](../reference/keybindings.md) — every keymap, including theme cycling
