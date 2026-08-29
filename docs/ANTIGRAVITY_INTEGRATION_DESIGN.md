# Antigravity CLI Integration Design

Date: 2026-08-15
Status: Implemented
Author: Codex

## 1. Problem Statement

OpenUsage cannot currently observe the installed Antigravity CLI (`agy`), even though Antigravity exposes a documented status-line JSON feed containing model, context-window, workspace, and quota data.

## 2. Goals

1. Add a native `antigravity` provider that renders the latest Antigravity status-line data in the OpenUsage dashboard.
2. Feed status-line observations through the existing telemetry source and deduplication pipeline without counting cumulative context totals as repeated spend.
3. Detect the installed `agy` CLI and its local configuration automatically.
4. Install and uninstall the Antigravity `statusLine` command through the existing integrations manager, preserving a backup of the tool settings.
5. Document setup, supported data, limitations, and the explicit no-cost/no-API-key behavior.

## 3. Non-Goals

1. Do not extract OAuth tokens, API keys, browser cookies, or other credentials.
2. Do not reverse-engineer or ingest Antigravity brain/transcript files in this slice. The installed `~/.gemini/antigravity-cli/brain` layout is useful future evidence, but it is not a stable public usage contract.
3. Do not estimate dollar spend. Antigravity's status-line payload exposes quota and token/context data, not a reliable public price surface.
4. Do not add a second Antigravity API client or refresh authentication independently of Antigravity.
5. Do not change the existing `gemini_cli` provider's behavior or reinterpret its legacy `~/.gemini/antigravity` paths.

## 4. Intake Decisions and Research

The implementation quiz is resolved from the installed CLI and the vendor documentation:

| Decision | Resolution |
|----------|------------|
| Provider name and stable ID | Antigravity CLI / `antigravity` |
| Authentication | Local, status-line feed only; no secret extraction |
| Primary source | Antigravity's documented `statusLine` command JSON on stdin |
| Local state | Atomic latest-status file under OpenUsage's telemetry state directory |
| Metrics | Context-window percentage, cumulative session input/output/total tokens, current usage token parts, quota remaining/reset values, model/workspace metadata |
| Account model | One auto-detected local account, ID `antigravity`; manual `status_file` path override remains possible |
| Dashboard color | `mauve`, the unused provider role |
| Spend | None; no dollar metric is emitted |
| Telemetry event semantics | The status line is a point-in-time feed. `current_usage` is emitted as event usage; cumulative totals remain snapshot metrics. Revision IDs make repeated polls idempotent. |
| Installer behavior | Register `openusage antigravity statusline` in `~/.gemini/antigravity-cli/settings.json`; back up the settings file first; refuse to replace an unrelated custom status-line command |

The vendor contract used here is the Antigravity CLI status-line interface: a configured command receives JSON on stdin and writes the rendered line to stdout. Antigravity also documents the `statusLine` configuration object and quota/context fields. The repository's live installation confirms the active config root is `~/.gemini/antigravity-cli/`, with `settings.json` and a `brain/` directory.

## 5. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | Reuse `UsageSnapshot`, `Metric`, `ModelUsageRecord`, and existing widget types. |
| providers | major | Add `internal/providers/antigravity` with status-line parsing, snapshot projection, dashboard metadata, and telemetry source behavior. |
| TUI | minor | The provider supplies a mauve widget and compact rows; no renderer changes. |
| config | minor | Add an example local account; no persisted schema change. |
| detect | minor | Detect `agy` plus `~/.gemini/antigravity-cli` and register a local account. |
| daemon | none | Existing source collector discovers the new `TelemetrySource` through the provider registry. |
| telemetry | minor | Consume the latest status-line state through the existing collector and dedup keys; no store schema change. |
| integrations | major | Add an installer definition and safe JSON patcher for Antigravity's `statusLine` setting. |
| CLI | minor | Add `openusage antigravity statusline`, the command invoked by Antigravity. |
| documentation | minor | Add provider/setup docs and update provider catalog/concepts. |

### Existing Design Doc Overlap

This extends the existing provider and unified telemetry patterns documented in `docs/skills/add-new-provider.md`, `docs/skills/openusage-provider.md`, and `docs/TELEMETRY_INTEGRATIONS.md`. It is additive to the older `gemini_cli` provider: Gemini CLI's OAuth/session-file implementation remains unchanged, while this provider owns the distinct Antigravity CLI status-line contract.

## 6. Detailed Design

### 6.1 Status-line capture and state

`openusage antigravity statusline` reads the JSON document from stdin, validates the fields OpenUsage understands, adds a local `received_at` timestamp, and atomically replaces:

```text
${XDG_STATE_HOME:-~/.local/state}/openusage/antigravity-status.json
```

The file is mode `0600`. The latest payload is intentionally the source of truth for the provider; this avoids making Antigravity invoke a database write on every status-line refresh.

The command prints a short, non-ANSI line such as `AGY model · quota 82% · context 31%`. Errors are returned to the caller but never written into the status-line output as a JSON document.

### 6.2 Provider snapshot

The provider reads the latest state file and projects:

- `quota` plus `quota_<bucket>` metrics as 0–100 percentage gauges, with reset timestamps when available.
- `context_window` as a percentage gauge when Antigravity supplies used/remaining percentages.
- `total_input_tokens`, `total_output_tokens`, and `total_tokens` as session cumulative counters.
- `current_input_tokens`, `current_output_tokens`, `current_cache_read_tokens`, and `current_cache_write_tokens` when supplied.
- model, workspace, session, plan, product, CLI version, and agent state as attributes/diagnostics.
- one session-window `ModelUsageRecord` for the active model when cumulative token totals exist.

Quota state determines `OK`, `NEAR_LIMIT`, or `LIMITED`; missing or malformed state is a non-fatal `ERROR` snapshot with setup guidance. No spend metric is emitted.

### 6.3 Telemetry source

The provider implements the existing `shared.TelemetrySource` interface:

- `Collect` reads the latest state file and emits one event for the current status revision.
- `ParseHookPayload` accepts the same raw status-line JSON for explicit `openusage telemetry hook antigravity` use.
- Event token usage uses `context_window.current_usage`, which represents the current status-line observation. Cumulative `total_*` values are retained in event payload metadata and are not added repeatedly as deltas.
- A stable revision ID combines session identity, cumulative totals, current usage, model, and agent state. Repeated daemon polls therefore deduplicate through the existing event store.

### 6.4 Detection and configuration

`AutoDetect` looks for the `agy` executable and `~/.gemini/antigravity-cli` data (`settings.json` or `brain/`). It adds:

```json
{
  "id": "antigravity",
  "provider": "antigravity",
  "auth": "local",
  "binary": "/path/to/agy",
  "provider_paths": {
    "config_dir": "~/.gemini/antigravity-cli"
  }
}
```

The provider accepts an optional `status_file` provider path for tests, alternate installations, and explicit multi-account configuration.

### 6.5 Integration installer

The integrations manager gains `antigravity`. Installation targets the Antigravity settings file and sets:

```json
{
  "statusLine": {
    "type": "command",
    "command": "<resolved-openusage-binary> antigravity statusline",
    "enabled": true,
    "stack_with_default": true
  }
}
```

The existing settings file is backed up as `settings.json.bak` by the current installer. If `statusLine.command` already contains a different command, installation fails rather than silently destroying the user's custom status line. Uninstall removes only an OpenUsage-owned command and leaves unrelated custom configuration intact.

### 6.6 Backward Compatibility

All existing providers, telemetry tables, config fields, and integrations remain compatible. The new provider is only activated when Antigravity is detected or manually configured. Existing Gemini CLI accounts continue using provider ID `gemini_cli` and their existing OAuth/session paths.

## 7. Alternatives Considered

### Extend `gemini_cli`

Rejected. The installed Antigravity CLI has a different executable, config root, status-line contract, and local state layout. Combining them would make provider authentication, detection, telemetry attribution, and troubleshooting ambiguous.

### Parse Antigravity brain/transcript files first

Rejected for the initial slice. The local brain directory is observable but undocumented and likely to change. The status-line JSON is explicit, versioned by the CLI contract, and already carries the dashboard-critical data.

### Spawn a telemetry subprocess from every status-line invocation

Rejected. Status lines are latency-sensitive and may refresh frequently. Writing one atomic latest-state file lets the existing daemon collector ingest on its normal cadence without adding process/database overhead to Antigravity.

## 8. Implementation Tasks

### Task 1: Add the provider and status-line state projection

Files: `internal/providers/antigravity/*.go`, `internal/providers/registry.go`

Description: Implement payload decoding, atomic state capture, snapshot metrics, model usage, status-line rendering, the mauve dashboard widget, and the telemetry source.

Tests: Payload parsing, quota/context projection, missing/malformed state, stable telemetry revisions, repeated collection, and status-line rendering.

### Task 2: Add detection and example configuration

Files: `internal/detect/detect.go`, `configs/example_settings.json`

Depends on: Task 1

Description: Detect `agy` and the Antigravity CLI config root, register the local account, and add the example account entry.

Tests: Auto-detection smoke/fixture coverage where practical; preserve existing detector precedence and deduplication.

### Task 3: Add the CLI command and integration installer

Files: `cmd/openusage/main.go`, `cmd/openusage/antigravity.go`, `internal/integrations/manager.go`, `internal/integrations/definitions.go`, integration tests

Depends on: Task 1

Description: Add the status-line command and safe `settings.json` patcher/detector, including backup-compatible install/uninstall behavior.

Tests: JSON patching, refusal to replace unrelated commands, install/uninstall detection, and CLI capture with a temporary state file.

### Task 4: Document the provider

Files: `docs/site/docs/providers/antigravity.md`, provider catalog/concepts, troubleshooting/configuration references

Depends on: Tasks 1–3

Description: Document installation, `openusage integrations install antigravity`, supported fields, state path, no-spend behavior, and known limitations.

Tests: Markdown links and generated site content remain structurally valid.

### Task 5: Validate the vertical slice

Files: none

Depends on: Tasks 1–4

Description: Run focused package tests, `gofmt`, build, vet, and lint as available. Review the final diff for unrelated changes and verify the design's scope.

Tests: `go test -race` for changed packages, `go build ./cmd/openusage`, `go vet ./...`, and `make lint` when the configured linter is available.

### Dependency Graph

```text
Sequential: Task 1 → Tasks 2 and 3 (parallel in concept)
Sequential: Tasks 2 and 3 → Task 4
Sequential: Task 4 → Task 5
```
