# Codex Artificial Credit Limit Design

Date: 2026-07-15
Status: Implemented
Author: Amanda Chappell / Codex

## 1. Problem Statement

OpenUsage can only forecast against Codex's provider-reported credit quota, so users cannot monitor themselves against a deliberately lower personal budget.

## 2. Goals

1. Allow a Codex account to define an optional absolute credit cap below the provider-reported quota.
2. Use the effective cap for percentage used, near-limit status, runout time, and projected percentage at reset.
3. Preserve and display the authoritative reported quota so an artificial cap is never mistaken for a provider-enforced limit.
4. Keep existing behavior unchanged when no override is configured.

## 3. Non-Goals

1. Preventing Codex from consuming credits after the artificial cap is reached; the feature is advisory only.
2. Adding a generic cross-provider budgeting framework.
3. Adding a generic in-TUI account editor; the MVP adds only a Codex personal-cap editor.
4. Adding this fork-specific feature to the existing upstream Codex credit forecasting pull request.
5. Adding Codex credit caps to the Analytics budget panel in the MVP.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add an optional pointer field to `AccountConfig`. |
| providers | minor | Codex preserves the reported quota and derives an effective cap before forecasting. |
| TUI | moderate | Settings edits the Codex cap; usage views label the self-imposed cap and reported quota. |
| config | moderate | Additive field plus safe promotion of an auto-detected account when the setting is saved. |
| detect | none | Manual accounts already override auto-detected accounts with the same ID. |
| daemon | minor | A changed or cleared cap bypasses adaptive/unchanged-source caches on the next provider poll. |
| telemetry | none | Existing snapshot metric maps can store the additional reported-limit metric. |
| CLI | none | No command or flag is added. |

### Existing Design Doc Overlap

This design builds on `docs/CODEX_PROVIDER_PARITY_DESIGN.md`, which introduced the authoritative Codex quota, inferred monthly period start, burn rate, runout time, and reset projection. It does not modify that document because the artificial cap is a fork-specific user preference rather than provider parity.

`docs/CURSOR_TEAM_BUDGET_GAUGE_DESIGN.md` demonstrates the existing convention that a dashboard gauge can represent an effective budget while retaining source billing data. This design reuses that presentation principle without introducing Cursor-specific types or a shared budgeting abstraction.

## 5. Detailed Design

### 5.1 Account Configuration

Add one optional field to `core.AccountConfig`:

```go
CreditLimitOverride *float64 `json:"credit_limit_override,omitempty"`
```

Example:

```json
{
  "id": "codex-cli",
  "provider": "codex",
  "binary": "codex",
  "credit_limit_override": 4000
}
```

The field is intentionally a pointer so an omitted override is distinguishable from an explicitly invalid zero. Only the Codex provider consumes it in this MVP. Codex auto-detection uses the account ID `codex-cli`, so a manual `codex-cli` account entry wins over the auto-detected account through the existing `MergeAccounts` ordering.

Validation is provider-local:

1. `nil`: preserve current behavior.
2. `<= 0`, NaN, or infinity: ignore the override and add a non-fatal diagnostic.
3. `>= reported limit`: retain the reported limit as effective and mark the configured override inactive.
4. `> 0` and `< reported limit`: activate the artificial cap.
5. No reported quota: retain the configured value as metadata but do not synthesize quota usage from it.

### 5.2 Reported and Effective Metrics

`applyCreditLimitDetails` continues to decode the authoritative Codex quota into `codex_credit_limit`. After live and CLI sources are merged, a new provider helper applies the account override before `applyCreditForecast` and `applyRateLimitStatus` run.

When an override is active:

1. Copy the original metric to `codex_credit_reported_limit` with its reported `Limit`, `Used`, and `Remaining` values.
2. Replace `codex_credit_limit.Limit` with the artificial cap.
3. Keep `codex_credit_limit.Used` equal to authoritative cumulative usage; do not truncate usage to the artificial cap.
4. Set effective remaining to `max(cap - used, 0)`.
5. Recalculate `codex_credit_percent_used` against the artificial cap and clamp its displayed percentage to `0...100`.
6. Keep the reported reset timestamp on `codex_credit_limit`; the personal budget follows the same provider period.

The existing metric key remains the effective metric so the dashboard gauge, status calculation, and forecast code use the personal cap without provider-agnostic TUI special cases. Codex credit limits are not currently extracted into the Analytics budget panel, and this MVP does not add that integration.

Snapshot metadata records the distinction:

```text
credit_limit_override_configured=<value>
credit_limit_override_active=true|false
credit_limit_reported=<value>
credit_limit_effective=<value>
```

### 5.3 Forecast and Status Semantics

The burn rate remains based on authoritative cumulative usage over elapsed time; an artificial cap does not change consumption history.

Runout and projection calculations use effective remaining credits:

```text
effective remaining = max(artificial cap - authoritative used, 0)
runout hours = effective remaining / current-period average burn rate
```

If authoritative usage already meets or exceeds the artificial cap:

1. `codex_credit_percent_used` is `100%`.
2. `codex_credit_runout_hours` is `0` when a burn rate is available.
3. The snapshot reaches `LIMITED` through the existing rate-limit status logic.

`LIMITED` describes the configured OpenUsage budget state; it does not claim that Codex will reject requests.

### 5.4 TUI Presentation

The Codex top usage gauge continues to use `codex_credit_percent_used`. When the override is active, its annotation includes a compact cap marker before the existing reset/projection text, for example:

```text
cap 4k · resets 16d 5h · 100% in 9d 2h
```

The Codex detail forecast section shows both values explicitly:

```text
Credit Usage ........ 3,200 / 4,000 credits (80%)
Personal Cap ........ 4,000 credits (advisory)
Reported Quota ...... 7,500 credits
Credit Rate ......... 7.8 credits/hour
Credit Forecast ..... 4.3 days left at 7.8 credits/hour
```

The `codex_credit_reported_limit` metric is added to the Codex widget's hidden metric keys for generic tile rendering and to the explicit skip list in `buildDetailOtherMetrics`. This keeps it out of generic rows so it is rendered only in the intentional Codex detail presentation; the shared detail renderer is not changed to reinterpret `HideMetricKeys` globally.

### 5.5 Configuration and Provider Documentation

Update the canonical configuration reference with `credit_limit_override`, including that it is currently consumed by Codex and is advisory. Update the Codex provider page with an account example, effective-limit behavior, and the distinction between provider quota and personal cap.

The example settings file gains a Codex account showing the field without changing defaults for existing users.

### 5.6 Settings UI

The Providers settings tab adds a `CAP` column. Codex rows show the configured value or `--`; other providers show `n/a`. With a Codex row selected, `l` opens an inline numeric editor. Enter saves a positive credit value, an empty value clears the cap, and Escape cancels. Invalid, zero, negative, NaN, and infinite values are rejected before persistence.

Saving a cap for an auto-detected account promotes that account into `accounts` with the same ID, preserving its detected fields. Clearing the cap removes the promoted account when it otherwise matches the detected account, restoring normal auto-detection; a customized manual account is retained with only the cap cleared. A successful save requests a snapshot refresh.

### 5.7 Backward Compatibility

The change is additive. Existing JSON configurations omit the pointer field and produce byte-for-byte equivalent provider calculations. Stored snapshots require no migration because metrics are map-backed and the new reported metric is optional. Auto-detected Codex accounts continue to work; users can opt in from Settings or JSON.

## 6. Alternatives Considered

### Percentage-Based Override

A percentage such as `80% of reported quota` adapts automatically when Codex changes the quota, but it is less direct for a user who has a concrete monthly credit budget. The confirmed MVP uses an absolute credit value.

### Provider-Specific Options Map

A generic `provider_options` map would avoid adding a field to `AccountConfig`, but it adds parsing and validation machinery for one setting and weakens schema documentation. A typed optional field is simpler and safer.

### Replace the Reported Limit Without Preserving It

This minimizes metrics but makes the artificial value look authoritative. Preserving `codex_credit_reported_limit` is required for trustworthy presentation and debugging.

### Enforce the Limit in Codex

OpenUsage is an observer and has no supported control path for stopping Codex requests. Enforcement is outside scope.

## 7. Implementation Tasks

### Task 1: Add the account configuration field and persistence
Files: `internal/core/provider.go`, `internal/config/config.go`, `internal/config/config_test.go`, `configs/example_settings.json`
Depends on: none
Description: Add the optional `CreditLimitOverride` field and persistence that promotes a detected account when saving and restores auto-detection when clearing a promoted-only account.
Tests: Cover promotion, field preservation, clearing, and customized manual accounts.

### Task 2: Add the Settings editor
Files: `internal/dashboardapp/service.go`, `internal/tui/model.go`, `internal/tui/model_commands.go`, `internal/tui/model_input.go`, `internal/tui/settings_modal.go`, `internal/tui/settings_modal_input.go`, `internal/tui/settings_modal_layout.go`, `internal/tui/settings_modal_sections.go`, `internal/tui/settings_modal_tab_handlers.go`, `internal/tui/settings_credit_limit_test.go`
Depends on: Task 1
Description: Add a Codex-only inline cap editor to Settings → Providers, persist through Services, update local state, and request a refresh after save.
Tests: Cover save, clear, and rendering.

### Task 3: Derive the effective Codex credit cap
Files: `internal/providers/codex/credit_forecast.go`, `internal/providers/codex/codex.go`, `internal/providers/codex/credit_forecast_test.go`
Depends on: Task 1
Description: Apply the configured cap after authoritative quota collection and before forecast/status calculation. Preserve the reported metric and metadata, recalculate effective remaining and percentage, and diagnose invalid values.
Tests: Cover absent, active, equal/higher, invalid, no-reported-quota, and already-over-cap cases.

### Task 4: Show the cap and reported quota in the TUI
Files: `internal/providers/codex/widget.go`, `internal/tui/tiles_gauge.go`, `internal/tui/tiles_gauge_test.go`, `internal/tui/detail_sections.go`, `internal/tui/detail_codex_credit_forecast_test.go`
Depends on: Task 3
Description: Add the compact cap annotation, explicitly label personal and reported limits in detail, add the reported metric to the widget hide list and the detail renderer's explicit skip list, and suppress it from generic rows without changing shared hide-key semantics.
Tests: Verify active-cap annotation/detail output, unchanged output without a cap, and over-cap rendering.

### Task 5: Document configuration and semantics
Files: `docs/site/docs/reference/configuration.md`, `docs/site/docs/providers/codex.md`, `docs/CODEX_ARTIFICIAL_CREDIT_LIMIT_DESIGN.md`
Depends on: Tasks 1, 2, and 3
Description: Document the Settings workflow, JSON field, advisory semantics, and relationship between effective and reported limits. Correct the existing Codex provider account ID and unsupported `extra` example.
Tests: Build the Docusaurus site and verify no broken links.

### Task 6: Integration validation
Files: no planned source changes
Depends on: Tasks 1, 2, 3, 4, and 5
Description: Run formatting, changed-package race tests, full build, vet, docs build, and a live snapshot/TUI check with an override below the current reported quota.
Tests: `gofmt`, `go test -race ./internal/core ./internal/config ./internal/providers/codex ./internal/tui -count=1`, `make build`, `make vet`, and `DOCS_PREVIEW=1 npm run build` from `docs/site`.

### Dependency Graph

- Task 1 is foundational.
- Tasks 2 and 3 depend on Task 1.
- Task 4 depends on Task 3.
- Task 5 depends on Tasks 1 through 3.
- Task 6 depends on all implementation and documentation tasks.
