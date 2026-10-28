# Claude Code Routine Cloud Usage Design

Date: 2026-10-13
Status: Approved for implementation
Author: OpenUsage contributors

## 1. Problem Statement

OpenUsage cannot see token usage from Claude Code cloud Routines because their runs execute outside the machine-local Claude Code transcript files.

## 2. Goals

1. Accept per-run Routine usage reports over an explicitly enabled HTTP endpoint protected by a bearer token suitable for a private HTTPS tunnel.
2. Preserve per-model token reporting in the existing telemetry analytics, while separating Routine Cloud totals from local Claude Code totals.
3. Reject malformed, oversized, unauthenticated, and duplicate reports safely.

## 3. Non-Goals

1. Query Anthropic's private cloud runtime or read Routine bearer tokens as read credentials.
2. Expose the daemon's Unix socket or enable a public listener by default.
3. Configure a tunnel, TLS termination, or Routine itself on the user's behalf.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|---|---|---|
| core types | none | Reuse canonical usage/token event types. |
| providers | minor | Reuse the existing Claude Code configured account as the dashboard destination. |
| TUI | minor | Existing analytics model dimensions distinguish `claude_code_routine_cloud` from local CLI. |
| config | minor | Add runtime-only optional listener address and secret, supplied from environment. |
| detect | none | No local cloud-routine detection. |
| daemon | major | Start an opt-in TCP HTTP listener and authenticate routine report submissions. |
| telemetry | minor | Persist report events using existing ingestion, deduplication, retention, and read-model paths. |
| CLI | minor | Document the endpoint/env configuration and Routine report schema. |

### Existing Design Doc Overlap

Extends the existing local-only telemetry design in `docs/site/docs/concepts/telemetry.md` and daemon endpoint documentation. Cloud Routine reports are a distinct source channel/agent and do not change local Claude Code hooks.

## 5. Detailed Design

### 5.1 Receiver and security

Add an opt-in HTTP listener configured by `OPENUSAGE_ROUTINE_LISTEN_ADDR`; require `OPENUSAGE_ROUTINE_TOKEN` whenever the listener is enabled. The listener exposes only `POST /v1/routines/claude-code/report`, uses constant-time bearer-token comparison, bounded request bodies, strict JSON decoding, and the existing telemetry store/pipeline. It is intended to be reachable only through the user's private HTTPS tunnel; it does not replace or expose the Unix socket. Empty address disables it.

### 5.2 Report schema and aggregation

Accept a report containing `routine_id`, stable `run_id`, `occurred_at`, and a list of per-model token counts (`model`, `input_tokens`, `output_tokens`, optional cache-read/cache-creation tokens). Validate nonempty identifiers/model names, nonnegative counts, timestamp, and reasonable model count. Derive a stable event/message ID from routine + run + model, record each model as a telemetry usage event with `agent_name=claude_code_routine_cloud`, `provider_id=anthropic`, and route it to the configured Claude Code account. Duplicate reports dedupe through existing event keys. Existing model analytics can then show per-model and combined totals; agent name provides the separate cloud-routine dimension.

### 5.3 Compatibility and data safety

All behavior is unchanged unless the listener address is explicitly configured. The token is read from process environment only, is never written to settings, and must not appear in logs. Existing event schema, local Claude Code totals, local hooks, and retention remain unchanged. Deployments must terminate HTTPS at the private tunnel/proxy and restrict inbound access there.

## 6. Alternatives Considered

### Public listener without authentication

Rejected: anyone reaching the endpoint could inject arbitrary usage records into local telemetry.

### Routine trigger token as a read credential

Rejected: Anthropic documents the per-routine token as trigger-only with no read access; it cannot be used to collect reports back from cloud.

## 7. Implementation Tasks

### Task 1: Implement report receiver and validated ingestion
Files: `internal/daemon/server.go`, `internal/daemon/server_http.go`, `internal/daemon/types.go`, `internal/daemon/routine_report.go`, `internal/daemon/routine_report_test.go`
Depends on: none
Description: Add an opt-in authenticated HTTP receiver alongside the Unix socket. Validate and convert report rows into deduplicable telemetry events routed to the existing Claude Code account. Bound body size and avoid token logging.
Tests: Cover valid/multi-model ingestion, unauthorized requests, bad schema/counts, oversized payload, method mismatch, and idempotent duplicate submission.

### Task 2: Wire runtime configuration
Files: `internal/daemon/service.go`, `internal/daemon/service_env_test.go`, `cmd/openusage/telemetry.go`
Depends on: Task 1
Description: Read listener address and token from environment, require a nonempty token when enabled, start/stop the optional listener with daemon lifecycle, and print endpoint/setup guidance in CLI help.
Tests: Verify listener disabled by default, enabled with credentials, missing-token startup refusal, and service env handling.

### Task 3: Surface and document Routine Cloud analytics
Files: `internal/providers/claude_code/claude_code.go`, `internal/providers/claude_code/widget.go`, `docs/site/docs/concepts/telemetry.md`, `docs/site/docs/daemon/overview.md`, `docs/site/docs/daemon/integrations.md`
Depends on: Task 1
Description: Preserve local Claude Code data while presenting incoming Routine Cloud events under a distinct dashboard/detail section using existing telemetry analytics dimensions. Document report JSON and safe private-tunnel deployment/setup.
Tests: Provider/dashboard widget tests ensure cloud metrics do not overwrite local metrics; docs include a usable example POST payload.

### Dependency Graph

Sequential: Task 1 → Task 2
Task 3 depends on Task 1; it can proceed independently of Task 2. Final integration verification covers all affected packages.
