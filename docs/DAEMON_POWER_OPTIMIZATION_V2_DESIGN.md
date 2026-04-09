# Daemon Power Optimization V2 Design

Date: 2026-04-09
Status: Implemented
Author: janekbaraniewski

## 1. Problem Statement

The daemon burns 141% CPU during active Claude Code use because the Collect loop re-parses ALL 886 JSONL files every 20 seconds with zero caching, duplicating work the Poll loop already does with a file-level cache — and there is no adaptive backoff on the Collect loop.

## 2. Goals

1. Add mtime+size caching to the Collect path so unchanged JSONL files are never re-parsed.
2. Add adaptive backoff to the Collect loop (same pattern as PollScheduler) so it backs off when no new events are found.
3. Add incremental JSONL parsing so only new lines (appended since last read) are parsed, avoiding full-file re-reads of active conversation files.

## 3. Non-Goals

1. Merging Poll and Collect into a single loop (architectural change, separate design).
2. fsnotify-based event-driven collection (adds external dependency, separate design).
3. Incremental read model queries (large refactor, separate design).
4. Changes to non-JSONL providers (Cursor SQLite, Copilot CLI — already optimized).

## 4. Impact Analysis

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | No changes |
| providers | moderate | Claude Code + Codex `Collect()` use cached parsing; `shared.CollectFilesByExt` replaced with stat-aware variant |
| TUI | none | No changes |
| config | none | No changes |
| detect | none | No changes |
| daemon | minor | Collect loop gets adaptive backoff |
| telemetry | minor | `SourceCollector` tracks last-collect time for change detection |
| CLI | none | No changes |

## 5. Detailed Design

### 5.1 Collect-path file caching for Claude Code

The `Collect()` method in `claude_code/telemetry_usage.go:28-50` currently:
1. Walks all JSONL files via `shared.CollectFilesByExt()` (no stat info)
2. Calls `ParseTelemetryConversationFile(file)` for EVERY file (no caching)

**Fix**: Replace with stat-aware walk + mtime/size cache, mirroring the Fetch path.

Add a telemetry cache to the Provider struct (`claude_code/claude_code.go`):

```go
type Provider struct {
    // ... existing fields ...
    telemetryCacheMu sync.Mutex
    telemetryCache   map[string]*telemetryCacheEntry
}

type telemetryCacheEntry struct {
    modTime time.Time
    size    int64
    events  []shared.TelemetryEvent
}
```

Change `Collect()` to:
1. Use `collectJSONLFilesWithStat()` (already exists in `local_helpers.go`) instead of `shared.CollectFilesByExt()`
2. Check mtime+size before calling `ParseTelemetryConversationFile()`
3. Return cached events for unchanged files

### 5.2 Collect-path file caching for Codex

Same pattern: `codex/telemetry_usage.go:32-55` uses `shared.CollectFilesByExt()` + full parse. Apply the same cache.

Add a telemetry cache to the Codex Provider and use mtime+size to skip re-parsing unchanged session files.

### 5.3 Incremental JSONL parsing

JSONL files are append-only. When the active conversation file grows (new messages appended), the current approach re-parses the ENTIRE file. Instead, track the byte offset of the last read and only parse new lines.

Change `telemetryCacheEntry` to also store the byte offset:

```go
type telemetryCacheEntry struct {
    modTime  time.Time
    size     int64
    events   []shared.TelemetryEvent
    byteSize int64 // file size at last full parse
}
```

Logic:
- If mtime changed but new size > old size: file was appended to
  - Seek to `byteSize`, parse only new lines
  - Append new events to cached events
  - Update `byteSize` to new size
- If mtime changed and new size <= old size: file was rewritten
  - Full re-parse (rare — JSONL files don't normally shrink)
- If mtime unchanged: return cached events

### 5.4 Adaptive backoff for Collect loop

The Collect loop in `server_collect.go:12-27` uses a fixed ticker. Add backoff when no new events are collected:

```go
func (s *Service) runCollectLoop(ctx context.Context) {
    interval := s.cfg.CollectInterval
    maxInterval := 5 * time.Minute
    consecutiveEmpty := 0

    for {
        select {
        case <-ctx.Done():
            return
        case <-time.After(interval):
            collected := s.collectAndFlush(ctx)
            if collected == 0 {
                consecutiveEmpty++
                if consecutiveEmpty >= 3 {
                    interval = min(interval*2, maxInterval)
                }
            } else {
                consecutiveEmpty = 0
                interval = s.cfg.CollectInterval
            }
        }
    }
}
```

This requires `collectAndFlush` to return the count of collected events. Currently it returns nothing — change it to return `int`.

The `dataIngested` flag already resets the read model refresh when new data arrives, so the read model will respond quickly after backoff resets.

### 5.5 Backward Compatibility

- Caching is transparent — same events produced, just faster.
- Incremental parsing produces identical results to full parsing (append-only invariant).
- Adaptive backoff resets immediately when new data is found, so latency is unchanged during active use.

## 6. Alternatives Considered

### Share the Fetch path's jsonlCache with Collect

Rejected: the Fetch path caches `conversationRecord` structs while Collect needs `TelemetryEvent` structs. Different output types require separate caches. Sharing the underlying file read is possible but would require a larger refactor (merging the two paths).

### Use a global file-change watcher instead of per-call stat checks

Rejected for this iteration: adds fsnotify dependency and complexity. The stat-based cache achieves 90%+ of the benefit with zero new dependencies.

## 7. Implementation Tasks

### Task 1: Add telemetry cache to Claude Code Collect path
Files: `internal/providers/claude_code/claude_code.go`, `internal/providers/claude_code/telemetry_usage.go`, `internal/providers/claude_code/local_helpers.go`
Depends on: none
Description:
- Add `telemetryCacheMu sync.Mutex` and `telemetryCache map[string]*telemetryCacheEntry` fields to Provider struct in `claude_code.go`.
- Add `telemetryCacheEntry` struct with `modTime`, `size`, `byteSize`, `events` fields.
- Change `Collect()` in `telemetry_usage.go:28-50` to use `collectJSONLFilesWithStat()` instead of `shared.CollectFilesByExt()`, and check mtime+size before parsing.
- Implement incremental parsing: when file grew (size > byteSize), seek to old offset and parse only new lines. When file shrunk or mtime changed with same size, full re-parse.
Tests: Test that unchanged files return cached events. Test that appended lines produce only new events. Test that a truncated file triggers full re-parse.

### Task 2: Add telemetry cache to Codex Collect path
Files: `internal/providers/codex/codex.go`, `internal/providers/codex/telemetry_usage.go`
Depends on: none (parallel with Task 1)
Description: Same pattern as Task 1 but for Codex. Add cache fields to Codex Provider, use stat-aware walk, skip unchanged files.
Tests: Same pattern as Task 1 tests.

### Task 3: Add adaptive backoff to Collect loop
Files: `internal/daemon/server_collect.go`
Depends on: none (parallel with Tasks 1-2)
Description:
- Change `collectAndFlush()` to return the number of collected events (`int`).
- Replace the fixed ticker in `runCollectLoop` with `time.After(interval)` and adaptive interval logic: double interval after 3 consecutive empty cycles (cap at 5 min), reset to base on any collected events.
Tests: Test that interval doubles after empty cycles. Test that interval resets on data.

### Task 4: Build and verify
Depends on: Tasks 1-3
Description: `go build ./...`, `go test` all changed packages, verify CPU usage drops.

### Dependency Graph
```
Tasks 1, 2, 3: parallel (independent)
Task 4: depends on all
```
