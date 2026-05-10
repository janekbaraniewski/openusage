---
title: Keybindings reference
description: Complete keybinding reference for every OpenUsage TUI context.
---

# Keybindings reference

Every key recognized by the TUI, grouped by context. For a high-level overview, see [Customization → Keybindings](../customization/keybindings.md).

## Global

Active everywhere.

| Key | Action |
|---|---|
| <kbd>?</kbd> | Toggle the help overlay |
| <kbd>q</kbd> | Quit |
| <kbd>Ctrl+C</kbd> | Quit |
| <kbd>Tab</kbd> | Next screen (Dashboard ↔ Analytics) |
| <kbd>Shift+Tab</kbd> | Previous screen |
| <kbd>Esc</kbd> | Close overlays / clear filter |

## Navigation

Active in any list-like view.

| Key | Action |
|---|---|
| <kbd>↑</kbd> / <kbd>k</kbd> | Move up |
| <kbd>↓</kbd> / <kbd>j</kbd> | Move down |
| <kbd>←</kbd> / <kbd>h</kbd> | Move left |
| <kbd>→</kbd> / <kbd>l</kbd> | Move right |
| <kbd>Enter</kbd> | Activate / drill in |
| <kbd>Esc</kbd> | Back / cancel |
| <kbd>Backspace</kbd> | Back (alias) |

## Dashboard

| Key | Action |
|---|---|
| <kbd>,</kbd> | Open settings modal |
| <kbd>Shift+S</kbd> | Open settings modal (alias) |
| <kbd>/</kbd> | Enter filter mode |
| <kbd>v</kbd> | Next dashboard view |
| <kbd>V</kbd> | Previous dashboard view |
| <kbd>r</kbd> | Refresh now |
| <kbd>t</kbd> | Cycle theme forward |
| <kbd>w</kbd> | Cycle time window (`1d` → `3d` → `7d` → `30d` → `all`) |
| <kbd>Ctrl+O</kbd> | Expand model breakdown for the focused tile |

Dashboard views cycled with <kbd>v</kbd> / <kbd>V</kbd>:

| Order | View |
|---|---|
| 1 | Grid (default) |
| 2 | Stacked |
| 3 | Tabs |
| 4 | Split |
| 5 | Compare |

A viewport too narrow for the chosen view auto-falls-back to **Stacked**.

## Scroll

Active in any scrollable pane (tile body, detail pane, analytics).

| Key | Action |
|---|---|
| <kbd>PgUp</kbd> | Page up |
| <kbd>PgDn</kbd> | Page down |
| <kbd>Ctrl+U</kbd> | Half page up |
| <kbd>Ctrl+D</kbd> | Half page down |
| <kbd>Home</kbd> / <kbd>g</kbd> | Jump to top |
| <kbd>End</kbd> / <kbd>G</kbd> | Jump to bottom |

## Detail pane

Active when a tile's detail pane is focused.

| Key | Action |
|---|---|
| <kbd>Tab</kbd> | Next section |
| <kbd>Shift+Tab</kbd> | Previous section |
| <kbd>[</kbd> | Previous tab within section |
| <kbd>]</kbd> | Next tab within section |
| <kbd>h</kbd> | Previous section (vim) |
| <kbd>l</kbd> | Next section (vim) |

## Analytics

| Key | Action |
|---|---|
| <kbd>s</kbd> | Cycle sort |
| <kbd>/</kbd> | Filter |

## Filter mode

Active after <kbd>/</kbd> in dashboard or analytics.

| Key | Action |
|---|---|
| Type | Update filter pattern |
| <kbd>Enter</kbd> | Apply and exit filter mode |
| <kbd>Esc</kbd> | Clear filter and exit |
| <kbd>Backspace</kbd> | Edit pattern |

## Settings modal — global

Active in any settings tab.

| Key | Action |
|---|---|
| <kbd>1</kbd>–<kbd>7</kbd> | Jump to tab |
| <kbd>Tab</kbd> / <kbd>]</kbd> / <kbd>→</kbd> | Next tab |
| <kbd>Shift+Tab</kbd> / <kbd>[</kbd> / <kbd>←</kbd> | Previous tab |
| <kbd>Esc</kbd> | Close modal |

Tabs:

| # | Tab |
|---|---|
| 1 | Providers |
| 2 | Widget Sections |
| 3 | Theme |
| 4 | View |
| 5 | API Keys |
| 6 | Telemetry |
| 7 | Integrations |

### Settings → Providers

| Key | Action |
|---|---|
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Toggle provider on/off |
| <kbd>Shift+J</kbd> / <kbd>Shift+K</kbd> | Reorder providers |
| <kbd>Ctrl+↑</kbd> / <kbd>Ctrl+↓</kbd> | Reorder (alias) |
| <kbd>Alt+↑</kbd> / <kbd>Alt+↓</kbd> | Reorder (alias) |

### Settings → Widget Sections

| Key | Action |
|---|---|
| <kbd>&lt;</kbd> | Previous sub-tab (Dashboard Tiles ↔ Detail Widgets) |
| <kbd>&gt;</kbd> | Next sub-tab |
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Toggle section on/off |
| <kbd>Shift+J</kbd> / <kbd>Shift+K</kbd> | Reorder sections |
| <kbd>h</kbd> / <kbd>H</kbd> | Toggle "hide empty" for the current section |

### Settings → Theme

| Key | Action |
|---|---|
| <kbd>↑</kbd> / <kbd>↓</kbd> | Highlight a theme |
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Apply highlighted theme |

### Settings → View

| Key | Action |
|---|---|
| <kbd>↑</kbd> / <kbd>↓</kbd> | Highlight a view |
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Apply highlighted view |

### Settings → API Keys

| Key | Action |
|---|---|
| <kbd>Enter</kbd> | Edit highlighted key |
| <kbd>d</kbd> | Delete highlighted key |
| <kbd>Backspace</kbd> | Delete highlighted key (alias) |

#### API key edit mode

| Key | Action |
|---|---|
| Type | Append to key |
| <kbd>Backspace</kbd> | Delete last character |
| <kbd>Enter</kbd> | Save and exit edit mode |
| <kbd>Esc</kbd> | Discard and exit edit mode |

### Settings → Telemetry

| Key | Action |
|---|---|
| <kbd>w</kbd> | Cycle time window |
| <kbd>m</kbd> | Open the provider link picker for the current source |
| <kbd>x</kbd> | Clear the link override on the current source |
| <kbd>Enter</kbd> | Activate the highlighted entry |

#### Provider link picker

| Key | Action |
|---|---|
| <kbd>↑</kbd> / <kbd>↓</kbd> | Highlight a destination provider |
| <kbd>Enter</kbd> | Apply link |
| <kbd>Esc</kbd> | Cancel |

### Settings → Integrations

| Key | Action |
|---|---|
| <kbd>Space</kbd> / <kbd>Enter</kbd> | Install / reinstall the highlighted integration |
| <kbd>r</kbd> | Refresh the integrations list |

## Mouse

| Action | Effect |
|---|---|
| Wheel up / down | Scroll. Step size scales with terminal height (minimum 3 lines per tick). |

Click-to-focus, drag-to-select, and other mouse interactions are intentionally not bound — the TUI is keyboard-first.

## See also

- [Customization → Keybindings](../customization/keybindings.md) — orientation overview
- [TUI screens](../concepts/architecture.md) — how screens compose into the binding contexts
