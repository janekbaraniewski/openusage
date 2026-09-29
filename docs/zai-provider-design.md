# Z.AI Provider Design for OpenUsage

Date: 2026-02-20
Author: Codex (design draft based on live endpoint validation + repository integration review)
Status: Proposed

## 1. Objective

Design and implement a first-class `zai` provider for OpenUsage that:

- Collects usage metrics and quota statistics for Z.AI coding accounts.
- Surfaces account/subscription-related metadata where available.
- Integrates cleanly with existing OpenUsage provider patterns, status semantics, TUI rendering, and detection pipeline.
- Handles free/no-balance accounts correctly (valid auth, no usage payloads, rate-limited/ineligible execution).

This document is implementation-focused and maps directly to concrete file changes.

## 2. Scope

In scope:

- New provider package `internal/providers/zai/`.
- Registry wiring.
- Detection wiring for env vars and optional local coding-helper config.
- Metric/Raw/DailySeries mapping into `core.QuotaSnapshot`.
- Status and message logic.
- Test plan and acceptance criteria.

Out of scope:

- New TUI components or schema changes in core types.
- Browser/session scraping.
- Non-public/private Z.AI internal APIs.

## 3. Existing System Constraints

Relevant architecture in current codebase:

- Provider interface: `internal/core/provider.go` (`ID`, `Describe`, `Fetch`).
- Snapshot schema: `internal/core/types.go` (metrics, resets, raw, daily series, status).
- Provider registration: `internal/providers/registry.go`.
- Auto-detection: `internal/detect/detect.go`.
- TUI already understands many billing/account fields from `Raw` keys in:
  - `internal/tui/model.go`
  - `internal/tui/detail.go`
  - `internal/tui/analytics.go`

Design must preserve existing behaviors:

- Missing key -> `AUTH_REQUIRED` snapshot with `err == nil`.
- Fatal transport/request build problems -> `error` return.
- Partial endpoint failures -> populate snapshot + `Raw["*_error"]`, avoid hard fail.
- Do not log or persist secrets.

## 4. External API Findings (Validated)

The following behavior was validated live on 2026-02-20 with a real token (token redacted):

1. `GET https://api.z.ai/api/coding/paas/v4/models`
- `Authorization: Bearer <token>`
- Returns `200` with OpenAI-style list object:
  - top-level keys include `object`, `data`
  - `.object == "list"`
  - `.data` array with model entries

2. `POST https://api.z.ai/api/coding/paas/v4/chat/completions`
- With valid but no-balance token, returns `429` with:
  - `error.code = 1113`
  - message equivalent to "Insufficient balance or no resource package"

3. Monitor endpoints (coding plan usage)
- `GET https://api.z.ai/api/monitor/usage/quota/limit`
- `GET https://api.z.ai/api/monitor/usage/model-usage?...`
- `GET https://api.z.ai/api/monitor/usage/tool-usage?...`
- Return wrapper object with keys `code`, `msg`, `success`.
- For free/no-entitlement account, wrapper returns success but no `data` payload.
- `Authorization` accepted both as raw token and bearer token in testing.

4. Credits endpoint
- Candidate paths on `api.z.ai` did not return a stable usable payload for tested token (404/500 variants observed).
- For implementation we treat credits endpoint as best-effort optional and never required for provider success.

Key interpretation:

- A token can be valid for models yet not eligible for paid coding requests or usage payload emission.
- Empty monitor `data` with `success=true` is a valid account state and must not be treated as provider failure.

## 5. Product Behavior Requirements

Provider must support three common account states:

1. Valid + active paid usage
- Models available, monitor data populated, chat usage possible.

2. Valid + free/no package (tested state)
- Models available.
- Chat completions denied with explicit business code (`1113`).
- Monitor returns success wrapper with empty/missing `data`.

3. Invalid/expired token
- Auth failures (401/403) on API calls.

Expected UX:

- State (2) should show account as connected but with a clear message that no package/balance is active.
- State (3) should show `AUTH_REQUIRED`.

## 6. Provider Design

### 6.1 Package Layout

Add:

- `internal/providers/zai/zai.go`
- `internal/providers/zai/zai_test.go`

### 6.2 Provider Identity

- `ID() string` -> `"zai"`
- `Describe()`:
  - `Name: "Z.AI"`
  - `Capabilities: []string{"coding_models", "coding_plan_usage", "quota_limit", "model_usage", "tool_usage"}`
  - `DocURL: "https://docs.z.ai/api-reference/introduction"`

### 6.3 Account Configuration Strategy

Reuse existing `core.AccountConfig` fields:

- `APIKeyEnv` for env var based auth.
- `Token` for runtime credential injection.
- `BaseURL` optional override for coding base URL.
- `ExtraData` optional hints:
  - `plan_type` (`glm_coding_plan_global` or `glm_coding_plan_china`)
  - `source` (`chelper`, `env`, etc.)

Default routing:

- Coding API base:
  - global: `https://api.z.ai/api/coding/paas/v4`
  - china: `https://bigmodel.cn/api/coding/paas/v4`
- Monitor base:
  - global: `https://api.z.ai`
  - china: `https://bigmodel.cn`

Base selection precedence:

1. `acct.BaseURL` if provided
2. `acct.ExtraData["plan_type"]` if present
3. heuristic by provider default -> global

### 6.4 Auth Header Rules

Use separate auth modes per endpoint class:

- Coding endpoints: `Authorization: Bearer <token>`
- Monitor endpoints: `Authorization: <token>` (raw)

Implementation note:

- If monitor raw header fails with auth code, one fallback retry with bearer is allowed.
- Do not retry endlessly.

### 6.5 Endpoint Calls in Fetch

`Fetch(ctx, acct)` flow:

1. Resolve key.
2. Initialize snapshot maps.
3. Call `fetchModels(...)` (required anchor call).
4. Call monitor endpoints best-effort:
  - `fetchQuotaLimit(...)`
  - `fetchModelUsage(...)`
  - `fetchToolUsage(...)`
5. Optional credit endpoint probe best-effort:
  - if endpoint returns non-2xx or unrecognized schema, store raw error and continue.
6. Evaluate aggregate status and message.
7. Return `snap, nil`.

Transport errors on required anchor call (`models`) may return fatal error only if request cannot be made (e.g. DNS/network). HTTP auth/business responses should remain snapshot-based.

### 6.6 External Response Models (Go structs)

Define private structs for:

- Models list response.
- Standard API error response (`error.code`, `error.message`).
- Monitor wrapper response (`code`, `msg`, `success`, `data`).
- Quota limit payload with `limits[]`:
  - `type`
  - `usage`
  - `currentValue`
  - `percentage`
  - `nextResetTime` (optional)
  - `usageDetails` (optional, keep flexible as `json.RawMessage` or `interface{}`).
- Model usage entries (tolerant decode using flexible fields).
- Tool usage entries (tolerant decode).

Given live variability, use resilient parsing:

- Accept `data` absent/null.
- Unknown fields ignored.
- Numeric fields decode with helper conversions where needed.

## 7. Snapshot Mapping

### 7.1 Metrics

Primary metrics:

- Windowed quota rows (`type == TOKENS_LIMIT`, the pre-credits shape, or
  `type == CREDIT_LIMIT`, the credits-metered shape) are routed by their
  `unit`/`number` pair: `unit: 3, number: 5` is the 5-hour rolling window and
  `unit: 6, number: 1` is the weekly window. Any other duration is left
  unmapped rather than guessed at. A legacy `TOKENS_LIMIT` row with no
  `unit`/`number` at all is still treated as the 5-hour window.

- `usage_five_hour` / `usage_seven_day`:
  - `Used = percentage` (clamped 0-100; `percentage` is an integer percent, so
    `1` means `1%`)
  - `Limit = 100`
  - `Unit = "%"`
  - `Window = "5h"` / `"7d"`

- `tokens_five_hour` / `tokens_seven_day` (if both numeric fields available):
  - `Used = currentValue`
  - `Limit = usage`
  - `Remaining = usage-currentValue`
  - `Unit = "tokens"`
  - `Window = "5h"` / `"7d"`

- `credits_five_hour` / `credits_seven_day` (same numeric fields, for
  `CREDIT_LIMIT` rows):
  - `Used = currentValue`
  - `Limit = usage`
  - `Remaining = usage-currentValue`
  - `Unit = "credits"`
  - `Window = "5h"` / `"7d"`

- `mcp_monthly_usage`:
  - from quota item `type == TIME_LIMIT`
  - `Used = currentValue`
  - `Limit = usage`
  - `Remaining = usage-currentValue`
  - `Unit = "calls"`
  - `Window = "1mo"`

Best-effort aggregated activity metrics (when monitor usage payload is populated):

- `today_requests`
- `today_input_tokens`
- `today_output_tokens`
- `today_api_cost`
- `7d_api_cost`

Model-usage aggregation:

- Live `model-usage` payloads expose a per-model time series (`modelDataList`),
  a summary list repeated both at the top level and inside `totalUsage`, and
  whole-window rollups (`totalUsage.totalModelCallCount`,
  `totalUsage.totalTokensUsage`).
- Scalar numeric keys are classified by name. Token-count keys map to token
  totals and are never read as requests, so a token rollup cannot surface as a
  request count; `total*` call/token keys are whole-window rollups rather than
  per-entity counts.
- Rollups are authoritative for `window_requests`/`window_tokens`. The
  per-model breakdown still feeds `model_*` metrics but is not summed on top of
  the rollups, and a mirrored summary list is dropped when the richer
  `modelDataList` is present, so repeated copies cannot multiply totals. A
  summary list repeated without `modelDataList` is compared by content and read
  once.

Tool-usage aggregation:

- Live `tool-usage` payloads use the same shape: per-tool time series arrays,
  a `toolDataList` breakdown, a `toolSummaryList` repeated at the top level and
  inside `totalUsage`, and whole-window rollups
  (`totalUsage.totalNetworkSearchCount`, `totalUsage.totalSearchMcpCount`).
- `total*` rollups never become tool rows. They can overlap — a group total
  (`totalSearchMcpCount`) next to the per-tool totals — so they are not summed
  either; they only supply the window call total when the per-tool breakdown is
  empty or carries no readable counts, and the largest reported rollup is taken
  there.
- A repeated `toolSummaryList` is read once, exactly like the model summary
  list.

These keys are chosen to align with existing TUI summary logic.

### 7.2 Resets

- If a windowed quota row has `nextResetTime`:
  - convert ms epoch -> `time.Time`
  - set `snap.Resets["usage_five_hour"]` (5h window) or
    `snap.Resets["usage_seven_day"]` (weekly window).

### 7.3 DailySeries

When model/tool usage provides date-bucketed data:

- `daily_series["cost"]`
- `daily_series["requests"]`
- `daily_series["tokens_<model>"]` for top models

If usage payload is missing/null, leave `DailySeries` empty.

### 7.4 Raw Metadata

Populate stable account/context keys:

- `provider_region` (`global` or `china`)
- `plan_type` (from local config hint, if known)
- `models_count`
- `active_model` (optional first/default model if determinable)
- `subscription_status` inferred:
  - `"inactive_or_free"` for code `1113` or empty monitor data with success wrapper
  - `"active"` when meaningful quota/usage data exists

Debug/diagnostic raw keys:

- `quota_api` (`ok` / `empty` / `error`)
- `model_usage_api`
- `tool_usage_api`
- `chat_probe_code` (if probe attempted in future mode)
- `quota_limit_error`, `model_usage_error`, `tool_usage_error`, `credits_error`

Do not store secrets in `Raw`.

## 8. Status and Message Logic

Status precedence:

1. Any endpoint returning auth failure (401/403 style) on required anchor call -> `AUTH_REQUIRED`.
2. If quota usage indicates >= 100% or explicit no-balance code from usage probe -> `LIMITED`.
3. If quota usage >= 80% -> `NEAR_LIMIT`.
4. If account valid but no monitor data -> `OK` (with explanatory message).
5. Fallback -> `OK`.

Message templates:

- Active quota: `"5h usage XX%"` or `"7d usage XX%"` — the tracked window
  (`5h`/`7d`) that is furthest along, with no unit in the label because
  `CREDIT_LIMIT` plans meter the same windows in credits — with ` · MCP YY/ZZ`
  appended when the monthly MCP `used`/`limit` pair is known.
- Empty/free state: `"Connected, but no active coding package/balance"`
- Auth state: `"HTTP 401/403 - check API key"`
- Limited by business code: `"Insufficient balance or no resource package"`

Rationale:

- For free accounts, `AUTH_REQUIRED` is misleading.
- `LIMITED` better communicates inability to run paid completions.

## 9. Detection and Account Enrichment

### 9.1 Env var detection

In `internal/detect/detect.go`, extend `envKeyMapping` with:

- `ZAI_API_KEY` -> provider `zai`, account `zai-auto`
- `ZHIPUAI_API_KEY` -> provider `zai`, account `zhipuai-auto`

### 9.2 Local coding-helper config detection (optional but recommended)

Add `detectZAICodingHelper(result *Result)`:

- Read `~/.chelper/config.yaml` if exists.
- Parse:
  - `plan` (`glm_coding_plan_global` / `glm_coding_plan_china`)
  - `api_key` (store in `Token`, not persisted)
- Add account:
  - `ID`: `zai-coding-plan-auto`
  - `Provider`: `zai`
  - `Auth`: `api_key`
  - `Token`: `<api_key>`
  - `ExtraData["plan_type"] = <plan>`
  - `ExtraData["source"] = "chelper"`

Security:

- Never print full key in logs.

## 10. Error Handling Policy

Rules:

- Missing key -> auth snapshot (`err == nil`).
- Request creation/transport failures on anchor call -> fatal error (`QuotaSnapshot{}, error`).
- Non-anchor failures -> annotate raw error and continue.
- JSON parse failures on optional endpoints -> annotate raw error and continue.

Provider should return the richest possible partial snapshot whenever feasible.

## 11. Testing Strategy

### 11.1 Unit tests (`internal/providers/zai/zai_test.go`)

Required test cases:

1. `TestFetch_MissingKey_ReturnsAuth`
2. `TestFetch_ModelsUnauthorized_ReturnsAuth`
3. `TestFetch_ModelsOK_NoMonitorData_FreeState`
4. `TestFetch_QuotaLimit_ParsesTokensAndMCP`
5. `TestFetch_QuotaLimit_SetStatusNearLimit`
6. `TestFetch_QuotaLimit_SetStatusLimited`
7. `TestFetch_QuotaLimit_ParsesResetTime`
8. `TestFetch_MonitorDataNull_DoesNotError`
9. `TestFetch_PartialFailures_StillReturnsSnapshot`
10. `TestFetch_BaseSelection_FromPlanTypeGlobalAndChina`

Use `httptest.NewServer`, table-driven style, no external dependencies.

### 11.2 Detection tests

Add/extend tests in `internal/detect/detect_test.go`:

- env mapping for new vars.
- optional chelper config parse behavior.

### 11.3 Manual validation checklist

Run against:

- Paid active token.
- Free/no-package token.
- Invalid token.
- Global and China plan routes.

Assertions:

- Status category correctness.
- No secret leakage in logs/raw.
- TUI summary appears meaningful.

## 12. Implementation Plan (File-Level)

1. Add provider:
- `internal/providers/zai/zai.go`
- `internal/providers/zai/zai_test.go`

2. Register provider:
- `internal/providers/registry.go`

3. Add detection mappings:
- `internal/detect/detect.go`
- `internal/detect/detect_test.go` (if needed)

4. (Optional) Add sample config entry:
- `configs/example_settings.json`
- `README.md` supported providers list update

5. Run verification:
- `go test ./internal/providers/... -v`
- `go test ./internal/detect -v`
- `go test ./...`

## 13. Acceptance Criteria

Provider is accepted when:

- Appears in provider registry and can be configured like others.
- For valid free token:
  - models call succeeds
  - snapshot not marked auth error
  - message explains no active package/balance
- For invalid token:
  - `AUTH_REQUIRED`.
- For active paid token:
  - quota/usage metrics populate.
- No API key/token leakage in logs or snapshot raw map.

## 14. Risks and Mitigations

Risk: monitor payload schemas may vary by region/account type.
- Mitigation: tolerant parsers, optional fields, robust fallback.

Risk: subscription metadata endpoint is not publicly stable.
- Mitigation: infer status from observable API signals and local config; expose as inferred raw fields.

Risk: semantic confusion between auth and entitlement failures.
- Mitigation: treat `1113` as entitlement/limit state, not auth failure.

## 15. Future Enhancements

- Add lightweight periodic chat probe (disabled by default) to enrich entitlement detection.
- Add richer model-level daily series once stable schema from monitor data is observed in paid accounts.
- Add optional region override in account config for users with cross-region routing needs.

## 16. Source References

Primary docs and artifacts used:

- https://docs.z.ai/api-reference/introduction
- https://docs.z.ai/api-reference/model-api/list-models
- https://docs.z.ai/api-reference/account/retrieve-user-credit-grants
- https://help.z.ai/en/articles/12328210-what-s-included-in-the-coding-membership-plans
- https://help.z.ai/en/articles/12336967-where-can-i-check-my-coding-membership-usage-and-limits
- https://registry.npmjs.org/@z_ai/coding-helper

Notes:

- Some conclusions in this design are explicitly inference-based from observed API behavior and official shipped tooling scripts, because a formal subscription-details API is not clearly documented.
