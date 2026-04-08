# Telemetry Timestamp Integrity Design

Date: 2026-04-08
Status: Implemented
Author: janekbaraniewski

## 1. Problem Statement

Telemetry events with missing/zero `OccurredAt` timestamps are silently stamped as `time.Now()` by `normalizeRequest()`, causing all historical data from providers like Cursor (38,083 events, 100% of tool_usage) and Ollama (18 events, 100%) to appear as "today" on every daemon restart — rendering time window filters useless for these providers.

## 2. Goals

1. Drop telemetry events with zero `OccurredAt` at the collector level before they reach the store.
2. Fix Cursor's `toolEventsFromBubbleRecords` and `bubbleTokenEventsFromRecords` to skip events when no session timestamp is available.
3. Fix Ollama to skip events with unparseable `createdAt`.
4. Clean up existing bad events in the telemetry database.

## 3. Non-Goals

1. Changing `normalizeRequest()`'s zero→now fallback — it's still correct for hook events that explicitly set `time.Now()` before calling it.
2. Fixing the 0.2-0.7% of Claude Code events with slightly off timestamps (minor edge cases).
3. Changing deduplication logic.

## 4. Impact Analysis

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | No changes |
| providers | minor | Cursor and Ollama telemetry skip zero-timestamp events |
| TUI | none | No changes |
| config | none | No changes |
| detect | none | No changes |
| daemon | none | No changes |
| telemetry | minor | `SourceCollector.Collect()` filters zero-timestamp events; store migration cleans bad data |
| CLI | none | No changes |

## 5. Detailed Design

### 5.1 Collector-level filter (`collector_source.go:44-47`)

Add a guard in the event mapping loop to skip events with zero `OccurredAt`:

```go
for _, ev := range events {
    if ev.OccurredAt.IsZero() {
        continue // skip events without a valid timestamp
    }
    out = append(out, mapProviderEvent(c.Source.System(), ev, c.AccountOverride))
}
```

This is the single choke point for ALL provider telemetry. It protects against any provider producing zero timestamps, current or future.

### 5.2 Cursor telemetry fixes (`cursor/telemetry.go`)

#### `toolEventsFromBubbleRecords` (line 328)

Skip records where the session timestamp lookup returns zero:

```go
occurredAt := sessionTimestamps[record.SessionID]
if occurredAt.IsZero() {
    continue
}
```

#### `bubbleTokenEventsFromRecords` (line 493)

Same fix:

```go
occurredAt := sessionTimestamps[record.SessionID]
if occurredAt.IsZero() {
    continue
}
```

### 5.3 Ollama telemetry fix (`ollama/telemetry.go`)

Lines 175 and 256: `shared.FlexParseTime()` returns zero on failure. Skip events where it fails:

```go
occurredAt := shared.FlexParseTime(createdAt.String)
if occurredAt.IsZero() {
    continue
}
```

Same pattern for tool_calls at line 256.

### 5.4 Database cleanup migration (`telemetry/store.go`)

Add a one-time migration in `ensureSchema()` that deletes events where `occurred_at` matches the ingestion time within 1 second AND the source is a collector (not hooks or pollers). These are the events that were stamped with `time.Now()` due to zero timestamps.

Simpler approach: delete cursor/ollama events from today that have the bad timestamp pattern (occurred_at within 1s of raw_event ingested_at AND source_system IN ('cursor', 'ollama')):

```sql
DELETE FROM usage_events
WHERE event_id IN (
    SELECT e.event_id
    FROM usage_events e
    JOIN usage_raw_events r ON r.raw_event_id = e.raw_event_id
    WHERE r.source_system IN ('cursor', 'ollama')
      AND e.session_id IS NULL OR e.session_id = ''
      AND ABS(julianday(e.occurred_at) - julianday(r.ingested_at)) < 0.00002
)
```

This targets events where occurred_at ≈ ingested_at (within ~1.7 seconds), which identifies the zero-timestamp events that got stamped as now.

### 5.5 Backward Compatibility

- Hook events that set `OccurredAt = time.Now()` before reaching `normalizeRequest()` are unaffected — they have non-zero timestamps.
- `normalizeRequest()` is unchanged — the zero→now fallback remains as a safety net but should rarely trigger now.
- Existing correctly-timestamped events are unaffected.

## 6. Implementation Tasks

### Task 1: Collector-level zero-timestamp filter
Files: `internal/telemetry/collector_source.go`
Depends on: none
Description: Add `if ev.OccurredAt.IsZero() { continue }` guard in `SourceCollector.Collect()` at line 45, before `mapProviderEvent`.

### Task 2: Cursor telemetry timestamp fixes
Files: `internal/providers/cursor/telemetry.go`
Depends on: none
Description: Add `if occurredAt.IsZero() { continue }` after the `sessionTimestamps` lookup in both `toolEventsFromBubbleRecords` (line 328) and `bubbleTokenEventsFromRecords` (line 493).

### Task 3: Ollama telemetry timestamp fixes
Files: `internal/providers/ollama/telemetry.go`
Depends on: none
Description: Add `if occurredAt.IsZero() { continue }` after `FlexParseTime` calls at lines 175 and 256.

### Task 4: Database cleanup
Files: `internal/telemetry/store.go`
Depends on: none
Description: Add a migration to delete events with bad timestamps (occurred_at ≈ ingested_at for cursor/ollama source systems with no session).

### Task 5: Build and test
Depends on: Tasks 1-4
Description: `go build ./...`, run tests, verify.
