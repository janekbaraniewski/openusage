# MCP Usage Section Design

Date: 2026-03-05
Status: Proposed
Author: janekbaraniewski

## 1. Problem Statement

MCP tool usage is buried in the general "Tool Usage" list alongside native tools like `bash`, `read`, `edit`. There's no grouping by MCP server or breakdown by function, making it impossible to see which MCP servers are most used and what functions are called — either per-session or in aggregate.

## 2. Goals

1. Extract MCP tools from the tool usage list into a dedicated "MCP Usage" section on both the dashboard tile and the detail view.
2. Group MCP tools by server (e.g., `gopls`, `github`, `slack`) with per-function breakdowns.
3. Track MCP usage per session so users can see which sessions relied on which MCP servers.
4. Present the data visually using existing chart infrastructure (horizontal bar charts, dot-leader rows).

## 3. Non-Goals

1. Changing how MCP tools are collected or detected — the existing telemetry pipeline already captures them.
2. MCP server health/connectivity monitoring.
3. Adding new config options for MCP grouping or filtering.
4. Replacing or removing MCP tools from the existing "Tool Usage" section — they stay there too for total tool counts.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add `DetailSectionStyleMCP` constant, `MetricGroupMCP` constant |
| TUI | moderate | New `renderMCPSection()` in detail.go, new `buildMCPUsageLines()` in tiles.go |
| telemetry | moderate | New `queryMCPAgg()` query that groups by server/function, new agg struct, emit `mcp_*` metrics |
| providers | minor | Claude Code and other telemetry providers add MCP section to `DetailWidget()` |
| config | none | No config changes |
| detect | none | No detection changes |
| daemon | none | No collection changes |
| CLI | none | No command changes |

### Existing Design Doc Overlap

- **UNIFIED_AGENT_USAGE_TRACKING_DESIGN**: Complementary — defines the pipeline that collects tool events including MCP tools.
- **DETAIL_PAGE_REDESIGN_DESIGN**: Complementary — defines the detail view framework; MCP is a new section slot.

## 5. Detailed Design

### 5.1 MCP Tool Name Parsing

MCP tool names in the database preserve their original format from Claude Code:

```
mcp__gopls__go_diagnostics       → server: "gopls",       function: "go_diagnostics"
mcp__github__create_issue        → server: "github",      function: "create_issue"
mcp__claude_ai_vcluster_yaml_mcp__smart_query → server: "claude_ai_vcluster_yaml_mcp", function: "smart_query"
```

The raw `tool_name` in SQLite uses double underscores (`__`) to separate `mcp`, server name, and function name. The `sanitizeMetricID()` function collapses these to single underscores for metric keys, but the raw DB values are intact.

**Parsing function** (new, in `internal/telemetry/usage_view.go`):

```go
// parseMCPToolName extracts server and function from an MCP tool name.
// Returns ("", "", false) for non-MCP tools.
func parseMCPToolName(raw string) (server, function string, ok bool) {
    raw = strings.ToLower(strings.TrimSpace(raw))
    if !strings.HasPrefix(raw, "mcp__") {
        return "", "", false
    }
    rest := raw[5:] // strip "mcp__"
    idx := strings.Index(rest, "__")
    if idx < 0 {
        return rest, "", true // server only, no function
    }
    return rest[:idx], rest[idx+2:], true
}
```

### 5.2 Telemetry Aggregation

New SQL query in `usage_view.go` that groups MCP tools by server and function:

```go
type telemetryMCPAgg struct {
    Server   string
    Function string
    Calls    float64
    Calls1d  float64
}
```

The query uses `queryToolAgg()` results and post-processes in Go (since SQL can't easily parse `__` separators). Filter tool rows where `tool_name LIKE 'mcp__%'`, then use `parseMCPToolName()` to split.

New aggregation struct added to `telemetryUsageAgg`:

```go
type telemetryUsageAgg struct {
    // ... existing fields ...
    MCPServers []telemetryMCPServerAgg  // new
}

type telemetryMCPServerAgg struct {
    Server    string
    Calls     float64
    Calls1d   float64
    Functions []telemetryMCPAgg
}
```

### 5.3 Metric Emission

New metrics emitted in `applyCanonicalUsageViewWithDB()`:

```
mcp_<server>_total          → total calls to this MCP server
mcp_<server>_<function>     → calls to specific function
mcp_calls_total             → total MCP calls across all servers
mcp_servers_active          → count of active MCP servers
```

Per-session MCP data is tracked via session-level aggregation: the existing `session_id` field in `usage_events` allows grouping MCP tool calls by session. This produces a `DailySeries` entry `mcp_calls` for trend visualization.

### 5.4 Metric Classification

Add to `metric_semantics.go`:

```go
const MetricGroupMCP MetricGroup = "MCP Usage"
```

Update `InferMetricGroup()` to route `mcp_*` keys to `MetricGroupMCP` (before the default Activity fallback).

### 5.5 Detail View — MCP Section

New `renderMCPSection()` in `detail.go`, dispatched directly from `RenderDetailContent()` (same pattern as Languages/Models/Trends — needs full snapshot context, does NOT go through `renderMetricGroup()`):

```
┌─────────────────────────────────────────┐
│  MCP Usage                              │
│  ▓▓▓▓▓▓▓▒▒▒░░                          │
│  ■ 1 gopls ·················· 65% 42    │
│      go_diagnostics ·········· 28       │
│      go_workspace ············ 14       │
│  ■ 2 github ················· 25% 16    │
│      create_issue ············  8       │
│      search_code ·············  5       │
│      get_pull_request ········  3       │
│  ■ 3 slack ··················· 10%  6   │
│      send_message ············  4       │
│      read_channel ············  2       │
│  3 servers · 64 calls                   │
└─────────────────────────────────────────┘
```

**Rendering approach:**
1. Scan `mcp_*` metrics from snapshot, parse server/function using the metric key structure.
2. Group by server, sort servers by total calls descending.
3. Render a stacked bar chart for server proportions (reuse `toolMixEntry`, `renderToolMixBar`, `sortToolMixEntries` from tiles.go).
4. For each server: header row with total, indented function rows below.
5. Footer: server count + total calls summary.

### 5.6 Dashboard Tile — MCP Section

New `buildMCPUsageLines()` in `tiles.go`, called after `buildActualToolUsageLines()`. Shows a compact server-level summary (no function breakdown to save space):

```
MCP Usage  64 calls · 3 servers
▓▓▓▓▓▒▒░░
■ 1 gopls ················· 65% 42
■ 2 github ················ 25% 16
■ 3 slack ················· 10%  6
```

MCP tools are **not removed** from the Tool Usage section — they remain there for the complete tool picture. The MCP section is an additional focused view.

### 5.7 Tab Integration

Add "MCP" tab to `DetailTabs()` when MCP metrics are present:

```go
if hasMCPMetrics(snap) {
    tabs = append(tabs, "MCP Usage")
}
```

The `hasMCPMetrics()` helper checks for any `mcp_*` metric keys.

### 5.8 Per-Session MCP Tracking

Extend `queryMCPAgg()` to also query session-level MCP usage:

```sql
SELECT session_id, tool_name, SUM(COALESCE(requests, 1)) AS calls
FROM deduped_usage
WHERE event_type = 'tool_usage' AND tool_name LIKE 'mcp__%'
GROUP BY session_id, tool_name
ORDER BY calls DESC
```

This feeds into a `DailySeries["mcp_calls"]` time series for trend visualization, and session-level MCP breakdowns visible in the detail view.

## 6. Backward Compatibility

Fully backward compatible:
- New metric keys (`mcp_*`) are additive; no existing keys change.
- New `DetailSectionStyle` doesn't affect providers that don't declare it.
- MCP tools remain in `tool_*` metrics for existing Tool Usage views.
- No config schema changes.

## Implementation Tasks

### Task 1: MCP parsing and telemetry aggregation
Files: `internal/telemetry/usage_view.go`
Depends on: none
Description: Add `parseMCPToolName()` function. Add `telemetryMCPAgg` and `telemetryMCPServerAgg` structs. Add `queryMCPAgg()` function that reuses `queryToolAgg()` results and groups by server/function. Add `MCPServers` field to `telemetryUsageAgg`. Include session-level MCP query for per-session tracking. Emit `mcp_*` metrics in `applyCanonicalUsageViewWithDB()` and `mcp_calls` daily series.
Tests: Test `parseMCPToolName()` with various formats. Test `queryMCPAgg()` with in-memory SQLite. Test metric emission produces correct `mcp_*` keys.

### Task 2: Core types — metric group and detail section style
Files: `internal/core/metric_semantics.go`, `internal/core/detail_widget.go`
Depends on: none
Description: Add `MetricGroupMCP` constant. Update `InferMetricGroup()` to route `mcp_*` metric keys to `MetricGroupMCP`. Add `DetailSectionStyleMCP` constant.
Tests: Test `InferMetricGroup()` returns `MetricGroupMCP` for `mcp_gopls_total`, `mcp_calls_total`, etc. Test it still returns `MetricGroupActivity` for `tool_bash`.

### Task 3: TUI detail view — MCP section renderer
Files: `internal/tui/detail.go`
Depends on: Task 2
Description: Add `hasMCPMetrics()` helper. Add "MCP Usage" tab to `DetailTabs()`. Add `renderMCPSection()` that scans `mcp_*` metrics, groups by server, renders stacked bar + server/function breakdown with dot-leader rows. Wire into `RenderDetailContent()` via direct dispatch (same pattern as Languages/Models/Trends — NOT through `renderMetricGroup()`).
Tests: Test `hasMCPMetrics()`. Test `renderMCPSection()` output with mock snapshot containing MCP metrics.

### Task 4: TUI dashboard tile — MCP section
Files: `internal/tui/tiles.go`
Depends on: Task 2
Description: Add `buildMCPUsageLines()` that extracts `mcp_*` server-level metrics and renders compact bar chart with server rows. Call it from the tile rendering pipeline after tool usage. Mark MCP-related keys as used so they don't double-render.
Tests: Test `buildMCPUsageLines()` with mock snapshot.

### Task 5: Provider widget configuration
Files: `internal/providers/claude_code/claude_code.go`, `internal/providers/copilot/copilot.go`, `internal/providers/codex/codex.go`, `internal/providers/gemini_cli/gemini_cli.go`
Depends on: Task 2
Description: Add `{Name: "MCP Usage", Order: N, Style: core.DetailSectionStyleMCP}` to `DetailWidget()` for providers that support telemetry (and therefore can have MCP data). Position between Languages and Spending.
Tests: Verify `DetailWidget()` includes MCP section for telemetry-capable providers.

### Task 6: Integration verification
Files: none (test-only)
Depends on: Tasks 1-5
Description: Run full test suite. Verify `make build` succeeds. Manual smoke test with demo data or real telemetry: confirm MCP section appears on dashboard tile and detail view, confirm grouping by server works, confirm per-session tracking produces trend data.
Tests: `make test`, `make build`, manual verification.

### Dependency Graph
- Tasks 1, 2: parallel (no dependencies between them)
- Tasks 3, 4: parallel (both depend on Task 2, independent of each other)
- Task 5: depends on Task 2
- Tasks 3, 4, 5: parallel group (all depend on Task 2, Task 3/4 also benefit from Task 1 metrics but can be developed against mock data)
- Task 6: depends on all (integration verification)
