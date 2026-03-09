# System Review: Post-Cleanup State

Date: 2026-03-09
Repository: `/Users/janekbaraniewski/Workspace/priv/openusage`
Branch: `feat/dashboard-race-parser-cleanups`

## Scope

This report reflects the tree after the dashboard timeframe-race fix, parser consolidation work, daemon/read-model cleanup, provider decomposition, TUI decomposition, render-cache follow-through, and the final `A1`/`A2`/`A3`/`A4`/`A12`/`A14`/`A15` cleanup pass.

It replaces the earlier “remaining gaps” snapshot. The goal now is to document the actual post-cleanup state, not to preserve stale open items.

## What Is Resolved

The following earlier review themes are materially closed in this branch:

- Dashboard timeframe race and stale snapshot acceptance.
- Read-model cache dedupe ignoring time window.
- Stringly typed daemon/telemetry time-window flow.
- Parser duplication across Cursor, Codex, and Claude Code dashboard/telemetry paths.
- OpenRouter, Cursor, Claude Code, Codex, Copilot, OpenCode, Z.AI, Gemini CLI, and Ollama monolith concentration in their previously hottest paths.
- TUI side-effect leakage into persistence, integration install, and provider validation.
- Major TUI composition concentration in tile/detail/settings code.
- Remaining detail/analytics metric-prefix parsing pockets that were still living in renderer code.
- Tile/detail/analytics render-path recomputation on every frame.
- Account-config runtime-path overload in the hot path.
- Repeated telemetry/config test setup boilerplate in the most actively changed suites.

## Current Findings

### 1. No remaining high-confidence correctness bug surfaced in the follow-up review

After the final cleanup pass and validation run, I did not find another issue on the level of the original dashboard timeframe race. The remaining items are not hidden state-corruption or concurrency defects; they are explicit maintenance tradeoffs.

Validation used for this state:

- `go test ./...`
- `go vet ./...`
- `make build`

### 2. The codebase now has clearer responsibility boundaries in the hot areas

The most change-prone areas are no longer concentrated the way they were at the start of the branch:

- TUI render/state work is split across dedicated settings/detail/cache/helper units.
- Provider-local parsing and fetch logic are split by concern in the previously worst provider files.
- Daemon hook ingest, HTTP, polling, spool, and read-model paths are separated.
- Telemetry usage-view query/materialization/projection/aggregate logic is separated.

This reduces review blast radius and makes future concurrency/data-flow work easier to reason about.

### 3. Residual items are explicit, low-risk follow-up opportunities

There are still a few non-blocking areas worth keeping in mind:

- `usage_view.go` still owns top-level orchestration, but it is no longer a monolith and does not currently hide a correctness issue.
- The daemon could be pushed into more formal worker abstractions later, but present lifecycle/context handling is consistent in the active paths.
- Ambiguous shared-path local account attribution still requires explicit user disambiguation by design; the code now avoids silent guessing.

These are not “unfinished fixes”. They are optional future design work.

## References

- [CODEBASE_AUDIT_ACTION_TABLE_2026-03-09.md](/Users/janekbaraniewski/Workspace/priv/openusage/docs/CODEBASE_AUDIT_ACTION_TABLE_2026-03-09.md)
- [internal/tui/render_cache.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/render_cache.go)
- [internal/tui/detail_metrics.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/detail_metrics.go)
- [internal/tui/settings_modal_input.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/tui/settings_modal_input.go)
- [internal/providers/ollama/desktop_db.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/ollama/desktop_db.go)
- [internal/providers/ollama/desktop_db_tokens.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/ollama/desktop_db_tokens.go)
- [internal/providers/gemini_cli/api_usage.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/providers/gemini_cli/api_usage.go)
- [internal/core/provider.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/core/provider.go)
- [internal/telemetry/test_helpers_test.go](/Users/janekbaraniewski/Workspace/priv/openusage/internal/telemetry/test_helpers_test.go)

## Bottom Line

- The original review’s high-priority structural set is addressed.
- The repo is in materially better shape than at the start of the branch.
- Remaining items are optional follow-up architecture choices, not outstanding bugs from the review.
