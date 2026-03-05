# Provider Widget Section Settings Design

Date: 2026-03-05
Status: Proposed
Author: Codex

## 0. Pre-Design Quiz Answers

1. Problem solved: provider tiles have hardcoded section ordering/visibility, so users cannot tailor what they see globally.
2. Beneficiaries: primarily end users; secondarily contributors (clearer widget configuration contract).
3. Affected subsystems: core types, providers, TUI, config.
4. Out of scope: changing telemetry ingestion/metrics semantics; redesigning detail panel sections.
5. Overlapping docs: `MCP_USAGE_SECTION_DESIGN.md` (adds MCP section usage), `DETAIL_PAGE_REDESIGN_DESIGN.md` (detail page only, not tile settings).
6. MVP: settings tab that edits a single global dashboard tile section visibility/order configuration, persisted to `settings.json`.
7. Public interfaces changed: config JSON schema (`dashboard.widget_sections`) and core helper exports for dashboard sections.
8. Backward compatibility: additive config only; missing field falls back to current provider defaults.

## 1. Problem Statement

Provider widgets expose standardized tile sections, but section visibility/order is fixed in code and cannot be configured by users in Settings.

## 2. Goals

1. Add a Settings tab to configure dashboard tile section visibility and order.
2. Persist global widget section preferences in config.
3. Apply preferences at render time without changing provider fetch logic.
4. Strengthen provider/widget interface consistency with explicit validation around standardized section usage.

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
| TUI | major | New Settings tab for section configuration + runtime widget override application |
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

### 5.4 Settings Modal: “Widget Sections” Tab

Add new tab in `settings_modal.go`:

- Top line: global scope indicator.
- Body: all canonical dashboard sections (excluding header) with checkbox (`enabled`) and ordered index.
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

### Task 1: Core + Config schema for widget section preferences
Files: `internal/core/widget.go`, `internal/config/config.go`, `internal/config/config_test.go`, `configs/example_settings.json`
Depends on: none
Description: Export canonical dashboard section helper(s). Add dashboard widget section config structs/normalization and save functions. Ensure loading defaults preserve legacy behavior. Update example config with a global widget section override.
Tests: Extend config tests for normalization, persistence, and backward compatibility with missing `widget_sections`.

### Task 2: TUI runtime support for section overrides
Files: `internal/tui/provider_widget.go`, `internal/tui/model.go`
Depends on: Task 1
Description: Add model-held global widget section preferences and plumbing to pass normalized global overrides into the widget lookup path. Apply override order/visibility on top of provider defaults before rendering.
Tests: Add focused tests in `internal/tui` to verify overridden section order and visibility are honored.

### Task 3: Settings modal tab for widget sections
Files: `internal/tui/settings_modal.go`, `internal/tui/model.go`, `internal/tui/telemetry_mapping_test.go` (or new settings modal test file)
Depends on: Task 2
Description: Add new “Widget Sections” tab, key handling, section toggle/reorder UI, and persistence command wiring. Keep existing settings tab behavior unchanged.
Tests: Add tab rendering and key-interaction tests for toggle, reorder, and persisted-save command emission.

### Task 4: Provider/widget interface consistency coverage
Files: `internal/providers/registry_test.go` (new or existing)
Depends on: none
Description: Add provider abstraction compliance tests covering provider IDs, spec alignment, and valid dashboard standard section usage.
Tests: New table-driven tests over `AllProviders()`.

### Task 5: Integration verification
Files: none (test/build only)
Depends on: Tasks 1-4
Description: Run build/tests/lint/vet for changed areas and verify no regressions in dashboard rendering and settings navigation.
Tests: `make build`, `go test ./internal/config ./internal/tui ./internal/providers -race`, `make vet`, `make lint` (skip if unavailable).

### Dependency Graph

- Task 1: foundational
- Task 4: parallel with Task 1
- Task 2: depends on Task 1
- Task 3: depends on Task 2
- Task 5: depends on Tasks 1-4
