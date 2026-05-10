---
title: Widget sections
description: Toggle, reorder, and hide-when-empty for dashboard tiles and detail widgets.
---

# Widget sections

Each provider exposes a set of **widgets** — gauges, breakdowns, recent-events lists, charts. OpenUsage groups these into named **sections** that can be enabled, disabled, and reordered globally so the dashboard shows only what you care about.

## Where to configure

Open the settings modal (<kbd>,</kbd> or <kbd>Shift+S</kbd>) and switch to the **Widget Sections** tab — press <kbd>2</kbd>, or use <kbd>Tab</kbd> to walk to it.

The tab has two sub-tabs:

- **Dashboard Tiles** — sections that render in the tile grid on the main screen.
- **Detail Widgets** — sections that render in the right-hand detail pane when a tile is focused.

Press <kbd>&lt;</kbd> / <kbd>&gt;</kbd> to switch between sub-tabs.

## Operations

| Action | Key |
|---|---|
| Toggle current section on/off | <kbd>Space</kbd> or <kbd>Enter</kbd> |
| Move section up | <kbd>Shift+K</kbd> (also <kbd>Ctrl+↑</kbd>, <kbd>Alt+↑</kbd>) |
| Move section down | <kbd>Shift+J</kbd> (also <kbd>Ctrl+↓</kbd>, <kbd>Alt+↓</kbd>) |
| Toggle "hide empty" for the current section | <kbd>h</kbd> or <kbd>H</kbd> |
| Switch sub-tab | <kbd>&lt;</kbd> / <kbd>&gt;</kbd> |
| Switch settings tab | <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> or <kbd>1</kbd>–<kbd>7</kbd> |
| Close modal | <kbd>Esc</kbd> |

Changes are saved to `~/.config/openusage/settings.json` immediately.

## Hide empty

Many sections only have data sometimes — e.g. an OAuth provider's "weekly limits" panel is empty until at least one block has elapsed. Toggling **hide empty** on a section makes it disappear when it has no rows, then reappear once there's something to show.

This is independent of the on/off toggle: a section can be enabled but hidden when empty.

## Common section IDs

The defaults installed for a fresh config are listed in the example settings file:

```json
{
  "dashboard": {
    "widget_sections": [
      { "id": "top_usage_progress", "enabled": true },
      { "id": "model_burn",         "enabled": true },
      { "id": "client_burn",        "enabled": true },
      { "id": "other_data",         "enabled": true },
      { "id": "daily_usage",        "enabled": false }
    ]
  }
}
```

Each provider contributes section IDs from its `Spec()`. The Widget Sections UI gives you the human label and provider scope for each one.

## Example: pin model breakdown above limits

If you mostly care about which model is burning credit:

1. Open settings → Widget Sections → Dashboard Tiles.
2. Highlight `model_burn`, press <kbd>Shift+K</kbd> until it's at the top.
3. Highlight `top_usage_progress`, press <kbd>Space</kbd> to disable it (or move it down).
4. Press <kbd>Esc</kbd>.

The dashboard re-renders with the new ordering immediately.

## Editing settings.json directly

```json
{
  "dashboard": {
    "widget_sections": [
      { "id": "model_burn",         "enabled": true,  "hide_empty": false },
      { "id": "client_burn",        "enabled": true,  "hide_empty": true  },
      { "id": "top_usage_progress", "enabled": false }
    ]
  }
}
```

Order in the array determines render order. Sections you don't list use their default ordering and `enabled=true`.

## Per-provider widget visibility

The Widget Sections tab is **global** — toggling `model_burn` affects every provider that contributes to it. To hide a specific provider entirely, use the **Providers** tab instead (settings tab <kbd>1</kbd>) and toggle individual accounts off.

## Related

- [Themes](./themes.md) — change appearance independently of layout
- [Keybindings reference](../reference/keybindings.md) — full settings keymap
- [Configuration reference](../reference/configuration.md) — `dashboard.widget_sections` schema
