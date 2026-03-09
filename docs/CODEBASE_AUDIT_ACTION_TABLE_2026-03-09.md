# Codebase Audit Action Table

Date: 2026-03-09
Repository: `/Users/janekbaraniewski/Workspace/priv/openusage`

## Scope

This pass combined:

- full test run: `go test ./...`
- targeted race run: `go test -race ./internal/daemon ./internal/telemetry ./internal/tui ./cmd/openusage`
- repo-wide static scans for large files, goroutines, mutex usage, legacy markers, and duplicated metric-prefix parsing
- targeted reads of the highest-risk files and subsystems

This table captures every issue found in this pass. It is broad and high-signal, but it is still a static audit, not a proof that no additional edge-case bugs exist.

## Resolved In This Pass

| ID | Priority | Area | Evidence | Change made | Follow-up |
| --- | --- | --- | --- | --- | --- |
| R1 | Fixed | Dashboard timeframe race | `cmd/openusage/dashboard.go`, `internal/tui/model.go`, `internal/daemon/runtime.go` | Snapshot messages now carry `TimeWindow` and `RequestID`, and stale responses are rejected. | None. Keep regression tests. |
| R2 | Fixed | Daemon cache refresh bug | `internal/daemon/accounts.go`, `internal/daemon/server.go`, `internal/daemon/types.go` | Read-model cache refresh dedupe now keys by normalized time window instead of collapsing all windows together. | None. |
| R3 | Fixed | Weakly typed time-window flow | `internal/daemon/types.go`, `internal/daemon/runtime.go`, `internal/telemetry/read_model.go`, `internal/telemetry/usage_view.go` | Internal daemon and telemetry paths now use `core.TimeWindow` instead of raw strings. | Continue shrinking stringly typed config boundaries over time. |
| R4 | Fixed | Dashboard refresh orchestration sprawl | `cmd/openusage/snapshot_dispatcher.go`, `cmd/openusage/dashboard.go` | Snapshot sequencing/version dispatch moved out of dashboard wiring into a dedicated helper. | Reuse the same pattern if other async UI data channels are added. |
| R5 | Fixed | Legacy runtime path overload cleanup | `internal/core/provider.go`, `internal/config/config.go`, `internal/detect/cursor.go`, `internal/detect/claude_code.go`, `internal/providers/cursor/cursor.go`, `internal/providers/claude_code/claude_code.go` | Legacy provider-specific path overloads are normalized into `Paths`, and runtime provider code now uses named paths instead of normal-path dependence on `Binary` / `BaseURL`. | The type still contains `Binary` and `BaseURL` for legitimate CLI/base-URL providers. |
| R6 | Fixed | Repeated coding-tool detail widgets | `internal/core/detail_widget.go`, `internal/providers/cursor/cursor.go`, `internal/providers/codex/codex.go`, `internal/providers/claude_code/claude_code.go`, `internal/providers/copilot/copilot.go`, `internal/providers/gemini_cli/gemini_cli.go` | Repeated detail section arrays were replaced with a shared `CodingToolDetailWidget(...)` constructor. | Extend the same pattern if more coding-tool providers are added. |
| R7 | Fixed | TUI side-effect boundary | `internal/tui/model.go`, `internal/dashboardapp/service.go`, `cmd/openusage/dashboard.go` | `tui.Model` no longer directly persists settings, saves credentials, installs integrations, or validates API keys. Those side effects now go through an injected dashboard application service. | More UI decomposition is still useful, but the highest-leak side effects are no longer hardcoded in the model. |
| R8 | Fixed | Codex parser duplication | `internal/providers/codex/session_decoder.go`, `internal/providers/codex/codex.go`, `internal/providers/codex/telemetry_usage.go` | Codex session JSONL parsing now runs through one shared decoder used by both the dashboard breakdown reader and telemetry ingestion path. | Apply the same consolidation to Claude Code and Cursor. |
| R9 | Fixed | Claude Code parser duplication | `internal/providers/claude_code/conversation_records.go`, `internal/providers/claude_code/claude_code.go`, `internal/providers/claude_code/telemetry_usage.go` | Claude Code JSONL parsing, token total calculation, and usage/tool dedupe keys now run through one shared normalized conversation-record helper used by both the dashboard aggregator and telemetry collector. | Apply the same consolidation pattern to Cursor. |
| R10 | Fixed | Cursor state DB reader duplication | `internal/providers/cursor/state_records.go`, `internal/providers/cursor/cursor.go`, `internal/providers/cursor/telemetry.go` | Cursor `composerData` and `bubbleId` rows from `cursorDiskKV` are now parsed once into shared record types and projected from both the dashboard provider and telemetry collector. This also removes the extra telemetry pass that queried `bubbleId` separately for tool and token events. | Tracking DB and daily-stats duplication still remain. |
| R11 | Fixed | Detached read-model refresh ownership | `internal/daemon/server.go` | Async read-model cache refreshes triggered from HTTP handlers now inherit the daemon service root context instead of launching from `context.Background()`. | If a worker pool is added later, reuse the same service-owned context there too. |
| R12 | Fixed | Cursor tracking and daily-stats reader duplication | `internal/providers/cursor/tracking_records.go`, `internal/providers/cursor/cursor.go`, `internal/providers/cursor/telemetry.go` | Cursor `ai_code_hashes` rows and `ItemTable` daily-stats envelopes now parse through shared record loaders, including compatibility for older tracking DB schemas with missing columns. Dashboard and telemetry projections now read the same normalized source records. | Keep compatibility coverage for older Cursor schemas. |
| R13 | Fixed | Ad hoc daemon log throttling | `internal/core/log_throttle.go`, `internal/daemon/server.go`, `internal/daemon/runtime.go` | Daemon service and dashboard runtime now use a shared throttling helper instead of separate timestamp/mutex patterns for repeated log suppression. | Reuse the same helper if more throttled log sites are added. |
| R14 | Fixed | Cursor time-source injection | `internal/core/clock.go`, `internal/providers/cursor/cursor.go`, `internal/providers/cursor/tracking_records.go`, `internal/providers/cursor/telemetry.go` | Cursor provider and its shared SQLite readers now use an injectable clock path instead of direct `time.Now()` calls in the main time-sensitive flow. | Extend the same pattern to other provider/analytics subsystems over time. |

## Action Table

| ID | Priority | Area | Evidence | Issue | Recommended action | Expected payoff |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | P2 | Account config contract hardening | `internal/core/provider.go:31-43`, `internal/config/config.go:199-206` | Path overload dependence is removed from the hot runtime flow, but `Binary` / `BaseURL` still coexist in the same type and the distinction between CLI path vs provider-local path is still not encoded by type. | Introduce a dedicated typed runtime-hints/path struct and eventually retire path-related legacy comments/compatibility in `AccountConfig`. | Finishes the contract cleanup and makes misuse harder. |
| A2 | P2 | TUI/application decomposition follow-through | `internal/tui/model.go:393-584`, `internal/dashboardapp/service.go` | The side effects are now injected, but `Model` still owns a very large amount of event-handling and state-transition logic. | Continue splitting update/action logic into smaller TUI units and move more orchestration decisions into the dashboard application layer over time. | Lower UI complexity and smaller blast radius per change. |
| A3 | P1 | UI metric-prefix parsing | `internal/tui/tiles_composition.go:302-322`, `internal/tui/tiles_composition.go:913-1527`, `internal/tui/detail.go:371-432`, `internal/tui/analytics.go:663-729` | Rendering code is still parsing raw metric key conventions (`model_`, `usage_client_`, `usage_source_`, `mcp_`, `lang_`) directly. This duplicates interpretation logic across views. | Introduce typed composition DTOs in `internal/core` or `internal/telemetry`; renderers should consume structured sections rather than re-parse maps. | Removes a large class of UI drift bugs and reduces per-render work. |
| A4 | P1 | OpenRouter provider size | `internal/providers/openrouter/openrouter.go:307-2188` | `openrouter.go` mixes auth probing, credits, keys, analytics parsing, generation pagination, provider resolution, metadata enrichment, and output projection in one 2800+ LOC file. | Split into subpackages/files: `api_client`, `analytics`, `generations`, `provider_resolution`, `projection`, `types`. | Easier maintenance, smaller diff surface, faster targeted testing. |
| A5 | P1 | Cursor provider responsibility overload | `internal/providers/cursor/cursor.go:181-335`, `internal/providers/cursor/cursor.go:903-1006`, `internal/providers/cursor/cursor.go:1087-2086` | Cursor provider combines API orchestration, local SQLite readers, token extraction, and two independent caches in one class. | Split into `api`, `trackingdb`, `statedb`, `cache`, and `snapshot_projection` modules. Move token extraction out of provider hot path. | Cleaner boundaries and less risk of local/API logic regressions. |
| A6 | P1 | Telemetry usage-view monolith | `internal/telemetry/usage_view.go:160-1757` | `usage_view.go` is simultaneously query planner, SQL execution layer, aggregation engine, naming normalizer, and snapshot projection layer. | Split into `query_*`, `aggregate_*`, `projection_*`, and `mcp_*` units. Add a typed intermediate aggregation model. | Easier optimization and safer incremental changes. |
| A7 | P1 | Daemon service monolith | `internal/daemon/server.go:1-1211` | `server.go` owns service startup, socket server, polling, collection, retention, cache refresh, hook handling, and HTTP endpoints. | Split into `service_runtime`, `http_handlers`, `polling`, `collection`, `cache`, and `hook_ingest` files/types. | Lower mental load and easier concurrency review. |
| A11 | P2 | Time-dependent logic without injectable clock | `internal/providers/openrouter/openrouter.go:728`, `internal/providers/ollama/ollama.go:1088`, `internal/core/analytics_normalize.go:61-103` | Cursor’s main time-sensitive path now uses an injectable clock, but several other providers and analytics helpers still read `time.Now()` directly, often mixing local time and UTC. | Extend the clock abstraction to the remaining provider and analytics subsystems and standardize UTC/local semantics per provider. | Better determinism and fewer timezone edge cases. |
| A12 | P2 | Test file sprawl and fixture duplication | `internal/providers/openrouter/openrouter_test.go`, `internal/providers/copilot/copilot_test.go`, `internal/telemetry/usage_view_test.go`, `internal/config/config_test.go` | Some tests are 1000-2600 LOC and re-encode similar setup logic inline. They are valuable but expensive to navigate and update. | Extract fixture builders and scenario helpers per package. Keep top-level tests declarative. | Faster iteration and simpler maintenance of large test suites. |
| A14 | P3 | File-size based decomposition needed in TUI | `internal/tui/model.go`, `internal/tui/detail.go`, `internal/tui/settings_modal.go`, `internal/tui/tiles_composition.go` | TUI logic is split across files, but the files are still individually very large and mix event handling, rendering, and data interpretation. | Continue decomposition by concern: `model_update`, `model_actions`, `model_display`, `settings_actions`, `detail_sections`, `composition_extractors`. | Better readability and easier targeted refactors. |
| A15 | P3 | Performance optimization opportunity in render path | `internal/tui/model.go:441-450`, `internal/tui/tiles_composition.go:302-322`, `internal/tui/detail.go:752-1046`, `internal/tui/analytics.go:663-729` | The UI recomputes display/composition structures from raw metric maps repeatedly during rendering. It is correct, but the work is duplicated across views and frames. | Cache derived display/composition sections per snapshot update instead of rebuilding them in each view path. | Lower render cost and less duplicated parsing logic. |

## Suggested Execution Order

1. A2, A3
2. A6, A7
3. A4, A5
4. A1, A11
5. A12, A14, A15

## Notes

- The highest-risk remaining issues are architectural rather than immediately broken behavior.
- The biggest remaining drift risk is the metric-prefix parsing still spread across the TUI render path.
- The race pass completed cleanly for the core dashboard/daemon/telemetry packages after the timeframe fix.
