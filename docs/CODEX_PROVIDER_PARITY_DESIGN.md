# Codex Provider Cursor-Parity Design

Date: 2026-02-26
Status: Implemented (runtime) / Demo parity in progress
Author: janekbaraniewski + Codex

## 1. Problem Statement

The Codex provider previously exposed only a narrow subset of session and limit data. Compared to the Cursor provider tile, Codex lacked:

1. Comparable composition sections (clients, tool usage, language, code statistics).
2. Cursor-compatible compact rows and metric aliases.
3. Daily trend series for model/client/request/token views.
4. A clear distinction between direct data and inferred estimates.
5. A demo snapshot that structurally mirrors the real Codex tile.

## 2. Goals

1. Make Codex provider/widget output structurally equivalent to the Cursor-style dashboard layout.
2. Expose all major Codex usage dimensions in one tile:
   - limits, model burn, clients, tools, language, code stats, compact rows.
3. Preserve compatibility with existing TUI behavior by emitting Cursor-compatible aliases.
4. Add daily series needed for trend sparklines/charts.
5. Keep account identifiers private in demo fixtures.

## 3. Non-Goals

1. Fabricating authoritative API limits not returned by Codex itself.
2. Replacing Codex source-of-truth with fully derived synthetic limit percentages.
3. Changing global dashboard rendering rules for other providers.

## 4. Implemented Design

### 4.1 Provider Data Extraction (`internal/providers/codex/codex.go`)

Codex now merges two sources:

1. Local session JSONL (`~/.codex/sessions/...`) for rich activity signals.
2. Live usage endpoint (`/wham/usage` / `/api/codex/usage`) for current limit windows and account metadata.

New extraction paths emit:

1. Model usage metrics:
   - `model_*_{input,output,cached,reasoning,total}_tokens`
   - `usage_model_*` daily series
2. Client usage metrics:
   - `client_*_{total,input,output,cached,reasoning}_tokens`
   - `client_*_{requests,sessions}`
   - `usage_client_*` and `usage_source_*` daily series
3. Interface metrics:
   - `interface_*` request buckets for CLI/Desktop/IDE/Cloud/Human-style groupings
4. Tool usage metrics:
   - `tool_<name>`
   - aggregates: `tool_calls_total`, `tool_completed`, `tool_errored`, `tool_cancelled`, `tool_success_rate`
5. Language usage metrics:
   - `lang_*` request counts
6. Code statistics metrics:
   - `composer_lines_added`, `composer_lines_removed`, `composer_files_changed`
   - `scored_commits`, `total_prompts`, `ai_code_percentage`
   - `ai_deleted_files`, `ai_tracked_files`
7. Request/session compatibility metrics:
   - `total_ai_requests`, `composer_requests`
   - `requests_today`, `today_composer_requests`
   - `composer_sessions`, `composer_context_pct`
8. Daily totals:
   - `analytics_tokens`, `analytics_requests`
   - aliases: `tokens_total`, `requests`

### 4.2 Cursor-Compatibility Aliases

`applyCursorCompatibilityMetrics` adds alias behavior so Codex fits existing compact rows and gauge logic:

1. `rate_limit_primary` -> `plan_auto_percent_used`
2. `rate_limit_secondary` -> `plan_api_percent_used`
3. Derived `plan_percent_used` from max(primary, secondary)
4. `context_window` -> `composer_context_pct` (if missing)
5. Raw `credit_balance` -> metric `credit_balance` (USD)
6. Request aliases between `total_ai_requests/composer_requests` and `requests_today/today_composer_requests`

### 4.3 Widget Parity (`internal/providers/codex/widget.go`)

Codex dashboard widget now mirrors Cursor-style composition:

1. `ShowClientComposition = true`
2. `ClientCompositionIncludeInterfaces = true`
3. `ShowActualToolUsage = true`
4. `ShowLanguageComposition = true`
5. `ShowCodeStatsComposition = true`
6. `ShowToolComposition = false` (keep separate actual-tool panel)
7. Code stats slot mapping uses Codex metric keys.
8. Compact rows align with Cursor-style `Credits/Team/Usage/Activity/Lines`.
9. Prefix/key hiding rules suppress noisy raw metric families once rendered as sections.

### 4.4 TUI Support for Codex Trends (`internal/tui/tiles.go`)

`collectInterfaceAsClients` was updated to consume:

1. `usage_client_*` daily series directly.
2. `usage_source_*` daily series as fallback, normalized into client buckets.

This enables client trend sparklines for Codex when interface composition mode is active.

## 5. Data Semantics

### 5.1 Direct (authoritative) metrics

Direct metrics are read from Codex events/API without estimation:

1. `rate_limit_*` percentages and reset times.
2. Session token counters (`session_*`, `context_window`).
3. Raw token deltas and per-model/per-client totals from JSONL.
4. Request/session counts from observed events.
5. Live account metadata (`plan_type`, account identifiers, credits presence).

### 5.2 Inferred/heuristic metrics

The following are computed heuristically from observed actions:

1. `lang_*` (from command/file-extension inference).
2. Code patch stats (`composer_lines_*`, `composer_files_changed`).
3. `scored_commits` (from command detection).
4. `ai_code_percentage` (patch-call ratio heuristic).

These are intentionally useful but not canonical API truth.

## 6. Known Source Limitation: Stuck 5h Primary Usage

Observed in live Codex session events:

1. `total_tokens` can increase materially while `rate_limits.primary.used_percent` remains `0.0`.
2. `secondary.used_percent` may remain `100.0` across the same period.

Design implication:

1. The tile currently reflects source-reported limit percentages as-is.
2. If Codex does not update `primary.used_percent`, the 5h gauge will appear static even during active usage.

Future mitigation option (not yet implemented):

1. Add an explicit derived `~5h` estimate metric from token deltas as a fallback visualization, while preserving raw source fields separately.

## 7. Demo Requirements (Codex)

The demo snapshot should match real Codex section structure and key families while staying synthetic:

1. Include all major section-driving keys used by the runtime tile.
2. Keep `Raw` identity fields anonymized (non-real email/account identifiers).
3. Randomize numeric values per run, including daily series, without changing key presence.
4. Preserve trend keys:
   - `usage_model_*`, `usage_client_*` / `usage_source_*`
   - `analytics_tokens`, `analytics_requests`
5. Preserve compatibility aliases used by compact rows/gauges.

## 8. Impacted Files

| File | Purpose |
|------|---------|
| `internal/providers/codex/codex.go` | session/API parsing, aliases, trends, model/client/tool/language/code metrics |
| `internal/providers/codex/widget.go` | Cursor-like section/compact-row config for Codex |
| `internal/providers/codex/codex_test.go` | codex extraction + alias + widget parity regression tests |
| `internal/tui/tiles.go` | interface-as-client trend support for `usage_client_*` / `usage_source_*` |
| `cmd/demo/main.go` | codex demo fixture parity/anonymization (in progress) |
| `cmd/demo/main_test.go` | demo codex coverage assertions (to be aligned with final fixture keys) |

## 9. Validation Strategy

1. Provider unit tests:
   - `go test ./internal/providers/codex -v`
2. TUI unit tests:
   - `go test ./internal/tui -v`
3. Full suite smoke:
   - `go test ./...`
4. Manual source-verification:
   - Compare provider snapshot values to raw `~/.codex/sessions/*.jsonl` token_count events and live usage payload fields.

## 10. Rollout Notes

1. Runtime Codex provider parity is implemented and validated by tests.
2. Demo codex parity work should be finalized so `cmd/demo` exposes the same section-driving keys with anonymized identity fields and randomized values.
