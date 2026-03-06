# Project Breakdown Dashboard Section Design

Date: 2026-03-06
Status: Proposed
Author: Codex

## 0. Pre-Design Quiz Answers

1. Problem solved: dashboard tiles do not show per-project (PWD/workspace) request share, so users cannot see how work splits across repositories/projects.
2. Beneficiaries: end users primarily; contributors secondarily (clearer telemetry dimensions and section architecture).
3. Affected subsystems: core types, TUI, telemetry, providers (audit/compat only).
4. Out of scope: retrofitting non-telemetry API providers with synthetic project attribution; per-provider custom naming/rules for project buckets.
5. Overlapping docs: `UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md` (workspace dimension in canonical events), `PROVIDER_WIDGET_SECTION_SETTINGS_DESIGN.md` (normalized section framework), `MCP_USAGE_SECTION_DESIGN.md` (pattern for adding a new dashboard section).
6. Simplest MVP: add a new dashboard tile section that reads telemetry-derived `project_*_requests` metrics (workspace/PWD based) and renders percent breakdown per provider.
7. Public interfaces changed: `core.DashboardStandardSection` adds one new normalized section ID (`project_breakdown`).
8. Backward compatibility: additive only; providers without workspace data simply do not render this section.

## 1. Problem Statement

OpenUsage currently shows model/client/language/tool breakdowns, but it does not expose request distribution by project workspace (PWD), so users cannot answer "what percent of my requests went to each project".

## 2. Goals

1. Add a dedicated dashboard section for project/PWD request breakdown per provider.
2. Aggregate project counts from canonical telemetry `workspace_id` (not client/source heuristics).
3. Preserve existing client/language/model sections and behavior.
4. Ensure section participates in global widget section ordering/toggling.
5. Provide deterministic tests for telemetry aggregation and tile rendering.

## 3. Non-Goals

1. Adding new CLI commands, daemon APIs, or settings schema fields.
2. Backfilling project data for providers that do not emit workspace/PWD information.
3. Renaming existing `client_*` semantics or reworking the client composition model.
4. Introducing filesystem path storage beyond current sanitized workspace basename handling.

## 4. Impact Analysis

### 4.1 Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add `project_breakdown` dashboard section constant and default ordering support |
| providers | minor | No fetch contract changes; verify capability matrix and compatibility |
| TUI | major | New tile section builder + section wiring + used-key tracking |
| config | none | Existing widget section config supports new section ID automatically |
| detect | none | No detection changes |
| daemon | none | No daemon protocol changes |
| telemetry | major | New workspace/project aggregation query + metric/daily-series emission |
| CLI | none | No command or flag changes |

### 4.2 Existing Design Doc Overlap

- `UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md`: this design implements the serving-layer "project/workspace" dimension for dashboard tiles.
- `PROVIDER_WIDGET_SECTION_SETTINGS_DESIGN.md`: this design extends normalized section IDs and leverages existing section ordering/toggle configuration.
- `MCP_USAGE_SECTION_DESIGN.md`: reused implementation pattern (new telemetry aggregate + new TUI section + standard section ID).

### 4.3 Provider Capability Audit (All Providers)

| Provider | Main data path today | Workspace/PWD signal available for request attribution | Project breakdown support in MVP |
|----------|----------------------|---------------------------------------------------------|----------------------------------|
| `openai` | API headers/limits | No | No |
| `anthropic` | API headers/limits | No | No |
| `alibaba_cloud` | API headers/limits | No | No |
| `openrouter` | API analytics/generations | No PWD/workspace dimension | No |
| `groq` | API headers/limits | No | No |
| `mistral` | API headers/limits | No | No |
| `deepseek` | API headers/limits | No | No |
| `xai` | API headers/limits | No | No |
| `zai` | API monitor endpoints | No PWD/workspace dimension | No |
| `gemini_api` | API headers/limits | No | No |
| `gemini_cli` | local session telemetry | Not currently emitted as `workspace_id` | No (future possible) |
| `ollama` | local SQLite telemetry | No workspace on emitted events | No |
| `cursor` | local SQLite telemetry | Not currently emitted as `workspace_id` | No (future possible) |
| `copilot` | local telemetry/SQLite | Yes (`cwd` -> sanitized workspace) | Yes |
| `claude_code` | local JSONL + hook telemetry | Yes (`cwd` -> sanitized workspace) | Yes |
| `codex` | local JSONL + hook telemetry | Yes (`cwd`/hook workspace fields) | Yes |
| `opencode` | hook + JSONL + SQLite telemetry | Yes (`path.cwd`/`path.root`) | Yes |

Notes:
- Current canonical usage view uses client heuristics that frequently prefer `source_system` over workspace. This design intentionally adds a separate project aggregate directly from `workspace_id`.
- `claude_code` non-telemetry fetch already has project-like totals in `Raw`, but not in windowed per-request form required for this feature.

## 5. Detailed Design

### 5.1 Telemetry: Add Project Aggregation by Workspace

Extend canonical usage aggregation to compute project/workspace request totals from `workspace_id` only:

- Add `telemetryProjectAgg` to `internal/telemetry/usage_view.go`.
- Add `Projects []telemetryProjectAgg` and `ProjectDaily map[string][]core.TimePoint` to `telemetryUsageAgg`.
- Add `queryProjectAgg(...)`:
  - Source: `deduped_usage`
  - Filter: `event_type='message_usage'`, `status!='error'`, non-empty `workspace_id`
  - Group: workspace id
  - Metrics: total requests + requests_today
- Extend `queryDailyByDimension(..., "project")` to emit per-day request series by workspace.

Metric and series emission in `applyUsageViewToSnapshot`:

- `project_<workspace>_requests`
- `project_<workspace>_requests_today`
- `DailySeries["usage_project_<workspace>"]`

Cleanup updates:

- Remove stale project metrics/series when rebuilding canonical view (same behavior as existing `model_`, `client_`, etc.).

### 5.2 Core: Add Standard Section ID

In `internal/core/widget.go`:

- Add `DashboardSectionProjectBreakdown DashboardStandardSection = "project_breakdown"`.
- Add to:
  - `defaultDashboardSectionOrder()`
  - `isKnownDashboardSection(...)`

Placement in default order: after `client_burn` and before `tool_usage`.

Rationale: project split is a composition view adjacent to model/client composition.

### 5.3 TUI: New Project Breakdown Section

In `internal/tui/tiles.go`:

- Add `projectMixEntry` type (name, requests, series).
- Add `collectProviderProjectMix(snap)`:
  - Primary source: `project_*_requests` metrics
  - Fallback: sum `usage_project_*` daily series when aggregate metric absent
- Add `buildProviderProjectBreakdownLines(snap, innerW, expanded)`:
  - Heading: `Project Breakdown  <N req>`
  - Stacked bar similar to language/client sections
  - Rows: `■ rank project-name .... xx% <requests> req`
  - Collapsed/expanded top-N behavior consistent with other composition sections
- Wire into section map in `renderTile(...)` and mark consumed keys.

No extra provider opt-in flag is required; the section renders only when project data exists.

### 5.4 Backward Compatibility and Data Behavior

- Additive constants/metrics only.
- Existing client/language/model sections unchanged.
- Providers lacking workspace telemetry render no project section (no placeholder to avoid noise).
- Existing `dashboard.widget_sections` remains valid; unknown IDs are already filtered, and the new known ID becomes available automatically.

## 6. Alternatives Considered

### Alternative A: Reuse `client_*` as project breakdown

Rejected. `client_*` is intentionally heuristic and often maps to `source_system` (`codex`, `claude_code`, etc.) or UI client labels, not workspace/PWD.

### Alternative B: Infer project from file paths in tool payload only

Rejected for MVP. Tool events are partial and not equivalent to request-level attribution; message usage rows already carry cleaner workspace IDs where available.

### Alternative C: Add per-provider custom extraction rules in TUI only

Rejected. Project attribution belongs in telemetry aggregation layer so all consumers (TUI/detail/future exports) share one source of truth.

## 7. Implementation Tasks

### Task 1: Add canonical telemetry project aggregation
Files: `internal/telemetry/usage_view.go`, `internal/telemetry/usage_view_test.go`
Depends on: none
Description: Add project aggregation structs/queries, emit `project_*` metrics and `usage_project_*` daily series from workspace data, and include cleanup of stale project series in canonical overwrite flow.
Tests: Add/extend usage view tests verifying workspace-derived project request metrics/series and non-regression of client behavior.

### Task 2: Add normalized dashboard section ID for project breakdown
Files: `internal/core/widget.go`, `internal/core/widget_test.go`
Depends on: none
Description: Add new `project_breakdown` section constant and include it in default/known section order logic.
Tests: Update section-order tests to assert presence and stable ordering.

### Task 3: Implement dashboard project breakdown renderer
Files: `internal/tui/tiles.go`, `internal/tui/tiles_normalization_test.go`
Depends on: Task 1, Task 2
Description: Implement project mix collection + section rendering (bar + rows + hidden-count behavior), wire section into tile assembly, and mark consumed metric keys.
Tests: Add tests for project mix extraction from `project_*_requests` and daily-series fallback; add rendering smoke test assertions.

### Task 4: Verify widget section configuration integration
Files: `internal/tui/settings_widget_sections_test.go` (and any failing section-order tests)
Depends on: Task 2
Description: Ensure settings/UI expectations remain correct with the new standard section inserted in canonical order.
Tests: Update expected section-order assertions where order prefixes are validated.

### Task 5: Integration verification
Files: none (verification only)
Depends on: Tasks 1-4
Description: Run build/tests/vet/lint for changed scope and confirm no regressions.
Tests: `make build`, changed-package tests with `-race`, `make vet`, `make lint` (skip if unavailable).

### Dependency Graph

- Tasks 1 and 2: parallel (telemetry and core constants independent)
- Task 3: depends on 1 and 2
- Task 4: depends on 2
- Task 5: depends on 1-4

