# Data Time Frames Design

Date: 2026-02-24
Status: Implemented
Author: janekbaraniewski

## 1. Problem Statement

Burn breakdown metrics (model usage, daily series, bar charts, tool/source/client aggregates) have no time-frame filtering — they always show all available data, making it impossible to scope analysis to a meaningful window like "today" or "last 7 days".

## 2. Goals

1. Allow users to view breakdown metrics scoped to a configurable time window (`1d`, `3d`, `7d`, `30d`).
2. Add server-side time-window filtering in the daemon's read model so the TUI receives only the requested window's data.
3. Provide a keyboard shortcut and settings modal option to switch the active time window.
4. Enforce configurable data retention in the daemon to keep the SQLite database bounded.

## 3. Non-Goals

1. **Top-row quota/credit progress bars are unchanged.** These show billing-cycle data from provider APIs, not time series.
2. **Remote/cloud storage.** This is local-only.
3. **Client-side filtering.** The TUI does not filter data — the daemon returns only the requested window.
4. **Per-provider time windows.** The time window is global, not per-account or per-provider.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | New `TimeWindow` type with constants |
| providers | none | Providers are not affected — they return raw snapshots |
| TUI | minor | Time window indicator in status bar, keyboard shortcut to cycle, settings modal option |
| config | minor | New `data` section with `time_window` and `retention_days` fields |
| detect | none | No changes |
| daemon | minor | Pass time window through `ReadModelRequest`, time-window-aware cache invalidation, retention cleanup loop |
| telemetry | moderate | All usage view queries accept a time-window filter; new retention pruning function |
| CLI | none | No new commands |

### Existing Design Doc Overlap

- **`UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md`**: Mentions default retention policy (`raw=30d`, `canonical=400d`). This design implements a simpler version: a single configurable retention for all rows.
- **`MODEL_NORMALIZATION_DESIGN.md`**: Defines a `Window` field on `ModelUsageRecord` and window-aware aggregation. This design does not change per-record windows; it filters at the query level.

Both are referenced but not extended. This is a standalone design.

## 5. Detailed Design

### 5.1 Core Types — TimeWindow

A new type in `internal/core/time_window.go`:

```go
package core

type TimeWindow string

const (
    TimeWindow1d  TimeWindow = "1d"
    TimeWindow3d  TimeWindow = "3d"
    TimeWindow7d  TimeWindow = "7d"
    TimeWindow30d TimeWindow = "30d"
)

var ValidTimeWindows = []TimeWindow{
    TimeWindow1d,
    TimeWindow3d,
    TimeWindow7d,
    TimeWindow30d,
}

func (tw TimeWindow) Days() int {
    switch tw {
    case TimeWindow1d:
        return 1
    case TimeWindow3d:
        return 3
    case TimeWindow7d:
        return 7
    case TimeWindow30d:
        return 30
    default:
        return 30
    }
}

func (tw TimeWindow) Label() string {
    switch tw {
    case TimeWindow1d:
        return "Today"
    case TimeWindow3d:
        return "3 Days"
    case TimeWindow7d:
        return "7 Days"
    case TimeWindow30d:
        return "30 Days"
    default:
        return "30 Days"
    }
}

func ParseTimeWindow(s string) TimeWindow {
    for _, tw := range ValidTimeWindows {
        if string(tw) == s {
            return tw
        }
    }
    return TimeWindow30d
}
```

### 5.2 Config — Data Settings

Add a `DataConfig` section to `internal/config/config.go`:

```go
type DataConfig struct {
    TimeWindow    string `json:"time_window"`    // "1d", "3d", "7d", "30d"
    RetentionDays int    `json:"retention_days"` // max days to keep in SQLite
}
```

Added to `Config`:
```go
type Config struct {
    // ... existing fields ...
    Data DataConfig `json:"data"`
}
```

Defaults: `time_window: "30d"`, `retention_days: 30`.

Validation in `Load()`:
- If `retention_days <= 0`, default to `30`.
- If `retention_days > 90`, cap at `90` (prevent unbounded growth).
- `time_window` is parsed via `core.ParseTimeWindow()` (invalid values default to `"30d"`).
- `time_window` days must not exceed `retention_days` (clamp if needed).

Add a `SaveTimeWindow(window string)` helper following the existing `SaveTheme()` pattern.

Example `settings.json`:
```json
{
  "data": {
    "time_window": "7d",
    "retention_days": 30
  }
}
```

### 5.3 Daemon — ReadModelRequest with TimeWindow

Add a `TimeWindow` field to `ReadModelRequest` in `internal/daemon/types.go`:

```go
type ReadModelRequest struct {
    Accounts      []ReadModelAccount `json:"accounts"`
    ProviderLinks map[string]string  `json:"provider_links"`
    TimeWindow    string             `json:"time_window,omitempty"`
}
```

**Request flow changes:**

1. `BuildReadModelRequestFromConfig()` reads `cfg.Data.TimeWindow` and sets it on the request.
2. `ReadModelRequestKey()` does NOT include the time window in the cache key. Instead, the cache entry stores the time window it was computed with. A time-window mismatch is treated as a cache miss, ensuring limit_snapshot gauge data (which is time-independent) is always fresh when switching windows.
3. `computeReadModel()` passes the time window through to `telemetry.ReadModelOptions`.
4. The `handleReadModel` HTTP handler requires no changes beyond the struct — it already JSON-decodes the full request.

**Client flow changes:**

1. `ViewRuntime.ReadWithFallback()` currently sends an empty `ReadModelRequest{}`. It will include the time window in the request. The `ViewRuntime` will accept a `TimeWindow` field set at construction time, updatable via a setter. The time window is sent as a query-style field in the `ReadModelRequest`.
2. The `StartBroadcaster` in `dashboard.go` passes the time window when creating the `ViewRuntime`.
3. When the user changes the time window (keyboard shortcut or settings modal), the TUI sends a message that updates `ViewRuntime`'s time window and triggers a refresh.

### 5.4 Telemetry — Time-Filtered Queries

Add `TimeWindow` to `ReadModelOptions`:

```go
type ReadModelOptions struct {
    ProviderLinks map[string]string
    TimeWindowDays int // 0 = no filter (all data)
}
```

**Query changes in `usage_view.go`:**

The `usageFilter` struct gets a new field:
```go
type usageFilter struct {
    ProviderIDs    []string
    AccountID      string
    TimeWindowDays int // 0 = no filter
}
```

The `usageWhereClause()` function appends a time bound when `TimeWindowDays > 0`:
```go
if filter.TimeWindowDays > 0 {
    where += fmt.Sprintf(" AND %soccurred_at >= datetime('now', '-%d day')", prefix, filter.TimeWindowDays)
}
```

This single change affects all downstream queries because they all go through `dedupedUsageCTE()` → `usageWhereClause()`.

**Queries that currently hardcode `-30 day`** (`queryDailyTotals`, `queryDailyByDimension`, `queryDailyClientTokens`) will be updated to use the filter's `TimeWindowDays` instead. If `TimeWindowDays` is 0, they fall back to 30.

**Cost window computation** in `applyUsageViewToSnapshot` (`usageCostWindowsUTC`) is also scoped — it operates on the `agg.Daily` data which is already time-filtered by the query. The derived metrics (`today_cost`, `7d_api_cost`, `analytics_30d_cost`) will naturally reflect the filtered window. If the requested window is smaller than 7d, the 7d metric will only reflect available data within the window.

**The `Window` field on emitted metrics** (currently hardcoded to `"all"`) will be updated to reflect the active time window (e.g., `"7d"`) so the TUI can display the correct context.

### 5.5 Telemetry — Data Retention

A new function in `internal/telemetry/store.go`:

```go
func (s *Store) PruneOldEvents(ctx context.Context, retentionDays int) (int64, error) {
    if retentionDays <= 0 {
        return 0, nil
    }
    cutoff := fmt.Sprintf("-%d day", retentionDays)

    // Delete usage_events older than retention window.
    // Foreign key cascade or manual cleanup handles raw events.
    result, err := s.db.ExecContext(ctx, `
        DELETE FROM usage_events
        WHERE occurred_at < datetime('now', ?)
    `, cutoff)
    if err != nil {
        return 0, fmt.Errorf("telemetry: prune old events: %w", err)
    }
    deleted, _ := result.RowsAffected()

    // Clean up orphaned raw events (raw rows no longer referenced by any usage event).
    // This reuses the existing PruneOrphanRawEvents mechanism.
    return deleted, nil
}
```

Called from a new retention loop in the daemon's `Service`, running every 6 hours:

```go
func (s *Service) runRetentionLoop(ctx context.Context) {
    // Run once at startup, then every 6 hours.
    s.pruneOldData(ctx)
    ticker := time.NewTicker(6 * time.Hour)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.pruneOldData(ctx)
        }
    }
}
```

The retention days are read from config at each prune cycle (not cached) so changes take effect without daemon restart.

### 5.6 TUI — Time Window Display and Switching

**Keyboard shortcut**: `w` cycles through time windows (`1d` → `3d` → `7d` → `30d` → `1d`). This is handled in the main `Update()` in `model.go`.

**Header indicator**: The active time window label (e.g., "7 Days") is shown in the header's right-aligned info section alongside the provider count (e.g., "7 Days · 4 providers").

**Settings modal**: Add the time window to the existing "Telemetry" tab. Display the 4 options as a selectable list, persisting on selection via `config.SaveTimeWindow()`.

**Scope**: The `w` shortcut works globally on both Dashboard and Analytics screens.

**Message flow**:
1. User presses `w` → TUI sends a `TimeWindowMsg` to itself.
2. `Update()` handles `TimeWindowMsg`: updates `model.timeWindow`, calls `config.SaveTimeWindow()`, and triggers a refresh via `model.onRefresh()`.
3. The refresh callback in `dashboard.go` reads the updated config (or the ViewRuntime's updated time window) and passes it in the `ReadModelRequest`.

### 5.7 Backward Compatibility

- **Existing configs**: Missing `"data"` section defaults to `time_window: "30d"`, `retention_days: 30`. No breakage.
- **Existing daemon data**: The retention loop only deletes data older than `retention_days`. On first run with default 30d, data older than 30 days is pruned. This is expected and safe — the feature description explicitly requires 30d max retention.
- **Empty `ReadModelRequest`**: The daemon's `handleReadModel` already handles empty requests by building from config. The time window defaults to the config value, so an empty request behaves identically to today (shows 30d).
- **Read model cache**: The cache key does not include the time window. Instead, a time-window mismatch triggers a cache miss and fresh computation. This ensures provider gauge data is always current when switching windows.

## 6. Alternatives Considered

### Client-side filtering

The TUI could receive all 30d of data and filter locally. Rejected because:
- Wastes bandwidth over the unix socket for unused data.
- Makes the daemon's cache less effective (always caching the full dataset).
- Server-side filtering keeps the TUI simple.

### Per-provider time windows

Each provider could have its own time window setting. Rejected because:
- Adds config complexity for marginal benefit.
- Users typically want a consistent view across all providers.
- Can be added later if needed.

### Window buckets: 1d/3d/7d/30d instead of 1d/7d/14d/30d

The original design used 14d as a window. Data analysis of real usage patterns showed that most active usage clusters within the last 7 days, making 14d identical to 7d for providers with cost data. A 3-day "recent work" bucket was added instead, which captures a meaningful slice between "today" and "this week" — e.g., for anthropic/claude-code, 1d=$347, 3d=$374, 7d=$802 showing clear differentiation at each level.

Hourly windows (1h, 2h, 6h, 12h) were also considered and initially implemented, but removed because the telemetry event model stores data at day granularity for most aggregations, and the visible metrics (gauge bars, cost summaries) showed no meaningful difference between hourly windows.

### Separate retention and display as independent configs with no relationship

We could allow `retention_days: 90` and `time_window: "7d"` independently. Accepted — this is what we're doing. The only constraint is that `time_window` days cannot exceed `retention_days` (clamped at load time).

## 7. Implementation Tasks

### Task 1: Core TimeWindow type
Files: `internal/core/time_window.go`, `internal/core/time_window_test.go`
Depends on: none
Description: Add the `TimeWindow` type, constants (`1d`, `3d`, `7d`, `30d`), `Days()`, `Label()`, and `ParseTimeWindow()` functions. Simple value type with no dependencies.
Tests: Table-driven tests for `Days()`, `Label()`, and `ParseTimeWindow()` with valid, invalid, and empty inputs.

### Task 2: Config DataConfig section
Files: `internal/config/config.go`, `internal/config/config_test.go`, `configs/example_settings.json`
Depends on: Task 1
Description: Add `DataConfig` struct with `TimeWindow` and `RetentionDays` fields. Add it to `Config`. Set defaults in `DefaultConfig()`. Add validation in `Load()` (clamp retention 1–90, parse time window, ensure window <= retention). Add `SaveTimeWindow()` helper. Update example config.
Tests: Test default values, validation clamping, `SaveTimeWindow()` round-trip, and backward compatibility (config without `data` section loads correctly).

### Task 3: Daemon ReadModelRequest time window plumbing
Files: `internal/daemon/types.go`, `internal/daemon/accounts.go`, `internal/daemon/server.go`, `internal/daemon/runtime.go`, `internal/daemon/client.go`
Depends on: Task 2
Description: Add `TimeWindow` field to `ReadModelRequest`. Update `BuildReadModelRequestFromConfig()` to read `cfg.Data.TimeWindow`. Update `ReadModelRequestKey()` to include the time window in the cache key. Add a `SetTimeWindow()`/`TimeWindow()` accessor to `ViewRuntime` so the TUI can update it. Update `ReadWithFallback()` to include the time window in requests.
Tests: Test `ReadModelRequestKey()` produces different keys for different windows. Test `BuildReadModelRequestFromConfig()` includes the window from config.

### Task 4: Telemetry time-filtered queries
Files: `internal/telemetry/usage_view.go`, `internal/telemetry/read_model.go`, `internal/telemetry/usage_view_test.go`
Depends on: Task 3
Description: Add `TimeWindowDays` to `usageFilter` and `ReadModelOptions`. Update `usageWhereClause()` to append a time bound. Remove hardcoded `-30 day` from `queryDailyTotals`, `queryDailyByDimension`, `queryDailyClientTokens` and use the filter value instead. Thread `TimeWindowDays` from `ReadModelOptions` through `applyCanonicalUsageViewWithDB` → `loadUsageViewForFilter`. Update `Window` field on emitted metrics to reflect the active window.
Tests: Integration tests with in-memory SQLite: insert events across multiple days, query with different time windows, verify correct filtering. Test that window=0 returns all data (backward compat).

### Task 5: Telemetry data retention
Files: `internal/telemetry/store.go`, `internal/telemetry/store_test.go`, `internal/daemon/server.go`
Depends on: Task 2
Description: Add `PruneOldEvents(ctx, retentionDays)` to `Store`. Add `runRetentionLoop()` to daemon `Service` (runs at startup then every 6 hours). Read `retention_days` from config at each cycle. After pruning events, call existing `PruneOrphanRawEvents` to clean up dangling raw rows.
Tests: Insert events with varied timestamps, call `PruneOldEvents(7)`, verify only events within 7 days remain. Test that raw events orphaned by the prune are cleaned up.

### Task 6: TUI time window switching
Files: `internal/tui/model.go`, `internal/tui/settings_modal.go`, `internal/tui/help.go`, `cmd/openusage/dashboard.go`
Depends on: Tasks 3, 4
Description: Add `timeWindow` field to TUI `Model`. Handle `w` key to cycle windows, save via `config.SaveTimeWindow()`, and trigger refresh. Show active window label in the status bar. Add time window option to settings modal (selectable list in the Telemetry or a new Data tab). Update `dashboard.go` to pass time window to `ViewRuntime` and handle window-change refreshes. Add `w` key to help overlay.
Tests: Manual TUI testing (keyboard shortcut cycles correctly, settings modal persists, status bar updates). Unit test for window cycling logic if extracted to a helper.

### Task 7: End-to-end verification
Files: none (verification only)
Depends on: Tasks 1–6
Description: Build and run the full application. Verify: (1) default config loads with 30d window, (2) pressing `w` cycles windows and the daemon returns filtered data, (3) settings modal shows and persists the window, (4) retention loop prunes old data, (5) existing configs without `data` section work without errors.
Tests: `make build && make test` passes. Manual smoke test of the full flow.
