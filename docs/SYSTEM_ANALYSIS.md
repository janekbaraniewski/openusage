# OpenUsage System Analysis: Antipatterns, Smells & Architectural Issues

**Date**: 2026-03-06
**Scope**: Full codebase analysis (~82K lines across ~150 Go files)

---

## Executive Summary

The codebase has a solid foundation — clean dependency graph, no import cycles, consistent provider interface pattern, and good test coverage. However, it has accumulated significant structural debt across several dimensions:

1. **God files** in the TUI layer (4800-line tiles.go, 2700-line model.go)
2. **Triple type duplication** for telemetry event representations
3. **Semantic field overloading** in `AccountConfig`
4. **`shared` package as a utils dumping ground** mixing HTTP, telemetry, formatting, and JSON traversal
5. **`http.DefaultClient` hardcoded everywhere** — untestable, unconfigurable
6. **Daemon server god object** orchestrating 6 goroutine loops
7. **Leaking presentation concerns** into core types

---

## 1. GOD FILES

### 1.1 `internal/tui/tiles.go` — 4786 lines, 135 functions

**Severity: Critical**

This single file contains:
- Grid layout logic (`tileGrid`, `tileCols`)
- Tile rendering (`renderTile`, `renderTiles*`)
- Gauge construction (`buildTileGaugeLines`, `buildGaugeShimmerLines`)
- Compact metric summaries (`buildTileCompactMetricSummaryLines`)
- Full metric line rendering (`buildTileMetricLines`)
- Header/meta/reset rendering
- Model composition (`buildProviderModelCompositionLines`, `collectProviderModelMix`)
- Vendor composition (`buildProviderVendorCompositionLines`)
- Client composition (`buildProviderClientCompositionLinesWithWidget`)
- Project breakdown (`buildProviderProjectBreakdownLines`)
- Tool composition (`buildProviderToolCompositionLines`)
- Language composition (`buildProviderLanguageCompositionLines`)
- Code stats (`buildProviderCodeStatsLines`)
- MCP usage (`buildMCPUsageLines`)
- Daily trends (`buildProviderDailyTrendLines`)
- Stacked bar rendering (`renderClientMixBar`, `renderModelMixBar`, `renderToolMixBar`)
- Color palette distribution (`distributedPaletteColor`, `stablePaletteOffset`)
- Gemini-specific quota logic (`collectGeminiQuotaEntries`, `buildGeminiOtherQuotaLines`)
- Metric formatting (`formatTileMetricValue`, `compactMetricValue`)
- 10+ composition-specific types (`modelMixEntry`, `clientMixEntry`, `toolMixEntry`, `projectMixEntry`, etc.)

**Recommended decomposition:**
- `tiles_layout.go` — grid calculation, column/row logic
- `tiles_render.go` — top-level tile rendering and tab strips
- `tiles_gauge.go` — gauge construction and shimmer
- `tiles_metrics.go` — compact summaries, metric lines, formatting
- `tiles_composition.go` — model/client/vendor/tool/language mix logic
- `tiles_charts.go` — bar rendering, color palettes
- `tiles_header.go` — header meta, resets, cycle pills
- `tiles_gemini.go` — Gemini-specific quota logic (provider-specific code in TUI is itself a smell)

### 1.2 `internal/tui/model.go` — 2695 lines

**Severity: High**

The `Model` struct is the Bubble Tea model and contains all TUI state. The `Update()` method handles every keypress, message, and event. This is typical of Bubble Tea apps, but at 2695 lines it warrants extraction of:
- Settings modal handling → already partially in `settings_modal.go`
- Analytics update logic → could be extracted
- Daemon status management

### 1.3 `internal/tui/detail.go` — 1924 lines

**Severity: Medium**

Similar bloat pattern with detail panel rendering.

---

## 2. TYPE DUPLICATION (Triple Representation Problem)

### 2.1 Telemetry Event Types — Three Nearly Identical Structs

**Severity: Critical**

The same token/cost field set appears in three places:

| Field | `shared.TelemetryEvent` | `telemetry.IngestRequest` | `telemetry.CanonicalEvent` |
|-------|------------------------|--------------------------|---------------------------|
| InputTokens | `*int64` | `*int64` | `*int64` |
| OutputTokens | `*int64` | `*int64` | `*int64` |
| ReasoningTokens | `*int64` | `*int64` | `*int64` |
| CacheReadTokens | `*int64` | `*int64` | `*int64` |
| CacheWriteTokens | `*int64` | `*int64` | `*int64` |
| TotalTokens | `*int64` | `*int64` | `*int64` |
| CostUSD | `*float64` | `*float64` | `*float64` |
| Requests | `*int64` | `*int64` | `*int64` |

Plus overlapping session/identity fields (SessionID, TurnID, MessageID, ToolCallID, ProviderID, AccountID, AgentName, ModelRaw, EventType, Status).

**Files:**
- `internal/providers/shared/telemetry.go:79-106` — `TelemetryEvent`
- `internal/telemetry/types.go:49-79` — `IngestRequest`
- `internal/telemetry/types.go:81-110` — `CanonicalEvent`

**Additionally**, `shared.HookUsage` (`shared/hook_usage.go:5-13`) duplicates the same token fields a fourth time.

**Fix:** Extract a shared `TokenUsage` struct into `core`:
```go
type TokenUsage struct {
    InputTokens      *int64
    OutputTokens     *int64
    ReasoningTokens  *int64
    CacheReadTokens  *int64
    CacheWriteTokens *int64
    TotalTokens      *int64
    CostUSD          *float64
    Requests         *int64
}
```
Embed it in all three event types and `HookUsage`.

### 2.2 Channel/Status Enum Duplication

**Severity: Medium**

| Concept | `shared` package | `telemetry` package |
|---------|-----------------|-------------------|
| Channel | `TelemetryChannel` (hook/sse/jsonl/api/sqlite) | `SourceChannel` (hook/sse/jsonl/api/sqlite) |
| Status | `TelemetryStatus` (ok/error/aborted/unknown) | `EventStatus` (ok/error/aborted/unknown) |
| Event type | `TelemetryEventType` (4 values) | `EventType` (6 values, superset) |

These are identical concepts with different names in different packages.

**Files:**
- `internal/providers/shared/telemetry.go:17-43`
- `internal/telemetry/types.go:9-43`

### 2.3 Backwards-Compatibility Aliases

**Severity: Low (smell)**

`shared/telemetry.go:114-117`:
```go
var Float64Ptr = core.Float64Ptr
var FirstNonEmpty = core.FirstNonEmpty
```

These var-aliases exist "for backwards compatibility" but create import confusion. Callers should use `core.Float64Ptr` directly.

---

## 3. SEMANTIC FIELD OVERLOADING

### 3.1 `AccountConfig.Binary` and `AccountConfig.BaseURL`

**Severity: High**

These fields have contradictory meanings depending on the provider:

| Provider | `Binary` means | `BaseURL` means |
|----------|---------------|----------------|
| copilot | CLI binary path | *(unused)* |
| gemini_cli | CLI binary path | *(unused)* |
| cursor | tracking DB path | state DB path |
| claude_code | stats-cache.json path | .claude.json path |
| codex | *(unused)* | ChatGPT base URL |
| openai | *(unused)* | API base URL |
| ollama | *(unused)* | API base URL or ollama.com URL |

Comments in `provider.go:18` and `provider.go:23` acknowledge this:
```go
// Binary is the path to a CLI binary for CLI-based providers.
// For local-file providers it is repurposed as a data file path
```

**Fix:** Replace with provider-specific config via a `map[string]string` or typed provider config:
```go
type AccountConfig struct {
    // ... common fields ...
    ProviderConfig map[string]string `json:"provider_config,omitempty"`
}
```

Or better: let providers define their own config schema and store it in `ExtraData` (which currently exists but is `json:"-"`).

---

## 4. PACKAGE STRUCTURE ISSUES

### 4.1 `internal/providers/shared` — Utils Dumping Ground

**Severity: High**

This package contains 6 files with unrelated concerns:

| File | Responsibility |
|------|---------------|
| `helpers.go` | HTTP request/response helpers, auth, URL resolution |
| `telemetry.go` | Telemetry types, timestamp parsing, file collection, path utilities |
| `format.go` | Number formatting, string truncation |
| `labels.go` | Dashboard section ordering, metric labels, coding tool config |
| `hook_usage.go` | Hook payload token extraction type |
| `jsonpath.go` | JSON path traversal utilities |

A package named "shared" that everything depends on is the classic "utils" antipattern. These have no cohesion.

**Fix:** Split into purpose-specific locations:
- `helpers.go` HTTP helpers → `internal/httputil/` or stay in `providerbase`
- `telemetry.go` types → `internal/telemetry/` (consolidate with existing types there)
- `telemetry.go` timestamp parsing → `internal/timeutil/` or `internal/parsers/`
- `telemetry.go` file collection → `internal/fsutil/`
- `format.go` → `internal/format/`
- `labels.go` → `internal/providers/providerbase/` (it's provider widget config)
- `hook_usage.go` → merge into the shared TokenUsage type
- `jsonpath.go` → `internal/jsonutil/`

### 4.2 `internal/parsers` — Thin Wrapper

**Severity: Low**

This package (`helpers.go`) provides HTTP header parsing utilities. It's well-focused but could be merged with the HTTP helpers currently in `shared/helpers.go` into a single `internal/httputil/` package.

### 4.3 `internal/providers/common` — Empty Directory

**Severity: Low**

An empty package directory exists at `internal/providers/common/`. Should be removed.

---

## 5. HTTP CLIENT USAGE

### 5.1 `http.DefaultClient` Hardcoded Everywhere

**Severity: High**

29 call sites across providers use `http.DefaultClient.Do(req)` directly. This means:
- **No timeout control** — `http.DefaultClient` has no timeout by default
- **No connection pooling tuning** — all providers share one global transport
- **Untestable without `httptest.Server`** — cannot inject a mock client
- **No retry/backoff** — each provider implements its own (or doesn't)

**Files (sample):**
- `openai/openai.go:65`, `deepseek/deepseek.go:91`, `anthropic/anthropic.go:60`
- `xai/xai.go:89`, `groq/groq.go:54`, `mistral/mistral.go:101,160,220`
- `cursor/cursor.go:702,726,749`, `openrouter/openrouter.go:355,531,594,748,1943,2199`
- `shared/helpers.go:88`

**Fix:** Inject an `*http.Client` through `providerbase.Base` or pass it via the `Fetch()` context:
```go
type Base struct {
    spec   core.ProviderSpec
    client *http.Client  // injected, testable
}
```

---

## 6. DAEMON SERVICE GOD OBJECT

### 6.1 `internal/daemon/server.go` — 1237 lines, 6 Goroutine Loops

**Severity: High**

`Service` struct manages:
1. `runCollectLoop` — telemetry collection
2. `runPollLoop` — provider polling
3. `runReadModelCacheLoop` — read model cache refresh
4. `runSpoolMaintenanceLoop` — spool flush + cleanup
5. `runHookSpoolLoop` — hook payload processing
6. `runRetentionLoop` — data retention

Plus: socket server, HTTP handlers, logging, mutex management, cache management.

Three separate mutexes (`pipelineMu`, `ingestMu`, `logMu`) with nested locking patterns:
```go
s.pipelineMu.Lock()
s.ingestMu.Lock()  // nested lock
// ...
s.ingestMu.Unlock()
s.pipelineMu.Unlock()
```

**Fix:** Extract each loop into its own worker type:
- `CollectWorker`, `PollWorker`, `ReadModelCacheWorker`, `SpoolWorker`, `HookSpoolWorker`, `RetentionWorker`
- `Service` becomes a coordinator that starts/stops workers

### 6.2 Daemon Knows Provider Paths

**Severity: Medium**

`server.go:30-35`:
```go
const (
    defaultCodexSessionsDir     = "~/.codex/sessions"
    defaultGeminiSessionsDir    = "~/.gemini/tmp"
    defaultClaudeProjectsDir    = "~/.claude/projects"
    defaultClaudeProjectsAltDir = "~/.config/claude/projects"
    defaultOpenCodeDBPath       = "~/.local/share/opencode/opencode.db"
)
```

Provider-specific paths are hardcoded in the daemon package. These should come from the providers themselves (via `TelemetrySource` or a new config method on providers).

---

## 7. LEAKING RESPONSIBILITIES

### 7.1 Presentation Logic in Core Types

**Severity: Medium**

`core.DashboardWidget` (widget.go) is a 57-field struct that mixes:
- **Data contract** (`DataSpec`, `RequiredMetricKeys`)
- **Presentation** (`ColorRole`, `GaugePriority`, `GaugeMaxLines`, `DisplayStyle`, `ResetStyle`)
- **Content filtering** (`HideMetricKeys`, `HideMetricPrefixes`, `SuppressZeroMetricKeys`)
- **Composition panel toggles** (`ShowClientComposition`, `ShowToolComposition`, `ShowLanguageComposition`, `ShowCodeStatsComposition`, `ShowActualToolUsage`, `ShowMCPUsage`)
- **Label overrides** (`MetricLabelOverrides`, `CompactMetricLabelOverrides`)
- **Auth metadata** (`APIKeyEnv`, `DefaultAccountID`)

The `IsZero()` method checks 21 fields manually — a maintenance trap that falls out of sync when new fields are added.

**Fix:** Split into `DataContract`, `PresentationConfig`, and `FilterConfig` sub-structs.

### 7.2 Auth Metadata in Widget Config

`DashboardWidget.APIKeyEnv` and `DashboardWidget.DefaultAccountID` duplicate `ProviderAuthSpec.APIKeyEnv` and `ProviderAuthSpec.DefaultAccountID`:

```go
// In ProviderAuthSpec:
APIKeyEnv        string
DefaultAccountID string

// Also in DashboardWidget:
APIKeyEnv        string
DefaultAccountID string
```

The widget shouldn't carry auth information.

### 7.3 Gemini-Specific Logic in Generic TUI Code

`tiles.go` contains 8 Gemini-specific functions:
- `collectGeminiQuotaEntries` (line 1763)
- `geminiQuotaLabelFromMetricKey` (line 1800)
- `geminiPrimaryQuotaMetricKey` (line 1827)
- `isGeminiQuotaResetKey` (line 1849)
- `filterGeminiPrimaryQuotaReset` (line 1857)
- `buildGeminiOtherQuotaLines` (line 1917)

Provider-specific rendering logic should not be in the generic TUI layer.

---

## 8. ENCAPSULATION ISSUES

### 8.1 `UsageSnapshot` — All Public Maps

**Severity: Medium**

`UsageSnapshot.Metrics`, `Raw`, `Attributes`, `Diagnostics` are all `map[string]string`/`map[string]Metric` with no access control. Any code can read/write any key. The `SetAttribute()` / `SetDiagnostic()` methods exist but nothing forces their use.

The `EnsureMaps()` method exists because the zero value is unsafe (nil maps panic on write). `NewUsageSnapshot()` initializes them, but `NewAuthSnapshot()` does not — creating an inconsistency.

### 8.2 `telemetry` Package Importing `providers/shared`

**Severity: Medium**

The dependency graph shows:
```
telemetry → providers/shared → core
```

The `telemetry` package should not depend on `providers/shared`. It imports it for `TelemetryEvent`, `TelemetryChannel`, etc. — types that should live in `telemetry` (or `core`) instead.

### 8.3 `daemon` Imports Everything

The daemon imports 8 internal packages:
```
daemon → config, core, detect, integrations, providers, providers/shared, telemetry, version
```

This is a cohesion problem. The daemon should depend on abstract interfaces, not concrete provider implementations.

---

## 9. CODE DUPLICATION

### 9.1 HTTP Request Construction Pattern

Every provider that makes HTTP calls repeats:
```go
req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil {
    return fmt.Errorf("...: creating request: %w", err)
}
req.Header.Set("Authorization", "Bearer "+apiKey)
resp, err := http.DefaultClient.Do(req)
if err != nil {
    return fmt.Errorf("...: request failed: %w", err)
}
defer resp.Body.Close()
```

Despite `shared.CreateStandardRequest` and `shared.ProbeRateLimits` existing, many providers still do this manually (deepseek `fetchBalance`, xai `fetchAPIKeyInfo`, mistral, ollama, cursor, openrouter, etc.).

### 9.2 `ProcessStandardResponse` vs `ProbeRateLimits` Overlap

`shared/helpers.go` has two functions that do nearly the same thing:
- `ProcessStandardResponse` (line 26) — handles status codes, sets snap fields
- `ProbeRateLimits` (line 81) — creates request, handles status codes, applies rate limits

`ProbeRateLimits` essentially does `CreateStandardRequest` + `ProcessStandardResponse` + `ApplyStandardRateLimits` but reimplements parts of each.

### 9.3 `TotalTokens` Computation — Duplicated 3 Times

The "sum token parts into TotalTokens" logic appears in:
1. `shared/hook_usage.go:26-41` — `HookUsage.SumTotalTokens()`
2. `telemetry/types.go:158-170` — `normalizeRequest()`
3. Implicitly in various provider telemetry adapters

### 9.4 Composition Rendering Pattern × 7

`tiles.go` repeats this pattern for every composition type:
1. `collect*Mix()` → build entries from snapshot metrics
2. `limit*Mix()` → truncate to N visible
3. `build*ColorMap()` → generate color assignments
4. `render*MixBar()` → render horizontal stacked bar
5. `build*CompositionLines()` → assemble final lines

This pattern is identical for model, client, vendor, tool, language, project, and upstream providers — 7 instances of the same structural pattern with different field names.

---

## 10. MINOR SMELLS

### 10.1 Inconsistent Error Wrapping

Some providers wrap with prefix: `fmt.Errorf("openai: creating request: %w", err)`
Others don't: `return err` (xai `fetchAPIKeyInfo`)

### 10.2 `stringInSlice` and `containsString` — Two Identical Functions

`tiles.go:1062` and `tiles.go:1305` — same logic, different names:
```go
func stringInSlice(s string, items []string) bool { ... }
func containsString(items []string, value string) bool { ... }
```
Both are equivalent to `slices.Contains`.

### 10.3 Mixed Pointer Conventions for Numeric Types

- `core.ModelUsageRecord` uses `*float64` for tokens
- `shared.TelemetryEvent` uses `*int64` for tokens
- `core.Metric` uses `*float64` for Limit/Remaining/Used

The same conceptual value (e.g., input tokens) is `*int64` in telemetry and `*float64` in core, requiring conversion helpers (`NumberToInt64Ptr`, `NumberToFloat64Ptr`).

### 10.4 `AppendModelUsageRecord` Free Function

`core/model_usage.go:96-101`:
```go
func AppendModelUsageRecord(snap *UsageSnapshot, rec ModelUsageRecord) {
    if snap == nil { return }
    snap.AppendModelUsage(rec)
}
```
This nil-guarded wrapper around a method adds no value. Callers can nil-check themselves.

---

## Recommended Priority Order

| Priority | Issue | Impact | Effort | Status |
|----------|-------|--------|--------|--------|
| 1 | Split `tiles.go` | Maintainability, code review, navigation | Medium | **DONE** — split into tiles_gauge.go, tiles_header.go, tiles_metrics.go, tiles_composition.go |
| 2 | Consolidate telemetry types | Remove triple duplication | Medium | **DONE** — extracted `core.TokenUsage`, embedded in 4 types |
| 3 | Fix `AccountConfig` field overloading | Correctness, clarity | Medium | **DONE** — added `Paths` map with `Path()` accessor |
| 4 | Inject HTTP client | Testability, timeout safety | Low | **DONE** — `providerbase.Base.Client()` with 30s timeout default |
| 5 | Split `shared` package | Package cohesion | Medium | *Not started* |
| 6 | Extract daemon workers | Maintainability, testability | Medium | **PARTIAL** — removed hardcoded paths via `DefaultCollectOptions()` |
| 7 | Separate widget presentation from data | Clean architecture | High | *Not started* |
| 8 | Remove Gemini-specific TUI code | Provider isolation | Low | *Not started* |
| 9 | Deduplicate composition pattern | DRY, maintainability | High | *Not started* |
| 10 | Clean up minor smells | Code hygiene | Low | **DONE** — removed compat aliases, replaced `stringInSlice`/`containsString` with `slices.Contains`, removed `AppendModelUsageRecord` free function, fixed `NewAuthSnapshot` nil maps |

### Additional fixes applied
- Channel/status enum duplication (2.2): Backward-compat aliases (`shared.Int64Ptr`, `shared.Float64Ptr`, `shared.FirstNonEmpty`) removed; all callers updated to `core.` directly
- Provider-specific paths moved from daemon constants to `TelemetrySource.DefaultCollectOptions()` on each provider
- `internal/providers/common/` empty directory confirmed already removed
