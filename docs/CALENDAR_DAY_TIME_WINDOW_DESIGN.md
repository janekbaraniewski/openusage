# Calendar Day Time Window Design

Date: 2026-04-08
Status: Proposed
Author: janekbaraniewski

## 1. Problem Statement

The "Today" (1d) time window uses a rolling 24-hour SQL filter (`datetime('now', '-24 hour')`) instead of calendar-day filtering, so yesterday afternoon's data appears under "Today" — while per-row `requests_today` annotations use a different definition (`date(occurred_at) = date('now')`, which is UTC calendar day), creating an inconsistency where the main totals and the row annotations disagree.

## 2. Goals

1. Make the "Today" (1d) time window filter from local midnight instead of rolling 24 hours.
2. Align the `requests_today` SQL annotations with the same local-midnight boundary.
3. Eliminate UTC timezone hazard in "today" computations by computing cutoffs in Go with `time.Now().Location()`.
4. Apply generically across all providers — the fix is in the telemetry query layer, not per-provider.

## 3. Non-Goals

1. **Changing 3d/7d/30d semantics.** These remain rolling-hour windows. Only "1d" changes to calendar-day.
2. **Per-user timezone configuration.** We use the system's local timezone (`time.Now().Location()`).
3. **Provider-level changes.** No provider code changes. The fix is entirely in the telemetry query layer.
4. **Changing the `requests_today` field name or semantics beyond aligning the boundary.** It still means "today's requests" — we're just fixing *what "today" means*.
5. **Changing the TUI or config schema.** The label "Today" is already correct; we're fixing the data behind it.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add `Since()` and `LocalMidnight()` to `time_window.go` |
| providers | none | No provider changes — filtering is in telemetry layer |
| TUI | none | "Today" label already correct, data behind it changes |
| config | none | No new config fields |
| detect | none | No changes |
| daemon | minor | `server_read_model.go` passes `Since` instead of `TimeWindowHours` |
| telemetry | moderate | `usageFilter`, `ReadModelOptions`, `usageWhereClause`, and 7 `requests_today` annotations change |
| CLI | none | No changes |

### Existing Design Doc Overlap

**`DATA_TIME_FRAMES_DESIGN.md`** — The original time window design (Status: Implemented). It documents `1d` as "Today" with rolling-hour filtering. Our design extends it by changing only the "1d" window's filter boundary from rolling 24h to local midnight. The overall architecture (single global time window, server-side filtering, `usageFilter` struct) is preserved. All other windows (3d, 7d, 30d, all) are unchanged.

## 5. Detailed Design

### 5.1 Core Types — `Since()` and `LocalMidnight()`

Add to `internal/core/time_window.go`:

```go
// LocalMidnight returns midnight (00:00:00) of the current local day.
func LocalMidnight() time.Time {
    now := time.Now()
    return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// Since returns the cutoff time for this window.
// For "1d" (Today): local midnight (calendar day boundary).
// For "3d", "7d", "30d": rolling N*24 hours from now.
// For "all": zero time (no filter).
func (tw TimeWindow) Since() time.Time {
    now := time.Now()
    switch tw {
    case TimeWindowAll:
        return time.Time{}
    case TimeWindow1d:
        return LocalMidnight()
    case TimeWindow3d:
        return now.Add(-3 * 24 * time.Hour)
    case TimeWindow7d:
        return now.Add(-7 * 24 * time.Hour)
    case TimeWindow30d:
        return now.Add(-30 * 24 * time.Hour)
    default:
        return now.Add(-30 * 24 * time.Hour)
    }
}
```

`Hours()`, `Days()`, and `SQLiteOffset()` are **kept unchanged** for backward compatibility — they're still used by `Days()` and trace logging.

### 5.2 Telemetry — Filter Struct Changes

Change `usageFilter` in `internal/telemetry/usage_view.go`:

```go
type usageFilter struct {
    ProviderIDs     []string
    AccountID       string
    Since           time.Time // main window cutoff (zero = no filter)
    TodaySince      time.Time // "today" annotation cutoff (always local midnight)
    materializedTbl string
}
```

Replace `TimeWindowHours int` with `Since time.Time` and add `TodaySince time.Time`.

Change `ReadModelOptions` in `internal/telemetry/read_model.go`:

```go
type ReadModelOptions struct {
    ProviderLinks map[string]string
    Since         time.Time       // computed from TimeWindow.Since()
    TodaySince    time.Time       // always LocalMidnight()
    TimeWindow    core.TimeWindow // kept for logging/labels
}
```

Replace `TimeWindowHours int` with `Since time.Time` and `TodaySince time.Time`.

### 5.3 Telemetry — Query Changes

#### Main WHERE clause (`usageWhereClause`)

In `internal/telemetry/usage_view_queries.go`, change:

```go
// Before:
if filter.TimeWindowHours > 0 {
    where += fmt.Sprintf(" AND %soccurred_at >= datetime('now', '-%d hour')",
        prefix, filter.TimeWindowHours)
}

// After:
if !filter.Since.IsZero() {
    where += fmt.Sprintf(" AND %soccurred_at >= '%s'",
        prefix, filter.Since.UTC().Format(time.RFC3339Nano))
}
```

Using inline-formatted UTC timestamp (not `?` parameter) because:
- The value is a `time.Time` format — no injection risk.
- Avoids complex parameter ordering with the existing CTE args.
- The same approach is used for `TodaySince` in SELECT clauses where parameter ordering would require duplicating the arg N times.

#### `requests_today` annotations

Add a helper method to `usageFilter` in `internal/telemetry/usage_view_queries.go` (alongside `usageWhereClause`):

```go
// todayExpr returns a SQL expression that is true for events occurring on
// the local calendar day. Falls back to UTC date('now') if TodaySince is zero.
func (f usageFilter) todayExpr(col string) string {
    if f.TodaySince.IsZero() {
        return fmt.Sprintf("date(%s) = date('now')", col)
    }
    return fmt.Sprintf("%s >= '%s'", col, f.TodaySince.UTC().Format(time.RFC3339Nano))
}
```

Replace all 7 occurrences of `date(occurred_at) = date('now')` in query functions with `filter.todayExpr("occurred_at")`:

| Query function | File:line | Occurrences | Notes |
|---|---|---|---|
| `queryModelAgg` | `usage_view_queries.go:51` | 1 | `requests_today` column |
| `querySourceAgg` | `usage_view_queries.go:96` | 1 | `requests_today` column |
| `queryProjectAgg` | `usage_view_queries.go:152` | 1 | `requests_today` column |
| `queryToolAgg` | `usage_view_queries.go:188,190,192,194` | 4 | `calls_today`, `calls_ok_today`, `calls_error_today`, `calls_aborted_today` columns |

Each query function already receives `filter usageFilter` as a parameter (passed from `loadMaterializedUsageAgg` at `usage_view_aggregate.go:11`), so no signature changes are needed for the query functions themselves.

**MCP aggregation** (`usage_view_helpers.go:101`, `buildMCPAgg`): Derives its `Calls1d` fields from `telemetryToolAgg.Calls1d`. No changes needed — it automatically inherits the corrected values.

### 5.4 Daemon — Threading `Since` Through

In `internal/daemon/server_read_model.go`, change `computeReadModel`:

```go
// Before:
tw := normalizeReadModelTimeWindow(req.TimeWindow)
result, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates,
    telemetry.ReadModelOptions{
        ProviderLinks:   req.ProviderLinks,
        TimeWindowHours: tw.Hours(),
        TimeWindow:      tw,
    })

// After:
tw := normalizeReadModelTimeWindow(req.TimeWindow)
result, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates,
    telemetry.ReadModelOptions{
        ProviderLinks: req.ProviderLinks,
        Since:         tw.Since(),
        TodaySince:    core.LocalMidnight(),
        TimeWindow:    tw,
    })
```

### 5.5 Telemetry — Internal Threading

#### `applyCanonicalUsageViewWithDB` (`usage_view.go:137`)

Current signature:
```go
func applyCanonicalUsageViewWithDB(
    ctx context.Context, db *sql.DB,
    snaps map[string]core.UsageSnapshot,
    providerLinks map[string]string,
    timeWindowHours int, timeWindow core.TimeWindow,
) (map[string]core.UsageSnapshot, error)
```

New signature:
```go
func applyCanonicalUsageViewWithDB(
    ctx context.Context, db *sql.DB,
    snaps map[string]core.UsageSnapshot,
    providerLinks map[string]string,
    since time.Time, todaySince time.Time, timeWindow core.TimeWindow,
) (map[string]core.UsageSnapshot, error)
```

**Internal changes in this function:**
- The `windowLabel` condition at lines 196-198 and 207-209 changes from `timeWindowHours > 0 && timeWindow != ""` to `!since.IsZero() && timeWindow != ""`.
- The call to `loadUsageViewForProviderWithSources` at line 176 passes `since, todaySince` instead of `timeWindowHours`.

#### `loadUsageViewForProviderWithSources` (`usage_view.go:249`)

Current signature:
```go
func loadUsageViewForProviderWithSources(
    ctx context.Context, db *sql.DB,
    providerIDs []string, accountID string,
    timeWindowHours int,
) (*telemetryUsageAgg, error)
```

New signature:
```go
func loadUsageViewForProviderWithSources(
    ctx context.Context, db *sql.DB,
    providerIDs []string, accountID string,
    since time.Time, todaySince time.Time,
) (*telemetryUsageAgg, error)
```

**Internal changes:** The `usageFilter` construction at lines 257-260 and 277-279 changes from `TimeWindowHours: timeWindowHours` to `Since: since, TodaySince: todaySince`.

#### Call site in `read_model.go:107`

```go
// Before:
result, err := applyCanonicalUsageViewWithDB(ctx, db, merged, links, options.TimeWindowHours, options.TimeWindow)

// After:
result, err := applyCanonicalUsageViewWithDB(ctx, db, merged, links, options.Since, options.TodaySince, options.TimeWindow)
```

#### Trace logging changes

- `usage_view_materialize.go:64`: Change `filter.TimeWindowHours` to `filter.Since.Format(time.RFC3339)` in trace message.
- `usage_view.go:315`: Change `filter.TimeWindowHours` to `filter.Since.Format(time.RFC3339)` in trace message.
- `read_model.go:59`: Change `options.TimeWindowHours` to `options.Since.Format(time.RFC3339)` in trace message.

### 5.6 Backward Compatibility

- **No config changes.** `TimeWindow` values ("1d", "3d", etc.) are unchanged.
- **No stored data changes.** Event timestamps in SQLite are unchanged.
- **No provider changes.** The fix is entirely query-side.
- **`Hours()` and `SQLiteOffset()` preserved.** They're still available for `Days()` and any future use.
- **`ReadModelRequest` wire format unchanged.** The daemon HTTP API sends `TimeWindow` strings; `Since` is computed server-side.
- **Behavioral change:** "Today" shows less data than before (only since midnight, not the last 24h). This is the intended fix. Users who previously relied on seeing yesterday afternoon's data under "Today" will need to switch to "3 Days".
- **`requests_today` annotations change from UTC calendar day to local calendar day.** For users in UTC, no visible difference. For others, the annotation now correctly reflects their local "today".

## 6. Alternatives Considered

### Alternative 1: Rename "Today" to "24h"

Change the label instead of the filter. Rejected because:
- Users universally expect "Today" to mean calendar today.
- The `requests_today` annotations already try to be calendar-based — renaming would increase the inconsistency.
- Fixes the symptom (confusing label) but not the root cause (wrong filter boundary).

### Alternative 2: Make all windows calendar-based

"3 Days" = midnight 3 days ago, "7 Days" = midnight 7 days ago, etc. Rejected because:
- Adds unnecessary complexity for windows where the distinction is negligible.
- Only "Today" has a strong user expectation of calendar semantics.
- Can be added later if requested.

### Alternative 3: Use SQL `datetime('now', 'localtime')` instead of Go-computed timestamps

SQLite's `'localtime'` modifier uses the process's TZ environment. Rejected because:
- Mixes timezone handling between Go and SQLite — harder to test and debug.
- Go's `time.Now().Location()` is more predictable and testable.
- Computing in Go allows unit testing with fixed times (inject a clock).

### Alternative 4: Use `?` parameters instead of inline-formatted timestamps

Parameterize the timestamp values in SQL. Rejected because:
- The `requests_today` expressions appear in SELECT clauses (not WHERE), and the tool query alone has 4 occurrences needing the same value — requiring 4 duplicate args in the right positional order.
- The formatted values come from `time.Time.Format()` — no injection risk.
- Inline formatting is simpler and less error-prone for this use case.

## 7. Implementation Tasks

### Task 1: Core — Add `Since()` and `LocalMidnight()`
Files: `internal/core/time_window.go`, `internal/core/time_window_test.go`
Depends on: none
Description: Add `LocalMidnight()` function and `Since()` method on `TimeWindow`. `Since()` returns local midnight for "1d", rolling hours for other windows, zero for "all". Keep `Hours()`, `Days()`, `SQLiteOffset()` unchanged.
Tests: Table-driven tests for `Since()` — verify "1d" returns midnight, "7d" returns ~168h ago, "all" returns zero. Test `LocalMidnight()` returns a time with zero hour/minute/second in the local timezone.

### Task 2: Telemetry — Change filter structs, query generation, and internal signatures
Files: `internal/telemetry/usage_view.go`, `internal/telemetry/usage_view_queries.go`, `internal/telemetry/usage_view_materialize.go`, `internal/telemetry/read_model.go`, `internal/telemetry/helpers_test.go`
Depends on: Task 1
Description: Specific changes per file:

**`internal/telemetry/read_model.go`:**
- Line 44: Replace `TimeWindowHours int` with `Since time.Time` and `TodaySince time.Time` in `ReadModelOptions` struct.
- Line 59: Update trace log to print `options.Since.Format(time.RFC3339)` instead of `options.TimeWindowHours`.
- Line 107: Update call to `applyCanonicalUsageViewWithDB` to pass `options.Since, options.TodaySince` instead of `options.TimeWindowHours`.

**`internal/telemetry/usage_view.go`:**
- Lines 130-135: Replace `TimeWindowHours int` with `Since time.Time` and `TodaySince time.Time` in `usageFilter` struct.
- Lines 137-144: Change `applyCanonicalUsageViewWithDB` signature from `(ctx, db, snaps, providerLinks, timeWindowHours int, timeWindow core.TimeWindow)` to `(ctx, db, snaps, providerLinks, since time.Time, todaySince time.Time, timeWindow core.TimeWindow)`.
- Lines 196-198, 207-209: Change `timeWindowHours > 0` condition to `!since.IsZero()` for windowLabel logic.
- Line 176: Update call to `loadUsageViewForProviderWithSources` to pass `since, todaySince` instead of `timeWindowHours`.
- Lines 249-289: Change `loadUsageViewForProviderWithSources` signature from `(ctx, db, providerIDs, accountID, timeWindowHours int)` to `(ctx, db, providerIDs, accountID, since time.Time, todaySince time.Time)`. Update filter construction at lines 257-260 and 277-279 to use `Since: since, TodaySince: todaySince`.
- Line 315: Update trace log to print `filter.Since.Format(time.RFC3339)` instead of `filter.TimeWindowHours`.

**`internal/telemetry/usage_view_queries.go`:**
- Lines 658-688: In `usageWhereClause()`, replace `TimeWindowHours > 0` check with `!filter.Since.IsZero()` check, and change SQL from `datetime('now', '-%d hour')` to inline-formatted `filter.Since.UTC().Format(time.RFC3339Nano)`.
- Add `todayExpr(col string) string` method on `usageFilter` (adjacent to `usageWhereClause`).
- Lines 51, 96, 152, 188, 190, 192, 194: Replace all 7 `date(occurred_at) = date('now')` with `filter.todayExpr("occurred_at")` using `fmt.Sprintf`.

**`internal/telemetry/usage_view_materialize.go`:**
- Line 64: Update trace log to print `filter.Since.Format(time.RFC3339)` instead of `filter.TimeWindowHours`.

**`internal/telemetry/helpers_test.go`:**
- Line 29: Update call to `applyCanonicalUsageViewWithDB` from `(ctx, db, snaps, nil, 0, "")` to `(ctx, db, snaps, nil, time.Time{}, time.Time{}, "")`. The zero `time.Time` values mean "no filter", preserving the existing test semantics.
- Line 33: `applyCanonicalTelemetryViewForTest` passes `ReadModelOptions{}` — zero-value struct has `Since: time.Time{}` which means "no filter". No changes needed.

Tests: Update existing `usage_view_test.go` — insert events across multiple days, query with `Since = core.LocalMidnight()`, verify only today's events are returned. Test `todayExpr` returns correct SQL for both zero and non-zero `TodaySince`.

### Task 3: Daemon — Thread `Since` through read model
Files: `internal/daemon/server_read_model.go`
Depends on: Task 2
Description: Update `computeReadModel()` to build `ReadModelOptions` with `Since: tw.Since()` and `TodaySince: core.LocalMidnight()` instead of `TimeWindowHours: tw.Hours()`.
Tests: Existing daemon tests should continue to pass. Add a focused test that verifies `computeReadModel` with TimeWindow "1d" produces a `Since` value at local midnight.

### Task 4: Verification
Files: none (verification only)
Depends on: Tasks 1-3
Description: `make build && make test` passes. Manual smoke test: run the daemon, select "Today" window, verify that only today's (since midnight) data appears. Verify `requests_today` annotations match the main totals when on "Today" window.
Tests: Full test suite green. Manual verification of the fix.

### Dependency Graph

```
Task 1 (core types) — foundational, no deps
Task 2 (telemetry queries) — depends on Task 1
Task 3 (daemon threading) — depends on Task 2
Task 4 (verification) — depends on all
```

All tasks are sequential — each builds on the previous. The change set is small enough that parallelization isn't needed.
