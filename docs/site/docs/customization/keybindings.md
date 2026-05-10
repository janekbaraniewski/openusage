---
title: Keybindings
description: Overview of how OpenUsage keybindings are organized by context, with a pointer to the full reference.
---

# Keybindings

OpenUsage's keymap is grouped by **context** — global keys are always live, while screen-specific keys only fire when that screen is focused. This page is the high-level orientation. For an exhaustive table of every key, see the [keybindings reference](../reference/keybindings.md).

## Contexts

| Context | When active |
|---|---|
| **Global** | Everywhere. Help, quit, screen switch. |
| **Dashboard** | Default screen. Tiles, filter, refresh, theme, time window. |
| **Scroll** | Inside any scrollable pane. PgUp/PgDn, half-page, top/bottom. |
| **Detail** | Right-hand detail pane after focusing a tile. Tabbed sections. |
| **Analytics** | Optional Analytics screen. Sort, filter. |
| **Filter mode** | After pressing <kbd>/</kbd>. Type to filter, Enter to apply. |
| **Settings** | Modal opened with <kbd>,</kbd>. Per-tab keymaps below. |
| **API key edit mode** | Inside the API Keys settings tab. Type to overwrite. |
| **Provider link picker** | Inside the Telemetry settings tab. Pick display provider. |

## Global highlights

| Key | Action |
|---|---|
| <kbd>?</kbd> | Show help overlay |
| <kbd>q</kbd> or <kbd>Ctrl+C</kbd> | Quit |
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Cycle screens (Dashboard ↔ Analytics) |
| <kbd>Esc</kbd> | Pop the current overlay or filter |

## Dashboard highlights

| Key | Action |
|---|---|
| <kbd>,</kbd> or <kbd>Shift+S</kbd> | Open settings modal |
| <kbd>/</kbd> | Filter tiles |
| <kbd>v</kbd> / <kbd>V</kbd> | Cycle dashboard view (Grid → Stacked → Tabs → Split → Compare) |
| <kbd>r</kbd> | Refresh now |
| <kbd>t</kbd> | Cycle theme |
| <kbd>w</kbd> | Cycle time window (`1d`, `3d`, `7d`, `30d`, `all`) |
| <kbd>Ctrl+O</kbd> | Expand model breakdown |

## Detail pane highlights

| Key | Action |
|---|---|
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Section navigation |
| <kbd>[</kbd> / <kbd>]</kbd> | Tab navigation within a section |
| <kbd>h</kbd> / <kbd>l</kbd> | Section navigation (vim-style) |

## Settings modal highlights

| Key | Action |
|---|---|
| <kbd>1</kbd>–<kbd>7</kbd> | Jump to tab (Providers, Widget Sections, Theme, View, API Keys, Telemetry, Integrations) |
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Cycle tabs |
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Activate selection |
| <kbd>Shift+J</kbd> / <kbd>Shift+K</kbd> | Reorder rows (where applicable) |
| <kbd>Esc</kbd> | Close modal |

## Filter mode

| Key | Action |
|---|---|
| Type | Update filter pattern |
| <kbd>Enter</kbd> | Apply filter |
| <kbd>Esc</kbd> | Clear filter |
| <kbd>Backspace</kbd> | Edit pattern |

## Mouse

Mouse support is intentionally minimal: **wheel scroll only**, 3 lines per tick. Click-to-focus and drag are not supported.

## Full reference

See [Keybindings reference](../reference/keybindings.md) for the complete list including:

- Per-tab key behavior in the settings modal
- Reorder bindings (`Ctrl+↑/↓`, `Alt+↑/↓` aliases)
- Scroll context (PgUp/PgDn, Ctrl+U/Ctrl+D, Home/End, g/G)
- Telemetry tab keys (window, link picker, clear)
- Integrations tab keys (install, refresh)
