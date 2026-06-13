# Cache Hit Ratio Design

Date: 2026-06-12
Status: Proposed
Author: cache-hit-ratio feature (issue #212)

## 1. Problem Statement

OpenUsage collects prompt-cache token counts (cache-read and cache-creation)
for every cache-capable provider but never computes or displays the **cache hit
ratio**, so users running Claude Code and similar tools cannot see how much of
their prompt volume is being served from cache.

## 2. Goals

1. Define a single, defensible cache-hit-ratio metric and compute it from token
   data that already flows through the pipeline.
2. Surface it as a `cache_hit_ratio` percentage metric that renders as a gauge
   on the dashboard tile and in the detail panel, for every provider whose data
   source exposes the needed token split.
3. Work in **both** runtime modes: daemon/telemetry mode (the central
   `usage_view` projection) and direct mode (the provider's own `Fetch`).
4. Degrade gracefully: providers that cannot support it emit nothing and show no
   misleading `0%`.

## 3. Non-Goals

1. **Not** changing header-probing API providers (`anthropic`, `openai`,
   `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`, `alibaba_cloud`, `zai`,
   `moonshot`, `perplexity`) to make billed completion calls just to obtain
   cached-token counts. Their lightweight probes never see a usage body, so
   cache hit ratio is structurally unavailable and out of scope.
2. **Not** a cost-savings metric. The ratio reflects token coverage, not the
   ~90% billing discount on cache reads. (Noted in docs; a cost-savings figure
   could be a later addition.)
3. **Not** a new persisted column or schema migration — the required columns
   (`cache_read_tokens`, `cache_write_tokens`, `input_tokens`) already exist.
4. **Not** a per-request hit/miss counter — the metric is token-weighted.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | New shared helper `core.CacheHitRatio(...)`; no struct changes. |
| providers | minor | `claude_code`, `codex`, `openrouter` emit `cache_hit_ratio` in direct-mode projection; coding-tool widgets allowlist the key. |
| TUI | none | `Unit:"%"` metrics already render as gauges via `MetricUsedPercent`; no rendering code changes. |
| config | none | No new config. Always-on, additive metric. |
| detect | none | — |
| daemon | none | Inherits the telemetry projection change. |
| telemetry | minor | `usage_view_projection.go` window loop computes the ratio centrally for all telemetry providers. |
| CLI | none | — |

### Existing Design Doc Overlap

None directly. Touches the same projection surface as
`MODEL_NORMALIZATION_DESIGN.md` and `UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md`
(both feed `usage_view`), but does not change their behavior. Reuses the
cache-token columns established by `COPILOT_TELEMETRY_INTEGRATION_DESIGN.md` and
the tiered-retention rollups (`TELEMETRY_TIERED_RETENTION_DESIGN.md`), which
already `SUM` cache columns.

## 5. Detailed Design

### 5.1 Definition

Prompt tokens split into three buckets that all providers with caching report:

- **input** — non-cached prompt tokens (`TokenUsage.InputTokens`)
- **cache-read** — prompt tokens served from cache, i.e. a hit
  (`TokenUsage.CacheReadTokens`)
- **cache-write / creation** — prompt tokens written to cache on a miss
  (`TokenUsage.CacheWriteTokens`)

```
cache_hit_ratio = cache_read / (input + cache_read + cache_write)   (× 100)
```

"Of all prompt tokens this window, what fraction were served from cache." This
matches Anthropic's own *cache hit rate* and the OpenAI
`prompt_tokens_details.cached_tokens` convention. Output and reasoning tokens
are excluded (they are never cacheable on the read side).

Rejected definitions:
- `cache_read / (cache_read + cache_write)` — "reads vs writes" conflates a
  cold session (many writes) with a non-cacheable workload. Misleading.
- request-count-weighted — we lack reliable per-request hit flags across all
  providers; token-weighting is both available and more honest.

For providers that report cache-read but **not** cache-write (e.g.
`gemini_cli`), `cache_write` is `0` and the denominator becomes
`input + cache_read`. This reads slightly high versus a full read+write split;
documented as a known asymmetry.

### 5.2 Shared helper (`internal/core/`)

A single pure function keeps the definition in one place and unit-testable:

```go
// CacheHitRatio returns the token-weighted cache hit ratio as a percentage
// (0..100) and whether it is defined. The denominator is the prompt-side
// token volume: non-cached input + cache reads + cache writes. Output and
// reasoning tokens are excluded. ok is false when the denominator is zero.
func CacheHitRatio(input, cacheRead, cacheWrite float64) (pct float64, ok bool) {
    denom := input + cacheRead + cacheWrite
    if denom <= 0 {
        return 0, false
    }
    pct = cacheRead / denom * 100
    if pct < 0 {
        pct = 0
    }
    if pct > 100 {
        pct = 100
    }
    return pct, true
}
```

`ok` is false when there is **no cache activity at all** (`cacheRead+cacheWrite==0`)
so a model/provider that never touches the cache reports nothing rather than a
noisy "0% cached". A cold cache (writes but no reads yet) still reports 0%.

A small sibling builds the metric so emission is identical everywhere:

```go
// CacheHitRatioMetric builds the standard cache_hit_ratio Metric, or returns
// (zero, false) when the ratio is undefined.
func CacheHitRatioMetric(input, cacheRead, cacheWrite float64, window string) (Metric, bool) {
    pct, ok := CacheHitRatio(input, cacheRead, cacheWrite)
    if !ok {
        return Metric{}, false
    }
    remaining := 100 - pct
    limit := 100.0
    return Metric{Used: &pct, Remaining: &remaining, Limit: &limit, Unit: "%", Window: window}, true
}
```

The metric key is the unprefixed string `"cache_hit_ratio"`. It must **not**
carry a `today_`/`7d_` prefix, because coding-tool widgets hide those prefixes
(`claude_code/widget.go:21-24`).

### 5.3 Central computation — telemetry / daemon mode

`internal/telemetry/usage_view_projection.go` already loops the per-model
aggregates and sums `windowCacheRead` (line ~254). `telemetryModelAgg` already
carries `InputTokens`, `CacheReadTokens`, `CacheWriteTokens`. Extend the loop:

```go
var windowRequests, windowCost, windowBillable, windowCacheRead float64
var windowInput, windowCacheWrite float64
for _, model := range agg.Models {
    windowRequests += model.Requests
    windowCost += model.CostUSD
    windowBillable += model.BillableTokens
    windowCacheRead += model.CacheReadTokens
    windowInput += model.InputTokens
    windowCacheWrite += model.CacheWriteTokens
}
...
if m, ok := core.CacheHitRatioMetric(windowInput, windowCacheRead, windowCacheWrite, windowLabel); ok {
    snap.Metrics["cache_hit_ratio"] = m
}
```

This single change lights up **all telemetry providers** (`claude_code`,
`codex`, `opencode`, `cursor`, `gemini_cli`, `copilot`, `ollama`) with a
window-scoped ratio, because they all hydrate through this projection.
`ollama` (no caching) naturally yields a zero denominator and emits nothing.

### 5.4 Direct mode — per provider

Direct mode bypasses the telemetry projection; each provider's `Fetch`
produces the snapshot. We add emission where the input/read/write totals
already exist:

- **`claude_code`** (the issue's named use case) —
  `conversation_usage_projection.go` already emits `7d_input_tokens`,
  `7d_cache_read_tokens`, `7d_cache_create_tokens` from `p.weekly*` fields.
  Emit a single headline `cache_hit_ratio` from the rolling-7d totals (the
  provider's most stable window, matching `usage_seven_day`). Reuse the same
  `p.weekly*` accumulators — no new parsing.
- **`codex`** — emits `session_cached_tokens`; compute the ratio from the same
  session aggregate's input + cached (+ cache-write if present).
- **`openrouter`** — `generations.go` emits `today_input_tokens` and
  `today_cached_tokens`; compute `cache_hit_ratio` from those (window "today";
  openrouter has no cache-write so denominator is input + cached).

`opencode`, `cursor`, `gemini_cli`, `copilot` are telemetry-only in practice
(no standalone direct snapshot of token totals), so they get the ratio via 5.3.

### 5.5 TUI surfacing — no rendering changes

`MetricUsedPercent` returns `*Used` directly for any `Unit:"%"` metric
(`metric_semantics.go`), and `buildTileGaugeLines` renders any gauge-eligible
key. To make `cache_hit_ratio` appear we only edit widget config:

1. Append `"cache_hit_ratio"` to each coding-tool provider's
   `WithGaugePriority(...)` allowlist (`claude_code`, `codex`, `copilot`,
   `cursor`, `gemini_cli`, `opencode` widget files).
2. Add a shared label once in `internal/providers/shared` code-stats label maps:
   `"cache_hit_ratio" → "Cache Hit"` (and a compact `"cache hit"`), so every
   coding-tool widget inherits the label via `CodingToolDashboard`.
3. Add `cache_hit_ratio` to a compact "Tokens"/"Cache" row where space allows
   (per provider, optional polish).

No changes to `tiles_gauge.go`, `gauge.go`, `detail.go`, or
`provider_widget.go`.

Additionally, the per-model **Token Breakdown** table (which already shows raw
`cache.r`/`cache.w` token counts) gets a header annotation `· N% cached`
derived from the aggregate of its own rows (`renderModelTokenBreakdown` in
`tiles_composition.go`). The gauge gives the at-a-glance percentage; the
annotation puts the same number where the raw counts already live.

### 5.6 Backward Compatibility

Purely additive. No config, schema, or interface changes. Existing snapshots
without the key render exactly as before. The metric only appears when a
provider's window has prompt-cache activity, so quiet/incapable providers are
unaffected. Stored telemetry needs no migration — the source columns already
exist and are already summed by rollups.

## 6. Alternatives Considered

### Per-provider computation only (no central projection)
Rejected: would duplicate the formula across 7 telemetry providers and miss the
window-scoping that `usage_view` already does for free. The central site covers
them in one edit.

### Generic post-Fetch scanner that synthesizes the ratio from emitted metrics
Rejected: direct-mode providers emit non-uniform keys (`session_cached_tokens`
vs `7d_cache_read_tokens` vs `today_cached_tokens`) with no consistent input
denominator key, so a scanner would be fragile and silently wrong.

### Cost-savings percentage instead of token coverage
Deferred (non-goal). Useful but requires per-provider cache-read discount rates;
token coverage is the standard "hit ratio" and needs no pricing model.

## 7. Implementation Tasks

### Task 1: Shared cache-hit-ratio helper
Files: `internal/core/cache_hit_ratio.go`, `internal/core/cache_hit_ratio_test.go`
Depends on: none
Description: Add `CacheHitRatio(input, cacheRead, cacheWrite float64) (float64, bool)` and `CacheHitRatioMetric(..., window string) (Metric, bool)` per §5.2. Clamp to [0,100]; return `ok=false` on zero denominator.
Tests: Table-driven — zero denom (ok=false), read-only, read+write, write-only (0%), all-cached (100%), clamping, typical mid value.

### Task 2: Central telemetry projection metric
Files: `internal/telemetry/usage_view_projection.go`, `internal/telemetry/usage_view_projection_test.go`
Depends on: Task 1
Description: Extend the window aggregate loop (§5.3) to sum `windowInput` and `windowCacheWrite`, then emit `snap.Metrics["cache_hit_ratio"]` via `core.CacheHitRatioMetric`. Window label = existing `windowLabel`.
Tests: Build an `agg` with known per-model input/read/write, assert `cache_hit_ratio.Used` equals the expected percentage and `Unit=="%"`; assert absent when all caches are zero.

### Task 3: claude_code direct-mode metric
Files: `internal/providers/claude_code/conversation_usage_projection.go`, `internal/providers/claude_code/conversation_usage_projection_test.go`
Depends on: Task 1
Description: After the existing `7d_*` emissions, emit headline `cache_hit_ratio` from the rolling-7d input/cache-read/cache-create accumulators using `core.CacheHitRatioMetric` (window "rolling 7 days").
Tests: Feed known weekly totals; assert ratio value, unit, and absence when no cache activity.

### Task 4: codex direct-mode metric
Files: `internal/providers/codex/` (the projection that emits `session_cached_tokens`), plus its `_test.go`
Depends on: Task 1
Description: Compute `cache_hit_ratio` from the session input + cached (+ cache-write if available) totals. Window "session".
Tests: Known session aggregate → expected ratio; absent when no cache.

### Task 5: openrouter direct-mode metric
Files: `internal/providers/openrouter/generations.go`, `internal/providers/openrouter/*_test.go`
Depends on: Task 1
Description: Emit `cache_hit_ratio` from `today_input_tokens` + `today_cached_tokens` (no cache-write). Window "today".
Tests: Known today totals → expected ratio; absent when zero.

### Task 6: Widget surfacing for coding-tool providers
Files: `internal/providers/shared/` (code-stats label maps), `internal/providers/{claude_code,codex,copilot,cursor,gemini_cli,opencode}/widget.go`
Depends on: none (can run alongside 1-5; metric just won't show until emitted)
Description: Add shared label `cache_hit_ratio → "Cache Hit"` (+ compact label) once in `shared`. Append `"cache_hit_ratio"` to each provider's `WithGaugePriority`. Optionally add to a compact row.
Tests: Per-provider widget test asserting `cache_hit_ratio` is in `GaugePriority` and has a label override. (`opencode` may not use `widget.go`; verify and adjust.)

### Task 7: Integration verification + docs
Files: `docs/site/docs/` (cache hit ratio mention on the relevant providers/metrics page), this design doc
Depends on: Tasks 1-6
Description: Run `make build` + `make test`; run `make demo` to eyeball the gauge; update user-facing docs and build the docs site. Confirm the metric appears for claude_code in both modes.
Tests: Full `make test`; docs site `DOCS_PREVIEW=1 npm run build` → `[SUCCESS]`, no broken links.

### Dependency Graph

```
Task 1 (helper) → Tasks 2, 3, 4, 5 (parallel group: each emits the metric in a different place)
Task 6 (widgets) — independent, parallel with 1-5
Task 7 (integration + docs) — depends on all
```
