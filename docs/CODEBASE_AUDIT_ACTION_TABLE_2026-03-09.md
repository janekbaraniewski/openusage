# Codebase Audit Action Table

Date: 2026-03-09
Repository: `/Users/janekbaraniewski/Workspace/priv/openusage`
Branch: `feat/dashboard-race-parser-cleanups`

## Fixed in This Branch

| ID | Status | Area | Evidence | Resolution | Follow-up |
| --- | --- | --- | --- | --- | --- |
| R57 | Fixed | Account config contract hardening | `internal/core/provider.go`, `internal/config/config.go`, `internal/daemon/source_collectors.go`, `internal/detect/cursor.go`, `internal/detect/claude_code.go` | Provider-local runtime paths now live behind `ProviderPaths` and `Path`/`SetPath` helpers. Config load normalizes legacy `paths` payloads into the new field, and daemon/detect flows consume the typed path accessors instead of ad hoc provider-specific overloads. | Retain legacy `paths` read compatibility until the persisted config shape can be fully simplified. |
| R58 | Fixed | TUI settings/detail decomposition | `internal/tui/settings_modal.go`, `internal/tui/settings_modal_input.go`, `internal/tui/detail.go`, `internal/tui/detail_metrics.go`, `internal/tui/detail_analytics_sections.go` | Settings input/update logic and large detail metric/render sections are split out of the remaining coordinator files. The hot TUI files now separate state/input from section rendering much more cleanly. | Only split further if new features start coupling unrelated flows again. |
| R59 | Fixed | Detail and analytics metric decoding cleanup | `internal/core/analytics_costs.go`, `internal/core/usage_breakdowns_domains.go`, `internal/tui/detail.go`, `internal/tui/detail_analytics_sections.go`, `internal/tui/model_display_info.go` | Remaining burn-rate, language, MCP, and model-cost detection paths now go through shared core helpers instead of renderer-owned metric-prefix checks. UI code consumes shared semantic helpers rather than decoding raw key conventions inline. | Keep new metric-schema additions in `internal/core`, not in TUI renderers. |
| R60 | Fixed | Render-path caching follow-through | `internal/tui/render_cache.go`, `internal/tui/analytics_cache.go`, `internal/tui/tiles_cache.go`, `internal/tui/model_input.go`, `internal/tui/model_commands.go`, `internal/tui/dashboard_views.go` | Tile, analytics, and detail render paths are now explicitly invalidated on snapshot, window, theme, layout, and selection changes. Detail rendering is cached the same way analytics and tile composition already were, closing the remaining hot-path rebuild gap. | Profile before adding any more caching layers. |
| R61 | Fixed | Gemini CLI provider decomposition | `internal/providers/gemini_cli/gemini_cli.go`, `internal/providers/gemini_cli/api_usage.go`, `internal/providers/gemini_cli/session_usage.go` | API/quota/account flows and local session aggregation are split out of the coordinator file. The main provider file is now mostly wiring plus fetch orchestration. | Keep future Gemini changes inside the matching helper unit. |
| R62 | Fixed | Ollama provider decomposition follow-through | `internal/providers/ollama/ollama.go`, `internal/providers/ollama/local_api.go`, `internal/providers/ollama/cloud_api.go`, `internal/providers/ollama/desktop_db.go`, `internal/providers/ollama/desktop_db_settings.go`, `internal/providers/ollama/desktop_db_tokens.go`, `internal/providers/ollama/desktop_db_breakdowns.go` | Ollama’s coordinator, local API, cloud API, and desktop SQLite flows are now separated by concern. The remaining large desktop DB path is split into settings/schema helpers, token estimation, and usage breakdown/daily series helpers. | Keep future SQLite-specific work inside the dedicated desktop DB helper files. |
| R63 | Fixed | Telemetry and config fixture cleanup | `internal/telemetry/test_helpers_test.go`, `internal/telemetry/usage_view_test.go`, `internal/config/test_helpers_test.go` | Shared store/file helpers now cover the repeated setup patterns in the telemetry and config suites, and `usage_view_test.go` is reduced below the previous monolith threshold. | Apply the same helper pattern to other large suites when they next change. |

## Residual Non-Blocking Follow-Up

These are no longer review blockers or known correctness issues. They are explicit maintenance opportunities left after the main cleanup.

| ID | Priority | Area | Evidence | Current state | Optional follow-up |
| --- | --- | --- | --- | --- | --- |
| A6 | P3 | Telemetry usage-view orchestration | `internal/telemetry/usage_view.go`, `internal/telemetry/usage_view_projection.go`, `internal/telemetry/usage_view_materialize.go`, `internal/telemetry/usage_view_aggregate.go` | The usage-view path is materially decomposed and validated. The remaining top-level coordinator is acceptable and no longer a review issue. | Split further only if future telemetry changes start re-coupling query planning, cache application, and projection. |
| A7 | P3 | Daemon service follow-through | `internal/daemon/server.go`, `internal/daemon/server_collect.go`, `internal/daemon/server_poll.go`, `internal/daemon/server_spool.go`, `internal/daemon/server_http.go`, `internal/daemon/server_read_model.go` | Daemon loops and HTTP/read-model flows are already separated, and no new race or lifecycle bug was found in the follow-up review. | Add extra worker abstractions only if future concurrency pressure justifies them. |
| A8 | P3 | Ambiguous local-source attribution | `internal/daemon/source_collectors.go`, `internal/daemon/server_http.go`, `cmd/openusage/telemetry.go` | Ambiguous shared-path local sources still intentionally require explicit user disambiguation instead of silent guessing. This is a product decision, not a hidden bug. | Add persisted source/account aliasing only if multi-account shared-path workflows become common. |

## Summary

- The original high-risk review items `A1`, `A2`, `A3`, `A4`, `A12`, `A14`, and `A15` are addressed in this branch.
- No additional high-confidence correctness bug was found during the follow-up review after the dashboard timeframe race fix.
- Remaining entries are intentional tradeoffs or low-priority structural opportunities, not outstanding breakages.
