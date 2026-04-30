# Telemetry Provider Mapping UX Design

Date: 2026-04-30
Status: Proposed
Author: Jan Baraniewski

Driven by GitHub issue #80: a user installed openusage with the OpenCode plugin and saw five "Unmapped" providers (`github-copilot`, `google`, `moonshot`, `openai`, `openrouter`) with no in-product way to fix them. The fix instructions told them to hand-edit `settings.json`. The Dashboard appeared empty (only `claude-code` was visible) even though their settings showed providers were "detected".

## 1. Problem Statement

The default telemetry-to-account mapping table contains a single entry (`anthropic→claude_code`), and the only way to add more is to hand-edit `settings.json` — leaving users with the OpenCode plugin installed staring at a vague "⚠ N unmapped" warning that conflates three distinct underlying problems.

## 2. Goals

1. Eliminate the "Unmapped" warning for OpenCode telemetry that *should* attribute to an existing account, by shipping default links for the renames OpenCode uses (`google→gemini_api`, `github-copilot→copilot`).
2. Categorize the remaining unmapped diagnostics so the user can tell the difference between "no account configured" vs "name mismatch I can fix with a link" vs "account exists but can't be reached".
3. Provide an interactive remap inside the Settings → 6 TELEM tab so users never have to edit JSON to fix a name mismatch.
4. Keep all existing behavior intact: user-defined `provider_links` still override defaults; current diagnostics keys still populate.

## 3. Non-Goals

1. Adding Moonshot or Perplexity providers / env-var detection — issue #79, separate work, requires test accounts.
2. Auto-creating synthetic provider tiles from telemetry. `docs/TELEMETRY_INTEGRATIONS.md` explicitly forbids that.
3. Changing OpenCode plugin emission to use openusage's internal IDs. We map at read time, not ingestion time, to keep the plugin source-of-truth honest.
4. Improving daemon environment-variable detection (e.g., the launchd-vs-shell case where `OPENROUTER_API_KEY` is set in the user's shell but not in the daemon's environment). This is a separate, larger problem.
5. Adding per-tile "unmapped" badges. The header pill + Settings tab is sufficient.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | No type changes. New diagnostic keys are just strings. |
| providers | none | No provider implementations change. |
| TUI | minor | New keybindings on the TELEM tab, expanded body rendering with categories and an interactive picker. |
| config | minor | `DefaultProviderLinks` gains entries; new `SaveProviderLinks` save function. |
| detect | none | |
| daemon | none | The read model already plumbs `Telemetry.ProviderLinks` end-to-end. |
| telemetry | minor | `annotateUnmappedTelemetryProviders` emits richer diagnostics (a category per source ID + a suggested target if any). |
| CLI | none | |

### Existing Design Doc Overlap

- `docs/TELEMETRY_INTEGRATIONS.md` — describes the architecture; states "OpenUsage does not auto-create synthetic providers from telemetry. Unmapped telemetry provider IDs are flagged for explicit user action." This design respects that line: we still flag them, we just make the flagging less hostile and the action shorter.
- `docs/COPILOT_TELEMETRY_INTEGRATION_DESIGN.md` — relevant context for the `github-copilot→copilot` default; the rename is exactly what that design is about.

No design doc supersedes anything.

## 5. Detailed Design

### 5.1 Expand `DefaultProviderLinks`

`internal/config/config.go:148`:

```go
func DefaultProviderLinks() map[string]string {
    return map[string]string{
        "anthropic":      "claude_code",
        "google":         "gemini_api",
        "github-copilot": "copilot",
    }
}
```

Only the renames are added. We deliberately do NOT add identity links (`openai→openai`, `openrouter→openrouter`, etc.) because the matcher already does direct-id matching (`read_model.go:315`) and adding identities would clutter `settings.json` exports without changing behavior.

User-defined links continue to win — `normalizeTelemetryConfig` (`config.go:302`) seeds from `DefaultProviderLinks()` and overlays user values.

### 5.2 Categorize Unmapped Diagnostics

Today `annotateUnmappedTelemetryProviders` emits a single `telemetry_unmapped_providers` CSV with two formats per token: bare `providerID` (no link configured) or `providerID->mappedTarget` (link configured but its target isn't a configured account, `read_model.go:322`). The arrow format is reachable in production but is awkwardly rendered as-is in both the TUI Settings tab and the header pill, and no test asserts it.

Move to a flat primary key + a structured-but-still-stringly meta diagnostic. The TUI is the only consumer, so flat-key encoding stays simple and avoids JSON in diagnostics. Drop the arrow format from the primary key (`telemetry_unmapped_providers` becomes purely bare IDs) and encode link/category info in the new `telemetry_unmapped_meta` key. Existing tests continue to pass — they only assert bare-ID formats.

Per source provider id, decide a category:
- `unconfigured` — no configured account matches; suggest the closest configured account (Levenshtein or simple substring) if a reasonable suggestion exists, otherwise no suggestion.
- `name_mismatch` — `provider_links` would map this to a target that *also* isn't configured. (Today this is signalled but lumped in.)
- `mapped_target_missing` — same as name_mismatch but explicit when the link has been set by the user.

Encoding:

```
telemetry_unmapped_providers       = "github-copilot,google,moonshot,openai,openrouter"
telemetry_unmapped_meta            = "github-copilot=unconfigured:copilot,google=unconfigured:gemini_api,moonshot=unconfigured,openai=unconfigured,openrouter=unconfigured"
telemetry_provider_link_hint       = (existing, unchanged)
```

`telemetry_unmapped_meta` is `<source>=<category>[:<suggestion>]`, comma-separated. Empty suggestion means none. Categories use snake_case for parser stability.

The TUI can derive categories from this map and render them. The existing `telemetry_unmapped_providers` key stays for backward-compatibility with snapshot tests and any external consumers.

Suggestion algorithm (deliberately simple):
1. Normalize source id (lowercase, strip non-alnum into `-`).
2. For each configured provider id, compute the same normalized form.
3. If any configured id is a substring of the source or vice versa, that's a candidate.
4. Otherwise no suggestion.

Examples on the user's set with configured = `claude_code, copilot, gemini_api, openrouter`:
- `github-copilot` → suggestion `copilot` (substring match)
- `google` → no substring match against any configured id (`gemini_api` doesn't contain "google") → no suggestion. Default link still attributes `google→gemini_api` so it doesn't appear here at all.
- `openai` → no suggestion (no configured `openai`)
- `openrouter` → matches configured `openrouter` exactly; not unmapped if account exists.

We accept that suggestions are weak. The interactive picker (5.3) is the safety net.

### 5.3 Interactive Remap in Settings → 6 TELEM

Today the TELEM tab is a static list. Extend it to:

```
[Time Window]            (existing)
  ▸ Today
    3 Days
    ...

[Unmapped telemetry providers]   (new keybinding hint: m to map, x to clear)
  ▸ github-copilot   suggested: copilot     [name match, unconfigured]
    google                                  [mapped → gemini_api]
    moonshot                                [no account configured]
    openai                                  [no account configured]
    openrouter                              [no account configured]
```

Two modes on this tab:
- **Default**: cursor moves through Time Window options OR unmapped providers (single combined cursor index).
- **Picker mode**: pressing `m` on an unmapped row enters a sub-picker showing configured account provider IDs (sorted). Up/down to select, Enter to apply, Esc to cancel, `x` to clear an existing user link for this source.

State changes are routed through a new `Services.SaveProviderLink(source, target string) error` and `Services.DeleteProviderLink(source string) error`. Implementations live in `internal/dashboardapp/service.go` and call new `config.SaveProviderLink` / `config.DeleteProviderLink` (read-modify-write, mirrors `SaveTimeWindow`).

Unified cursor model: `m.settings.cursor` covers a flat list of "rows" (time windows + each unmapped provider). The renderer translates cursor index → row type. Keeping a single cursor avoids restructuring `settings_modal_input.go`. Picker mode uses a separate `m.settings.providerLinkPicker` sub-state struct: `{active bool, source string, choices []string, cursor int}`.

After a save, the next read-model refresh recomputes diagnostics; the row either disappears (now mapped) or gets re-categorized.

### 5.4 Header pill messaging

`internal/tui/model_view.go:106` currently says `"detected additional providers, check settings"`. Tighten to:

- If all unmapped have `unconfigured` and no suggestion: keep a softer phrasing — `"N telemetry sources without an account"`.
- If any have a suggestion or are `name_mismatch`: `"N telemetry sources need mapping"`.

Both states still render the `⚠ N unmapped` count chip on the left.

### 5.N Backward Compatibility

- **Defaults table grows**: `normalizeTelemetryConfig` already merges user values on top of defaults, so adding entries cannot break a user's existing config. A user who had `google→openrouter` in their settings keeps that override.
- **Diagnostic key**: `telemetry_unmapped_providers` keeps the bare-ID CSV form. We drop the previously-possible `source->target` arrow encoding from this key — no test asserts it and it rendered awkwardly. Link/category info moves into the additive `telemetry_unmapped_meta` key. Existing tests (`TestApplyCanonicalTelemetryView_FlagsUnmappedTelemetryProviders`, the TUI mapping tests) all use bare IDs and stay green.
- **Settings file shape**: no schema changes. The config still has `telemetry.provider_links` as `map[string]string`.
- **Tests**: existing tests in `internal/telemetry/read_model_test.go` and `internal/tui/telemetry_mapping_test.go` continue to pass with the unchanged primary diagnostic; new tests cover the new diagnostic and TUI behavior.

## 6. Alternatives Considered

### Alternative A: Fix it in the OpenCode plugin

Have the plugin emit `gemini_api` instead of `google`, `copilot` instead of `github-copilot`. Rejected because (a) the plugin should report the upstream tool's vocabulary, not openusage's internal IDs, and (b) any future telemetry source (not just OpenCode) would face the same problem and need the same fix. Read-time mapping is the right layer.

### Alternative B: Fuzzy auto-map at ingestion

Auto-create links the first time a new unmapped provider id appears, using the same heuristic suggestion. Rejected because it makes behavior magical and irreversible without UI — exactly the situation issue #80 complains about, just with a different cause.

### Alternative C: Auto-create synthetic provider tiles from telemetry

Already explicitly forbidden by `TELEMETRY_INTEGRATIONS.md`. Would put data on the dashboard for providers the user never configured (e.g., a "Moonshot" tile from a single OpenCode message). Skipped.

### Alternative D: Rich JSON in diagnostics

Encode `telemetry_unmapped_meta` as JSON. Rejected; existing diagnostics use flat key=value strings, and the consumer is internal. JSON would invite over-design.

## 7. Implementation Tasks

### Task 1: expand `DefaultProviderLinks` defaults

Files: `internal/config/config.go`, `internal/config/config_test.go`
Depends on: none
Description: Add `google→gemini_api` and `github-copilot→copilot` to `DefaultProviderLinks()`.
Tests: extend `TestDefaultProviderLinks` to assert the three default entries; assert user override still wins.

### Task 2: emit categorized unmapped diagnostic

Files: `internal/telemetry/read_model.go`, `internal/telemetry/read_model_test.go`
Depends on: Task 1
Description: In `annotateUnmappedTelemetryProviders`, build a parallel `meta` slice. For each unmapped source id, classify into `unconfigured` / `mapped_target_missing` (when `provider_links[id]` exists but its target isn't configured). Compute a single optional suggestion via substring match against configured ids. Set diagnostic `telemetry_unmapped_meta`. Keep `telemetry_unmapped_providers` exactly as-is.
Tests: a new test asserting the meta key is populated with correct categories on an OpenCode-shaped fixture (telemetry events for `openai`, `google`, `github-copilot`, configured accounts = `claude_code, openrouter`).

### Task 3: persistence helpers for individual links

Files: `internal/config/config.go`, `internal/config/config_test.go`, `internal/dashboardapp/service.go`
Depends on: none (can run in parallel with Task 2)
Description: Add `SaveProviderLink(source, target string) error` and `DeleteProviderLink(source string) error` (with `…To(path, …)` variants), implemented via `modifyConfig`. Add corresponding `Services` methods on `internal/dashboardapp/service.go`. Extend `Services` interface in `internal/tui/model.go`.
Tests: round-trip test in `internal/config/config_test.go` — set a link, load, assert; delete, load, assert removed.

### Task 4: render unmapped section with categories in TELEM tab

Files: `internal/tui/settings_modal_preferences.go`, `internal/tui/telemetry_mapping_test.go`
Depends on: Task 2
Description: Replace the current static unmapped list with a categorized renderer that reads `telemetry_unmapped_meta` and shows `[no account configured]`, `[mapped → target]`, or `[suggested: target]` per row. Also adds new keybinding hints in the body.
Tests: extend `TestRenderSettingsTelemetryBody_ShowsUnmappedProviders` to cover each category.

### Task 5: interactive remap input handling

Files: `internal/tui/settings_modal.go` (new state), `internal/tui/settings_modal_input.go`, `internal/tui/model.go` (Services interface), `internal/tui/model_commands.go` (new `persistProviderLinkCmd` / `deleteProviderLinkCmd`), new test file `internal/tui/telemetry_mapping_input_test.go`
Depends on: Tasks 3, 4
Description: Extend `settingsTabTelemetry` keypress handler to handle a unified cursor across time windows + unmapped providers. `m` on an unmapped row opens a picker; the picker submits Enter / cancels Esc; `x` clears an existing link. Picker state stored on the settings sub-model.
Tests: simulate keypresses (using existing test patterns in `internal/tui/`) — assert the picker opens, applying triggers `SaveProviderLink` on the fake `Services`, applying clears the unmapped row from the next snapshot.

### Task 6: header pill phrasing

Files: `internal/tui/model_view.go`, `internal/tui/telemetry_mapping_test.go`
Depends on: Task 2
Description: Read `telemetry_unmapped_meta`; render softer phrasing when all entries are `unconfigured` with no suggestion.
Tests: extend `TestRenderHeader_ShowsGlobalUnmappedWarning` with two cases: all `unconfigured` (soft phrasing), at least one `name_mismatch` or with suggestion (action phrasing).

### Task 7: documentation

Files: `docs/TELEMETRY_INTEGRATIONS.md`
Depends on: Tasks 1–6
Description: Add a short section "Mapping Telemetry to Accounts" documenting the default links, the categorization, and the interactive remap.
Tests: none (docs only).

### Dependency Graph

```
Task 1 ──┐
         ├─→ Task 2 ──→ Task 4 ─┐
Task 3 ──┘                     ├─→ Task 5 ──→ Task 7
                                ├─→ Task 6 ────┘
```

Parallel groups:
- **Round 1**: Task 1, Task 3 (independent)
- **Round 2**: Task 2 (needs 1)
- **Round 3**: Task 4, Task 6 (both need 2; can run in parallel)
- **Round 4**: Task 5 (needs 3 + 4)
- **Round 5**: Task 7 (needs all)
