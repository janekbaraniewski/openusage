---
title: tmux integration — Claude Code, Codex & Cursor usage in your status bar
description: Show Claude Code, Codex, Cursor, Copilot, and OpenRouter usage (cost, quota, burn rate) in your tmux status bar with OpenUsage — an interactive installer, real provider icons, and full theming.
keywords:
  - tmux ai usage
  - claude code tmux
  - claude code tmux status bar
  - tmux status bar ai cost
  - codex cursor tmux usage
  - openusage tmux
---

# tmux integration

> Show your **Claude Code, Codex, Cursor, Copilot, and OpenRouter** usage — cost, quota, burn rate, and the active tool — right in your **tmux status bar**.

OpenUsage ships a `tmux` subcommand that renders a one-line status segment of your current AI tool usage. It is provider-agnostic (it picks the most-recently-used local tool), themed against the same 18-theme palette as the dashboard, and offers four output shapes:

- **preset** named templates (12 built-in)
- **format** custom template strings
- **segment** single named segment
- **json** structured payload

It is meant to live in `status-right` or `status-left` and update every few seconds. The renderer ships its own runtime budget so a slow daemon or a missing key can never freeze tmux.

{/* TODO: asciinema cast */}

## Requirements

- **tmux 3.0+** for the status snippet itself.
- **tmux 3.2+** if you want `--bind-popup` (uses `display-popup`).
- A terminal that supports 256 colors. Truecolor is the default but `--color-mode 256` and `--color-mode ansi` are also supported.

## Quick install

Just run the wizard — it's the one-stop setup:

```bash
openusage tmux install
```

On an interactive terminal this opens a small form to choose your **status bar
position**, **preset** (or **Custom**, which opens a component builder — toggle
the pieces you want, with a live preview that updates as you go), and whether to
use **emoji or real provider icons**. It then does everything for you: writes the
tmux.conf snippet, saves a custom template to `settings.tmux.format` if you built
one, and (if you pick real icons) installs the icon font and configures your
terminal. Reload tmux with the printed `tmux source-file` command and you're
done.

For scripting, pass flags (or run with a non-interactive stdin) to skip the
wizard and apply directly:

```bash
openusage tmux install --write            # write the snippet, no prompts
tmux source-file ~/.config/tmux/tmux.conf
```

`--write` writes a sentinel-bracketed block to your tmux configuration, takes a `.bak` of any prior content, and prints the path you need to reload. Re-running it replaces the block in place. To remove it cleanly:

```bash
openusage tmux uninstall
```

The block sits between two sentinel comments, so nothing outside it is touched:

```
# >>> openusage tmux >>> (managed; do not edit between sentinels)
set -g status-interval 5
set -g status-right-length 200
set -g status-left-length 80
run-shell -b 'seg="$(printf "#%s" "(openusage tmux --preset compact)")"; cur="$(tmux show -gqv status-right)"; case "$cur" in *"$seg"*) exit 0 ;; *) tmux set -g status-right "$seg $cur" ;; esac'
# <<< openusage tmux <<<
```

### Why a `run-shell` line instead of `set -ga status-right`?

The segment is **prepended** to `status-right` so it sits at the inner (left) edge of the right side, next to the center of the bar, ahead of your existing segments (clock, battery, and the like). A plain `set -ga status-right` would *append* it to the far-right edge instead.

tmux has no native "prepend" for an option, so the block reads the current `status-right` and re-sets it with the openusage segment in front. The line is written carefully:

- It runs at config-load time and is **idempotent**: the `case "$cur" in *"$seg"*) exit 0` guard skips the insert when the segment is already present, so repeated `tmux source-file` calls never stack copies.
- It avoids a literal `#(` in the `run-shell` argument. tmux expands `#(...)` inside `run-shell` arguments at *parse* time, which would run `openusage` immediately and freeze its output. The shell rebuilds the leading `#` at runtime via `printf`, and `tmux set` (without `-F`) stores both the openusage segment and your existing `#(...)` segments unexpanded for live rendering.

The install helper looks for your tmux config in this order: `$XDG_CONFIG_HOME/tmux/tmux.conf`, `~/.config/tmux/tmux.conf`, `~/.tmux.conf`. If none exist, it creates `~/.config/tmux/tmux.conf`.

## Manual install

If you prefer to edit your tmux config by hand, splice the `#(openusage …)` command directly into your own `status-right` (or `status-left`) at the spot you want it. Editing by hand, *you* control the position, so there is no need for the `run-shell` prepend the installer uses. For example, first on the right side:

```
set -g status-interval 5
set -g status-right-length 200
set -g status-right "#(openusage tmux --preset compact) | %H:%M | %d-%b"
```

Or append it to the far-right edge instead:

```
set -ga status-right "#(openusage tmux --preset compact)"
```

Then run `tmux source-file ~/.tmux.conf`. Pick any preset name from the gallery below, or replace `--preset compact` with a `--format` template of your own.

## Preset gallery

The bundled presets cover the common shapes. List them with `openusage tmux presets`.

| Name | Glyphs | Sample |
| --- | --- | --- |
| `ascii-safe` | ascii | `[CLAUDE_CODE] $4.21 block:47% burn:$1.20/hr ctx:42%` |
| `burn` | unicode | `🔥 $1.20/hr → $9.40 EOB` |
| `claude-focused` | unicode | `🤖 Opus 4.7 $3.40 block (2h17m) 🔥 $1.20/hr 🧠 42%` |
| `compact` | unicode | `🤖 5h 15% $6.79/today` |
| `cost-only` | ascii | `$4.21` |
| `emoji-rich` | unicode | `🤖 CLAUDE_CODE \| 💰 $4.21 \| 📅 42 req \| 🔥 $1.20/hr \| 🧠 42%` |
| `minimal` | ascii | `claude_code $4.21` |
| `multi-tool` | unicode | `claude_code \| cursor \| codex` |
| `nerdfont` | nerdfont | ` claude_code  $4.21  $1.20` |
| `powerline` | nerdfont | `🤖  $4.21  $1.20/hr ` |
| `themed` | unicode | `🤖 $4.21 $1.20/hr` |
| `verbose` | unicode | `🤖 Opus 4.7 \| 💰 $4.21 today / $3.40 block \| 🔥 $1.20/hr \| 🧠 84k (42%)` |

Inspect a single preset's JSON definition with:

```bash
openusage tmux presets --show claude-focused
```

## Format reference

A `--format` template (or the `format` field in a preset JSON) is a string with three substitution kinds interleaved with literal text:

1. **Variable expansion**: `{name}` or `{name:mod1[:arg1]...}` or chained `{name:mod1:mod2}`
2. **Conditional**: `{?cond:then:else}` where `cond` is a variable name (truthy if non-empty and not `0`/`0.00`)
3. **Theme/tmux passthrough**: `#[fg=$accent]`, `#[fg=colour208]`, `#[bg=$base,bold]`. Inside `#[...]`, `$name` resolves against the active theme. Outside `#[...]`, tokens like `#(...)` and `#{...}` are passed through verbatim so users can compose with native tmux syntax.

Escapes: `\{`, `\}`, `\#`, `\$`, `\\`, `\n`.

User-supplied content is sanitized before substitution: every `#` in a variable value becomes `##` (tmux's own escape) so model names and account ids cannot accidentally trigger tmux substitution.

### Variables

`openusage tmux variables` prints the live list. The schema:

| Kind | Examples | Notes |
| --- | --- | --- |
| Snapshot attribute | `{tool}`, `{provider}`, `{account}`, `{model}` | Always available |
| Built-in segment | `{cost}`, `{burn}`, `{block}`, `{tokens}`, `{context}`, `{daily}`, `{active_tools}` | Composable, see `internal/tmux/segments.go` |
| Semantic alias | `{today_cost}`, `{block_cost}`, `{block_pct}`, `{block_remaining}`, `{block_projection}`, `{burn_rate}`, `{plan_pct}`, `{context_pct}`, `{context_tokens}`, `{requests_today}`, `{today_input_tokens}`, `{today_output_tokens}`, `{tool_color}` | Map to the best per-provider metric automatically |

If a variable resolves to nothing the substitution emits the empty string, so `{?burn_rate:🔥 {burn_rate:money}/hr:}` is the idiomatic way to suppress a segment when there is nothing to show.

### Modifiers

Modifiers chain left-to-right (`{block_pct:bar:10:color}` builds a 10-cell bar, then colors it):

| Modifier | Args | Example | Output |
| --- | --- | --- | --- |
| `:short` | (none) | `{today_cost:short}` | `$4.21` |
| `:long` | (none) | `{today_cost:long}` | `$4.21 today` |
| `:money` | precision (default 2) | `{burn_rate:money:1}` | `$12.3/hr` |
| `:pct` | precision (default 0) | `{block_pct:pct}` | `47%` |
| `:bar` | width (default 8) | `{block_pct:bar:10}` | `▓▓▓▓▓░░░░░` |
| `:color` | (none) | `{block_pct:color}` | applies threshold colors |
| `:brand` | (none) | `{tool:icon:brand}` | tints the value with the active provider's brand color (Claude orange, etc.) |
| `:icon` | (none) | `{tool:icon}` | `🤖` (respects `--glyphs` tier) |
| `:tokens` | (none) | `{today_input_tokens:tokens}` | `1.2M`, `47k` |
| `:duration` | (none) | `{block_remaining:duration}` | `2h17m` |
| `:upper` / `:lower` | (none) | `{tool:upper}` | `CLAUDE` |
| `:trunc` | width | `{model:trunc:12}` | `Opus 4.7 (1` |
| `:pad` | width, side (default r) | `{tool:pad:10:l}` | `    claude` |
| `:default` | value | `{burn_rate:default:-}` | `-` when empty |

### Conditionals

```
{?burn_rate: 🔥 {burn_rate:money}/hr:}
{?block_pct:({block_pct:pct}):}
```

The form is `{?var:then:else}`. The condition variable is truthy when non-empty and not `0` / `0.00`.

## Theming

The renderer uses the same theme palette as the dashboard. The active theme is whichever is configured in `~/.config/openusage/settings.json` (`theme`), but you can override per invocation with `--theme catppuccin-mocha` or via `settings.tmux.theme`.

### Color modes

| Mode | Output | Use when |
| --- | --- | --- |
| `truecolor` (default) | `#[fg=#RRGGBB]` in tmux, `\033[38;2;R;G;Bm` in preview | Modern terminal + tmux with `set -g default-terminal "tmux-256color"` and `set -ga terminal-overrides ",*256col*:Tc"` |
| `256` | `#[fg=colourNNN]` mapped via nearest-neighbor in the xterm 256 palette | Older terminals, screen-via-tmux, conservative defaults |
| `ansi` | The 8 base ANSI colors | Truly minimal environments |
| `none` | Strips every `#[...]` / `\033[...]` token | Scripts, golden tests, `--json` consumers |

### Theme refs

Inside `#[...]` you can use named theme refs that resolve against the active theme:

```
#[fg=$accent]      # the brand accent (orange in default)
#[fg=$peach]
#[fg=$green]
#[bg=$base,bold]
```

The full set is `$base`, `$mantle`, `$surface0`, `$surface1`, `$surface2`, `$overlay`, `$text`, `$subtext`, `$dim`, `$accent`, `$blue`, `$sapphire`, `$green`, `$yellow`, `$red`, `$peach`, `$teal`, `$lavender`, `$sky`, `$maroon`, `$mauve`.

### Threshold coloring

The `:color` modifier applies threshold rules. The built-in defaults map percentages like this:

- 0 to 70: `$green`
- 70 to 90: `$yellow`
- 90+: `$red`

Override per variable via `settings.tmux.color_rules`:

```json
{
  "tmux": {
    "color_rules": {
      "block_pct": {
        "low_at": 0, "medium_at": 60, "high_at": 85,
        "low_color": "$blue", "medium_color": "$peach", "high_color": "$red"
      }
    }
  }
}
```

## Provider icons (custom font)

The `:icon` modifier renders a glyph for the active tool. There are four glyph
tiers, picked best-to-safest:

| Tier | Looks like | Needs |
| --- | --- | --- |
| `customfont` | real provider logos (Claude, Cursor, Codex, …) | the bundled OpenUsage icon font installed |
| `nerdfont` | Nerd Font brand glyphs | a Nerd Font as your terminal font |
| `unicode` (default) | emoji (`🤖`, `▸`, `🦙`) | any modern terminal |
| `ascii` | bracketed labels (`[claude]`) | nothing |

### The easy way: the install wizard

`openusage tmux install` (run it with no flags on a terminal) asks whether you
want real icons and sets up everything for your terminal automatically. That's
all most people need. The rest of this section is the detail behind it.

OpenUsage ships an icon font built from its own SVG provider icons. The icons
are sized to fill the cell height.

### How a terminal actually renders the icons

The glyphs live at Private Use Area codepoints (U+E900–U+E912). A terminal only
shows them if it is told to use the icon font for those codepoints — and how you
do that depends on the terminal:

| Terminal | Mechanism | Automated by |
| --- | --- | --- |
| kitty, Ghostty, WezTerm | per-range font **fallback** — your main font is left untouched | `openusage tmux font setup` (kitty/Ghostty written automatically; WezTerm prints a snippet) |
| iTerm2, Terminal.app, Alacritty | **no** per-range fallback — the glyphs must be *in* the font you use | `openusage tmux font patch` — copies your terminal font, adds the 19 glyphs under a new name, installs it; you select the `… +OpenUsage` family (your original font is never modified) |

The preferred path is per-range fallback (no font modification). Patching is the
only option for terminals without it.

### Commands

```bash
openusage tmux font setup       # auto-configure detected terminals (preferred)
openusage tmux font install     # install the standalone icon font (used by the fallback path)
openusage tmux font status      # install state + whether it is up to date
openusage tmux font uninstall   # remove the standalone font
```

After setup, **restart your terminal and tmux**. The default preset then
auto-upgrades from emoji to real icons; force it with `--glyphs customfont` (or
`settings.tmux.glyphs`). Providers without a bundled glyph fall back to the
unicode emoji automatically.

`font status` compares the installed font against the version embedded in your
`openusage` binary (by content hash), so after you upgrade `openusage` it tells
you when the font is **outdated** and should be reinstalled.

## Power-user recipes

### Multi-segment status bar

```
set -ga status-right "#[fg=#FF6600]openusage:#[default] #(openusage tmux --segment cost) | #(openusage tmux --segment burn) | #(openusage tmux --segment block)"
```

Or stay inside a single template:

```bash
openusage tmux --format '#[fg=$accent]ai:#[default] {cost} | {burn} | {block}'
```

### Conditional segments

```bash
openusage tmux --format '{tool:icon} {?block_pct:({block_pct:pct:color}):}{?burn_rate: 🔥 {burn_rate:money}/hr:}'
```

The `{?...:then:else}` form lets you suppress segments when there is no value, so the line collapses gracefully when no block is active.

### Custom variables

Define reusable fragments in `settings.tmux.variables`:

```json
{
  "tmux": {
    "variables": {
      "cost_block": "#[fg=$peach]{block_cost:money}#[default]",
      "header":     "#[fg=$accent]ai#[default]"
    },
    "format": "{header} {tool:icon} {cost_block} 🔥 {burn_rate:money}/hr"
  }
}
```

Variables are resolved recursively with a depth cap of 4 to keep cycles harmless.

### Per-pane brand coloring with `{tool_color}`

`{tool_color}` emits a tmux conditional that picks the brand color of whichever AI tool's process is in the current pane:

```bash
openusage tmux --format '{tool_color}{tool:icon}#[default] {today_cost:money}'
```

This means each pane in a multi-pane layout can display its own tool's color without you needing to wire a separate status per pane.

### Push alerts with `tmux watch`

`openusage tmux watch` runs in the foreground (or `--background` with a pidfile), polls the daemon at the configured interval, and on threshold cross calls `tmux display-message` to flash a banner:

```bash
openusage tmux watch --alert-mode message
openusage tmux watch --background --alert-mode both   # message + bell
```

Thresholds live in `settings.tmux.alerts`:

```json
{
  "tmux": {
    "alerts": {
      "burn_rate_per_hour": 5.00,
      "block_minutes_remaining": 10,
      "cooldown_minutes": 30,
      "mode": "message"
    }
  }
}
```

The pidfile is at `~/.cache/openusage/tmux-watch.pid`. A second `--background` invocation replaces the first.

### Pin to a specific tool

Force the renderer to always pick one tool:

```bash
openusage tmux --provider claude_code --preset claude-focused
```

Or persist it in `settings.tmux.provider`. Useful when you only care about one tool's state.

### Tune active-tool detection

`--strategy` accepts a comma-separated list of detection strategies, evaluated in order:

| Strategy | What it does |
| --- | --- |
| `recency` | stat each local-file provider; pick the newest mtime within `recency_window` (default 4h) |
| `process` | scan `ps` for a known AI tool process (skipped on Windows) |
| `priority` | first tool in `priority_order` (default `claude_code,cursor,codex,aider,copilot,gemini_cli,ollama`) with any 24h activity |
| `multi` | return every recently-active tool; `{tool}` returns the first, `{active_tools}` lists them all |
| `pinned` | use `settings.tmux.provider` |

Default: `recency,priority`. Examples:

```bash
openusage tmux --strategy process,priority      # process first, fall back to priority
openusage tmux --strategy multi --preset multi-tool
openusage tmux --no-cache                       # skip the detection cache
```

The detected tool is cached for ~15 seconds so the segment stays stable across
status refreshes instead of re-detecting (and visibly flipping) on every tick.
When **not** pinned, detection also skips any tool that has no metric to show —
so a background Ollama touching its files will not hijack the segment and leave
you with an icon and blank numbers. Pin a provider (`--provider` /
`settings.tmux.provider`) when you want one tool's metrics, always.

### Why the value sometimes looks "stale" for a moment

If the snapshot read is degraded (for example the telemetry daemon is briefly
unreachable), the renderer reuses the **last good** status instead of flashing
a blank or zeroed segment. This is an intentional anti-flicker measure; the
cached value is used for at most a few minutes before live (possibly empty)
state shows through.

## Troubleshooting

### `openusage tmux` prints `?`

The renderer ran into an error and gracefully emitted a placeholder so tmux would not block. Look at `tmux show-environment -g` or run `openusage tmux doctor` to inspect the cause. Likely culprits:

- Daemon is configured but not running, and the snapshot fetch timed out within the 800ms budget. Either start it (`openusage telemetry daemon install`) or pass `--source direct` to bypass it.
- No provider is detected. See below.

### "active provider: none detected" in `doctor`

`openusage tmux` only renders when at least one provider is reachable. Run `openusage detect` to verify a provider is configured, and `openusage tmux doctor` to inspect:

- whether tmux itself is on the path
- whether `$TMUX` is set (you are inside a session)
- whether truecolor is advertised (`COLORTERM=truecolor`)
- whether the daemon is running
- whether your tmux config has an openusage block

### Broken colors / escape sequences leaking into the bar

You probably have a mismatch between tmux's `default-terminal` and the terminal you launched it in. The canonical fix:

```
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*256col*:Tc"
```

If that does not work, fall back to `--color-mode 256` or `--color-mode ansi`.

### `#` characters render as garbage

tmux uses `#` for substitution. The renderer escapes user-supplied content automatically, but any literal `#` in your own `--format` string that you do not want tmux to interpret must be doubled: `##`.

### "display-popup not available" (tmux too old)

`--bind-popup` requires tmux 3.2+. Check with `tmux -V` and upgrade, or omit the flag.

### Provider icons show as a box (tofu), as emoji, or render too small

The icon font is correct (`openusage tmux font status` shows it installed); the
issue is almost always your terminal not rendering the installed glyphs. Work
through these:

- **Box / tofu instead of a logo** — your terminal is not using the icon font
  for the icon codepoints (U+E900–E912). On kitty/Ghostty/WezTerm run
  `openusage tmux font setup` (per-range fallback). On iTerm2/Terminal.app run
  `openusage tmux font patch` and select the `… +OpenUsage` font in the profile
  — those terminals have no per-range fallback.
- **Still tofu / wrong size after `font patch`** — macOS caches fonts by family
  name, and the augmented font keeps your font's name, so the old copy is served
  until the cache is cleared. **Fully quit the terminal (Cmd-Q), run
  `atsutil databases -remove`, then relaunch.** A reboot also works. Verify with
  printing the Claude codepoint U+E900 in the terminal — the logo should
  render at full size.
- **Emoji instead of the logo** — the font is not installed, so the renderer
  falls back to the unicode tier. Run `openusage tmux font install` (or the
  `tmux install` wizard) and restart the terminal.
- **Icon looks small** — terminal cells are taller than wide, so a square logo
  that fills the full line height would overflow into the next cell. The
  patched glyph is sized to ~80% of line height as a balance; that is expected.

### Watch alerts not firing

Make sure `tmux` is on the path of the watch process (it shells out), the daemon is running so the watcher can read live data, and your thresholds in `settings.tmux.alerts` are not so high they never trigger. `--alert-mode bell` will fall back to a terminal bell on systems where `display-message` is silenced.

## Reference: `settings.tmux.*` schema

The full TmuxConfig surface in `~/.config/openusage/settings.json`:

| Key | Type | Default | Purpose |
| --- | --- | --- | --- |
| `preset` | string | `compact` | Named preset to render. |
| `format` | string | (empty) | Custom template. Overrides `preset` when set. |
| `provider` | string | (empty) | Pin a provider id. Skips auto-detection. |
| `active_strategy` | string | `recency,priority` | Comma-separated detection strategies. |
| `priority_order` | string[] | `["claude_code","cursor","codex","aider","copilot","gemini_cli","ollama"]` | Order used by the `priority` strategy. |
| `recency_window` | duration string | `4h` | mtime window for the `recency` strategy. |
| `color_mode` | string | `truecolor` | `truecolor`, `256`, `ansi`, or `none`. |
| `glyphs` | string | per-preset | `ascii`, `unicode`, `nerdfont`, or `customfont` (the bundled provider-icon font; see [Provider icons](#provider-icons-custom-font)). |
| `theme` | string | (inherits `theme`) | Theme name. |
| `source` | string | `auto` | Snapshot source: `auto`, `daemon`, `direct`. |
| `interval` | int | 5 | Suggested `status-interval`. |
| `max_runtime_ms` | int | 800 | Self-kill budget so tmux never blocks. |
| `variables` | object | (none) | User-defined template variables. |
| `segments` | object | (none) | User-defined named segments. |
| `color_rules` | object | (defaults) | Per-variable threshold rules. |
| `alerts.burn_rate_per_hour` | number | 0 | Trigger a watch alert above this `$/hr`. |
| `alerts.block_minutes_remaining` | int | 0 | Trigger when the active block drops below this many minutes. |
| `alerts.cooldown_minutes` | int | 30 | Minutes between repeated alerts for the same threshold. |
| `alerts.mode` | string | `message` | `message`, `bell`, `both`, or `none`. |

See the [CLI reference](../reference/cli.md#openusage-tmux) for the matching command-line flags, and the [configuration reference](../reference/configuration.md) for the full settings.json surface.
