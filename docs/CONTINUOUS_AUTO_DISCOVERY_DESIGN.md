# Continuous Auto-Discovery & Copilot CLI Enhancement — Design Doc

Date: 2026-02-25
Status: Proposed
Author: Claude (with janbaraniewski)

## 1. Problem Statement

Auto-detection only runs on cold start, so newly installed tools are never discovered; additionally, the standalone Copilot CLI (`copilot` binary, the current recommended tool since `gh copilot` was deprecated Oct 2025) is not detected at all, and the existing copilot provider misses rich per-request token/cost data from `assistant.usage` events in session files.

## 2. Goals

1. **Re-run auto-detection on every poll cycle** so newly installed tools are discovered without restart.
2. **Detect the standalone Copilot CLI** (`copilot` binary) in addition to the deprecated `gh copilot` extension.
3. **Parse `assistant.usage` events** from Copilot CLI session data to get per-request input/output tokens, cache tokens, cost, and embedded quota snapshots.
4. **Parse `session.shutdown` events** to get per-session totals with model-level cost breakdowns.
5. **Populate `ModelUsageRecord`** with accurate token counts and cost from usage events (currently only approximated from log compaction data).

## 3. Non-Goals

1. Real-time filesystem watching (inotify/fsnotify).
2. Removing previously detected accounts when a tool is uninstalled.
3. Direct HTTP calls to Copilot API (continue using `gh api` as the gateway).
4. Supporting the `--acp` JSON-RPC mode for live quota queries (future work).
5. Changing TUI components, config schema, or public interfaces.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | No changes |
| providers | minor | Copilot provider enhanced to parse `assistant.usage` and `session.shutdown` events |
| TUI | none | New data flows through existing widget/metric rendering |
| config | none | Existing `SaveAutoDetected()` already supports re-persist |
| detect | minor | Add standalone `copilot` binary detection alongside `gh copilot` |
| daemon | minor | `resolveConfigAccounts()` fix (already implemented) |
| telemetry | none | No changes |
| CLI | none | No changes |

### Existing Design Doc Overlap

- **COLD_START_POLISH_DESIGN.md**: No conflict — explicitly excludes detection logic changes.

## 5. Detailed Design

### 5.1 Fix `resolveConfigAccounts()` to always re-run detection

**Already implemented.** Change `resolveConfigAccounts()` in `internal/daemon/accounts.go` to call the resolver whenever `cfg.AutoDetect` is true, not just when `len(accounts) == 0`.

### 5.2 Detect standalone Copilot CLI binary

In `internal/detect/detect.go`, update `detectGHCopilot()` to:

1. Try `gh copilot --version` first (existing behavior for the deprecated extension)
2. If that fails, look for standalone `copilot` binary via `findBinary("copilot")`
3. If found, also check for `~/.copilot/` directory as confirmation
4. Register account with `Binary` set to `gh` path (the provider uses `gh api` for quota calls) and add `ExtraData["copilot_binary"]` with the standalone binary path
5. Set `ExtraData["config_dir"]` to `~/.copilot/` so the provider knows where to read session data

The provider already reads `~/.copilot/` for sessions/config/logs, so this change is purely about detection. The `gh` binary is still required for API-based quota fetching since the copilot provider calls `gh api /copilot_internal/user`.

### 5.3 Parse `assistant.usage` events from session JSONL

The existing `readSessions()` in `copilot.go` already iterates over events.jsonl lines. Add a new case for `"assistant.usage"` events which contain:

```json
{
  "type": "assistant.usage",
  "data": {
    "model": "claude-sonnet-4.5",
    "inputTokens": 5200,
    "outputTokens": 1800,
    "cacheReadTokens": 3000,
    "cacheWriteTokens": 500,
    "cost": 0.042,
    "duration": 2500,
    "quotaSnapshots": {
      "premium_interactions": {
        "entitlementRequests": 300,
        "usedRequests": 158,
        "remainingPercentage": 47.3,
        "resetDate": "2026-03-01T00:00:00Z"
      }
    }
  }
}
```

For each `assistant.usage` event:
- Accumulate `inputTokens`, `outputTokens`, `cacheReadTokens`, `cacheWriteTokens` per model
- Accumulate `cost` per model and total
- Track `duration` for average latency
- Store latest `quotaSnapshots` as a fallback when `gh api` quota calls fail

This data supplements the existing token tracking (which only comes from log compaction lines, an approximation). The usage events provide **exact** token counts and dollar costs from GitHub's billing system.

### 5.4 Parse `session.shutdown` events

Add a case for `"session.shutdown"` events which contain per-session summaries:

```json
{
  "type": "session.shutdown",
  "data": {
    "totalPremiumRequests": 12,
    "totalApiDurationMs": 45000,
    "codeChanges": {"linesAdded": 150, "linesRemoved": 30, "filesModified": 5},
    "modelMetrics": {
      "claude-sonnet-4.5": {
        "requests": {"count": 10, "cost": 0.35},
        "usage": {"inputTokens": 52000, "outputTokens": 18000, "cacheReadTokens": 30000, "cacheWriteTokens": 5000}
      }
    }
  }
}
```

For each `session.shutdown` event:
- Accumulate `totalPremiumRequests` across sessions
- Accumulate per-model token/cost from `modelMetrics` (this is the most accurate source)
- Track `codeChanges` for productivity metrics (lines added/removed)
- Store `totalApiDurationMs` for latency tracking

### 5.5 Populate ModelUsageRecord with accurate data

Currently `ModelUsageRecord` entries for copilot only have `InputTokens` from log compaction approximations. With `assistant.usage` and `session.shutdown` data, populate:
- `InputTokens` — from usage events
- `OutputTokens` — from usage events (NEW — not currently tracked)
- `CacheReadTokens`, `CacheWriteTokens` — from usage events (NEW)
- `TotalTokens` — sum of all token types
- `Cost` — from usage events (NEW — actual dollar cost)
- `Requests` — count of usage events per model (NEW)

### 5.6 New metrics and daily series

New metrics from usage event data:
- `cli_output_tokens` — total output tokens across all sessions
- `cli_cache_read_tokens` — total cache read tokens
- `cli_cache_write_tokens` — total cache write tokens
- `cli_cost` — total dollar cost from usage events
- `cli_premium_requests` — total premium requests from shutdown events
- `cost` daily series — cost per day

These complement existing metrics (`cli_input_tokens`, `cli_messages`, etc.) and flow through the existing widget rendering system.

### 5.7 Backward Compatibility

- **All changes are additive.** No existing metrics/Raw fields are removed or renamed.
- **Graceful degradation.** If `assistant.usage`/`session.shutdown` events are absent (e.g., older Copilot CLI versions or short sessions), the provider falls back to existing log-compaction-based tracking. The new parsing is purely supplementary.
- **Detection.** The `gh copilot` extension path still works. Standalone detection is a fallback when `gh copilot --version` fails.
- **Config schema.** Unchanged — `ExtraData` map is already flexible.

## 6. Alternatives Considered

### Parse Copilot API responses directly via HTTP

Bypass `gh` and call `api.githubcopilot.com` directly. Rejected because:
- Requires managing OAuth tokens separately
- The `copilot_internal/user` endpoint is undocumented
- `gh api` already handles auth and token refresh

### Add a separate "copilot_cli" provider

Create a distinct provider for the standalone CLI. Rejected because:
- The data sources overlap heavily (same `~/.copilot/` dir, same `gh api` calls)
- Users would see duplicate providers in the dashboard
- Better to enhance the existing provider to handle both detection paths

## 7. Implementation Tasks

### Task 1: Fix `resolveConfigAccounts()` (DONE)
Files: `internal/daemon/accounts.go`, `internal/daemon/accounts_test.go`
Depends on: none
Description: Already implemented — `resolveConfigAccounts()` now always calls the resolver when `AutoDetect` is true.
Tests: `TestResolveConfigAccounts_ReRunsResolverWhenAccountsExist`, `TestResolveConfigAccounts_SkipsResolverWhenAutoDetectFalse`

### Task 2: Detect standalone Copilot CLI binary
Files: `internal/detect/detect.go`, `internal/detect/detect_test.go`
Depends on: none
Description: Update `detectGHCopilot()` to fall back to `findBinary("copilot")` when `gh copilot --version` fails. Check for `~/.copilot/` config dir. Set `ExtraData` with copilot binary path and config dir.
Tests: Add test for standalone binary detection (mock binary existence), test that `gh copilot` still takes precedence when available.

### Task 3: Parse `assistant.usage` events in session reader
Files: `internal/providers/copilot/copilot.go`
Depends on: none
Description: Add `assistantUsageData` struct and handle `"assistant.usage"` events in `readSessions()`. Accumulate per-model input/output/cache tokens, cost, and duration. Store latest quota snapshots. Add new struct types for the usage event data.
Tests: Add test cases in `copilot_test.go` with mock events.jsonl containing `assistant.usage` events, verify token/cost accumulation.

### Task 4: Parse `session.shutdown` events in session reader
Files: `internal/providers/copilot/copilot.go`
Depends on: none
Description: Add `sessionShutdownData` struct and handle `"session.shutdown"` events in `readSessions()`. Accumulate premium requests, per-model cost/token breakdowns from `modelMetrics`, and code change stats.
Tests: Add test cases with mock `session.shutdown` events, verify metrics accumulation and `ModelUsageRecord` population.

### Task 5: Emit new metrics and daily series from usage data
Files: `internal/providers/copilot/copilot.go`, `internal/providers/copilot/widget.go`
Depends on: Task 3, Task 4
Description: After parsing usage/shutdown events, emit new metrics (`cli_output_tokens`, `cli_cost`, `cli_premium_requests`, etc.), populate `ModelUsageRecord` with accurate data (output tokens, cost, requests), and add `cost` daily series. Update widget to include cost row if data is available.
Tests: End-to-end test with mock session data containing usage+shutdown events, verify all new metrics appear in snapshot.

### Dependency Graph

```
- Tasks 1, 2, 3, 4: parallel group (all independent)
- Task 5: depends on 3, 4 (combines their data into metrics/widget)
```
