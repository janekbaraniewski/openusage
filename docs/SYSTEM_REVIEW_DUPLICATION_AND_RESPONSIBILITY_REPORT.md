# System Review: Remaining Responsibility and Duplication Gaps

Date: 2026-03-09
Repository: `/Users/janekbaraniewski/Workspace/priv/openusage`

## Scope

This is a refreshed architecture review after the dashboard race fix, daemon/read-model cleanup, provider parser consolidation, and the recent Cursor/OpenRouter/Ollama/TUI refactors on branch `feat/dashboard-race-parser-cleanups`.

The goal of this report is not to restate already-fixed issues. It documents the meaningful problems still left in the current tree.

## What Is No Longer Open

These were major concerns in earlier reviews and are now materially addressed:

- Dashboard timeframe race and stale snapshot acceptance.
- Read-model cache dedupe ignoring time window.
- Stringly typed daemon/telemetry time-window flow.
- Telemetry source account binding for unambiguous local collectors and hooks.
- Cursor parser/SQLite duplication across dashboard and telemetry paths.
- Codex and Claude Code raw parser duplication.
- OpenRouter provider-resolution, analytics, generation, projection, and account-path monolith sprawl.
- TUI side-effect leakage into config persistence / integration install / provider validation.
- Ollama hot-path `time.Now()` usage in behavioral window/reset logic.
- Shared hook ingest parsing/local fallback drift between daemon and CLI.
- Usage-view temp-table materialization and aggregate query fanout living inline in the main orchestration path.

## Findings

### 1. [P2] TUI rendering and state handling are still concentrated in a few very large files

The TUI is much better than before, and provider tile display-summary logic no longer lives inline in `model.go`, but [model.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/model.go), [detail.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/detail.go), [tiles_composition.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/tiles_composition.go), and [settings_modal.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/settings_modal.go) are still large enough that unrelated concerns move together.

Refs:
- [model.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/model.go)
- [model_display_info.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/model_display_info.go)
- [detail.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/detail.go)
- [tiles_composition.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/tiles_composition.go)
- [settings_modal.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/settings_modal.go)

What to address:
- Continue section-level file extraction from `detail.go`.
- Split model orchestration further by update/action/display boundaries.
- Push more typed extractor work out of rendering code.

### 2. [P2] Some analytics/detail sections still decode raw metric-key conventions in UI code

The major composition paths, provider tile fallback/rate-limit selection, and token-table paths now use shared extractors, but analytics/detail still contain pockets of renderer-owned key interpretation. That is better than before, but it is still a drift vector.

Refs:
- [analytics.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/analytics.go)
- [detail.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/detail.go)
- [usage_breakdowns.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/core/usage_breakdowns.go)
- [analytics_snapshot.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/core/analytics_snapshot.go)
- [dashboard_display_metrics.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/core/dashboard_display_metrics.go)

What to address:
- Promote remaining analytics/detail extractors into `internal/core`.
- Keep renderers as display adapters over typed sections.

### 3. [P2] Telemetry usage-view orchestration is smaller, but still centralized

The usage-view path is much cleaner after helper, projection, query, materialization, and aggregate-fanout splits, but the top-level file still coordinates source selection, cache/application flow, and final snapshot application in one place.

Refs:
- [usage_view.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/usage_view.go)
- [usage_view_materialize.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/usage_view_materialize.go)
- [usage_view_aggregate.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/usage_view_aggregate.go)
- [usage_view_projection.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/usage_view_projection.go)

What to address:
- Keep future telemetry work inside the split helper units.
- Only split the remaining coordinator path further if new behavior starts coupling unrelated concerns again.

### 4. [P2] Several providers are still large mixed-responsibility units

Cursor and OpenRouter are now in much better shape, but several other providers remain monoliths that mix transport, parsing, normalization, and projection in one place.

Refs:
- [ollama.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/ollama/ollama.go)
- [zai.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/zai/zai.go)
- [gemini_cli.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/gemini_cli/gemini_cli.go)
- [copilot.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/copilot/copilot.go)
- [claude_code.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/claude_code/claude_code.go)
- [codex.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/codex/codex.go)

What to address:
- Split by concern, not by arbitrary line count:
- account/API fetch
- local-data adapters
- projection helpers
- telemetry-specific collectors

### 5. [P3] Ambiguous shared-path local sources still require explicit account disambiguation

The daemon now binds local telemetry to configured accounts when the source/account mapping is unambiguous. If multiple accounts share the same source paths, it intentionally degrades to source-scoped attribution instead of silently guessing. That is the correct behavior today, but it means truly ambiguous local multi-account setups still need an explicit binding mechanism if they become a first-class use case.

Refs:
- [source_collectors.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/daemon/source_collectors.go)
- [server_http.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/daemon/server_http.go)
- [telemetry.go](/Users/janekbaraniewski/Workspace/priv/openusage/cmd/openusage/telemetry.go)

What to address:
- Add persisted source/account alias mapping only if ambiguous local multi-account setups become common.
- Keep ambiguous attribution explicit; do not reintroduce silent account guessing.

### 6. [P3] Account config contract cleanup is not finished

The hot-path abuse of `Binary`/`BaseURL` is fixed, but the type still allows path-like runtime hints and canonical provider config to coexist ambiguously.

Refs:
- [provider.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/core/provider.go)
- [config.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/config/config.go)

What to address:
- Introduce a dedicated typed runtime-hints structure.
- Retire compatibility comments and residual semantic ambiguity in `AccountConfig`.

### 7. [P3] Test suites are strong but still expensive to maintain

Some package tests remain extremely large and inline too much fixture logic.

Refs:
- [openrouter_test.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/openrouter/openrouter_test.go)
- [copilot_test.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/copilot/copilot_test.go)
- [usage_view_test.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/usage_view_test.go)
- [config_test.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/config/config_test.go)

What to address:
- Extract fixture builders and scenario helpers.
- Keep top-level tests declarative.

## Recommended Order

1. TUI extractor/decomposition follow-through.
2. Telemetry and TUI decomposition follow-through.
3. Remaining provider monolith splits.
4. Telemetry account identity mapping and daemon follow-through.
5. Account config contract hardening.
6. Test fixture cleanup.

## Notes

- The repo is in materially better shape than it was at the start of this cleanup branch.
- The main remaining risks are now architectural and maintainability-oriented rather than immediate correctness regressions.
- The highest near-term drift risk is the remaining metric-prefix parsing still sitting in TUI render code plus the size of the remaining TUI/provider units.
