# Provider Widget Section Settings Design

Date: 2026-03-06
Status: Proposed
Author: Codex

## 0. Pre-Design Quiz Answers

1. Problem solved: provider tiles have hardcoded section ordering/visibility, and users cannot preview section changes while editing settings.
2. Beneficiaries: primarily end users; secondarily contributors (clearer widget configuration contract).
3. Affected subsystems: core types, providers, TUI, config.
4. Out of scope: changing telemetry ingestion/metrics semantics; redesigning detail panel sections.
5. Overlapping docs: `MCP_USAGE_SECTION_DESIGN.md` (adds MCP section usage), `DETAIL_PAGE_REDESIGN_DESIGN.md` (detail page only, not tile settings).
6. MVP: settings tab that edits a single global dashboard tile section visibility/order configuration, persisted to `settings.json`, with a live preview panel.
7. Public interfaces changed: config JSON schema (`dashboard.widget_sections`) and core helper exports for dashboard sections.
8. Backward compatibility: additive config only; missing field falls back to current provider defaults.

## 1. Problem Statement

Provider widgets expose standardized tile sections, but section visibility/order is fixed in code and cannot be configured by users in Settings.

## 2. Goals

1. Add a Settings tab to configure dashboard tile section visibility and order.
2. Persist global widget section preferences in config.
3. Apply preferences at render time without changing provider fetch logic.
4. Strengthen provider/widget interface consistency with explicit validation around standardized section usage.
5. Provide a live preview as a separate sibling panel (not nested inside the sections list body) so users can evaluate changes instantly.

## 3. Non-Goals

1. Changing provider data collection or metric key generation.
2. Adding per-account or per-provider overrides in this iteration.
3. Reworking detail-page section abstractions in this iteration.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Export canonical dashboard section list helpers for UI/config normalization |
| providers | minor | Add provider/widget consistency test coverage using existing interfaces |
| TUI | major | Widget Sections controls + runtime override + separate live preview panel in settings overlay |
| config | major | New persisted dashboard widget section config schema and save helpers |
| detect | none | No changes |
| daemon | none | No changes |
| telemetry | none | No changes |
| CLI | none | No command changes |

### Existing Design Doc Overlap

- `docs/MCP_USAGE_SECTION_DESIGN.md`: complementary; introduces `mcp_usage` section, which this feature makes user-toggleable/reorderable.
- `docs/DETAIL_PAGE_REDESIGN_DESIGN.md`: no conflict; this feature targets dashboard tile widget sections only.

## 5. Detailed Design

### 5.1 Core Section Catalog

`internal/core/widget.go` currently has unexported section-order helpers. Add exported helpers:

- `DashboardStandardSections() []DashboardStandardSection`
- `IsKnownDashboardStandardSection(section DashboardStandardSection) bool`

These provide a canonical section list for config normalization and the settings UI.

### 5.2 Config Schema

Add additive dashboard widget section configuration:

```go
type DashboardWidgetSection struct {
    ID      core.DashboardStandardSection `json:"id"`
    Enabled bool                          `json:"enabled"`
}

type DashboardConfig struct {
    Providers      []DashboardProviderConfig       `json:"providers"`
    View           string                          `json:"view"`
    WidgetSections []DashboardWidgetSection        `json:"widget_sections,omitempty"`
}
```

Normalization rules:

1. Unknown section IDs are dropped.
2. Duplicate section IDs are deduplicated by first occurrence.
3. `header` is dropped (header is always rendered outside body section ordering).

Add save API:

- `SaveDashboardWidgetSections(sections []DashboardWidgetSection) error`
- `SaveDashboardWidgetSectionsTo(path string, sections []DashboardWidgetSection) error`

### 5.3 Runtime Widget Override Path

Provider defaults remain source-of-truth. TUI applies config overrides at render time:

1. Keep provider-defined `DashboardWidget().StandardSectionOrder` as fallback.
2. If global override exists, derive `StandardSectionOrder` from enabled section entries in configured order.
3. If override produces zero enabled sections, render no body sections (header remains).
4. If no override exists, preserve current behavior exactly.

Implementation approach:

- Extend `internal/tui/provider_widget.go` with thread-safe in-memory global override state.
- Add setter used by model initialization/update whenever config changes.

### 5.4 Settings Modal: “Widget Sections” Tab + Separate Preview Panel

Add new tab in `settings_modal.go`:

- Top line: global scope indicator.
- Body: all canonical dashboard sections (excluding header) with checkbox (`enabled`) and ordered index.
- Render a separate preview panel (sibling to the Settings modal panel) for live widget preview.
- Preview uses `provider_id: claude_code` with deterministic synthetic snapshot data.
- Preview updates immediately from in-memory state on toggle/reorder actions.
- Responsive panel layout:
  - Side-by-side when terminal width allows.
  - Stacked (settings panel above preview panel) on narrower terminals.
- Controls:
  - `Up/Down`: select section row.
  - `Space/Enter`: toggle section enabled.
  - `Shift+Up/Down` or `Shift+J/K`: move section row.
- Persist after each mutation via new config save command.

UI uses canonical section defaults when no global override exists.

### 5.5 Provider Interface Consistency Check

Add regression tests in `internal/providers` that enforce:

1. Every provider ID is unique/non-empty.
2. `p.Spec().ID` resolves correctly against `p.ID()`.
3. `p.DashboardWidget().EffectiveStandardSectionOrder()` contains only known standard sections.
4. No provider emits duplicate section IDs in effective order.

This keeps interfaces well-defined and prevents drift as providers evolve.

### 5.6 Backward Compatibility

Backward compatible:

1. `dashboard.widget_sections` is optional and additive.
2. Existing configs load unchanged and keep current provider defaults.
3. Providers need no data-path changes; only presentation is overridden at render time.

## 6. Alternatives Considered

### Per-provider section overrides

Rejected for this iteration. A single global configuration is simpler, matches user intent, and is easier to reason about in Settings.

### Per-account section configuration

Rejected for MVP to avoid significantly larger settings surface/state. Global configuration provides the needed value with lower complexity.

## 7. Implementation Tasks

### Task 1: Separate live preview panel for Widget Sections
Files: `internal/tui/settings_modal.go`, `internal/tui/settings_widget_sections_test.go`
Depends on: existing widget section settings/runtime override implementation
Description: Keep Widget Sections list body focused on controls; render live preview as a separate sibling panel in the modal overlay. Use Claude preset synthetic snapshot and responsive side-by-side/stacked layout.
Tests: Add/update TUI tests for panel separation and preview behavior.

### Task 2: Integration verification
Files: none (test/build only)
Depends on: Task 1
Description: Run build/tests/lint/vet for changed areas and verify no regressions in dashboard rendering and settings navigation.
Tests: `make build`, `go test ./internal/config ./internal/tui ./internal/providers -race`, `make vet`, `make lint` (skip if unavailable).

### Dependency Graph

- Task 1: preview panel implementation
- Task 2: depends on Task 1
