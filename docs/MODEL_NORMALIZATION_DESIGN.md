# Cross-Provider Model Normalization Design

Date: 2026-02-21
Status: Proposed (implementation-ready)
Author: Codex

## 1) Problem Statement

OpenUsage already collects model-level usage in several providers (`claude_code`, `codex`, `gemini_cli`, `copilot`, `cursor`, `openrouter`), but model IDs are inconsistent and often provider-specific.

Examples today:

- `model_claude_opus_4_6_input_tokens`
- `model_claude-4.6-opus-high-thinking_input_tokens`
- `model_openai_gpt_4_1_input_tokens`
- `model_anthropic_claude-opus-4.1_input_tokens`

Because of this, analytics can only do best-effort grouping by metric key shape. It cannot reliably answer:

- "How many tokens did I spend on Opus 4.6 total?"
- "How is Opus 4.6 split across providers/accounts?"

## 2) Goals and Non-Goals

## Goals

1. Keep the current dynamic/autodiscovery behavior (no fixed model allowlist required).
2. Introduce canonical model identities across providers.
3. Preserve raw provider model IDs for traceability.
4. Support high-confidence grouping (lineage + optional snapshot granularity).
5. Enable cross-provider analytics splits by provider/account.
6. Keep backward compatibility with existing metric keys and UI.

## Non-Goals (for this phase)

1. Building a historical warehouse beyond data already available in providers.
2. Replacing all existing `model_*` metric keys immediately.
3. Perfect normalization for every unknown model on day one.

## 3) Research Findings (Official Sources)

Model naming is intentionally not uniform across ecosystems and often has alias/snapshot behavior.

1. OpenAI: undated aliases can point to newer dated snapshots over time.
- Reference: [OpenAI deprecations](https://platform.openai.com/docs/deprecations)

2. Anthropic: docs explicitly separate aliases and snapshot IDs (date-suffixed variants).
- Reference: [Anthropic models overview](https://docs.anthropic.com/en/docs/about-claude/models/overview)

3. OpenRouter: activity schema exposes both `model` and `model_permaslug`; routing docs note slugs may change as new versions arrive.
- References:
  - [OpenRouter user activity API](https://openrouter.ai/docs/api-reference/limits-and-account/get-user-activity)
  - [OpenRouter auto router](https://openrouter.ai/docs/features/auto-router)

4. Gemini API: model IDs include stable and preview/date-flavored forms via the models API.
- Reference: [Google AI models](https://ai.google.dev/gemini-api/docs/models)

5. Mistral API schemas expose model aliases.
- Reference: [Mistral API](https://docs.mistral.ai/api/)

Design implication: we need two canonical levels:

- `lineage` (stable grouping target, e.g. `anthropic/claude-opus-4.6`)
- `release` (snapshot-specific when known, e.g. `anthropic/claude-opus-4.6@20260219`)

## 4) Current Codebase Findings

Relevant code paths:

- Snapshot schema: `internal/core/types.go`
- Snapshot normalization hook: `internal/core/snapshot_normalize.go`
- Analytics extraction (current metric-key parsing): `internal/tui/analytics.go`
- Model mix extraction: `internal/tui/tiles.go`, `internal/tui/detail.go`

Current limitations:

1. Most model identity is derived from metric keys, often sanitized/lossy.
2. No structured, provider-agnostic model usage records.
3. Cross-provider grouping relies on string heuristics in UI layer.

## 5) Proposed Architecture

## 5.1 New Data Model (Core)

Add structured model usage records to `UsageSnapshot`.

```go
type ModelUsageRecord struct {
    // source identity
    RawModelID      string            `json:"raw_model_id"`      // exact provider/source model id
    RawSource       string            `json:"raw_source"`        // "api", "jsonl", "sqlite", "metrics_fallback"

    // canonical identity
    CanonicalLineageID string         `json:"canonical_lineage_id,omitempty"` // e.g. anthropic/claude-opus-4.6
    CanonicalReleaseID string         `json:"canonical_release_id,omitempty"` // e.g. anthropic/claude-opus-4.6@20260219
    CanonicalVendor    string         `json:"canonical_vendor,omitempty"`     // anthropic/openai/google/...
    CanonicalFamily    string         `json:"canonical_family,omitempty"`     // claude/gpt/gemini/...
    CanonicalVariant   string         `json:"canonical_variant,omitempty"`    // opus/sonnet/mini/pro/flash/...

    // confidence and traceability
    Confidence float64                `json:"confidence,omitempty"` // 0..1
    Reason     string                 `json:"reason,omitempty"`     // override/exact/permaslug/heuristic

    // dimensions
    Window     string                 `json:"window,omitempty"`     // today, 7d, all-time, billing-cycle, ...
    Dimensions map[string]string      `json:"dimensions,omitempty"` // provider/account/client/endpoint

    // usage values
    InputTokens     *float64          `json:"input_tokens,omitempty"`
    OutputTokens    *float64          `json:"output_tokens,omitempty"`
    CachedTokens    *float64          `json:"cached_tokens,omitempty"`
    ReasoningTokens *float64          `json:"reasoning_tokens,omitempty"`
    TotalTokens     *float64          `json:"total_tokens,omitempty"`
    CostUSD         *float64          `json:"cost_usd,omitempty"`
    Requests        *float64          `json:"requests,omitempty"`
}
```

Extend snapshot:

```go
ModelUsage []ModelUsageRecord `json:"model_usage,omitempty"`
```

Backward compatibility:

- Keep existing `Metrics` and `Raw` behavior unchanged.
- `ModelUsage` is additive.

## 5.2 Canonicalization Engine

Create `internal/core/modelnormalize/`.

Public API:

```go
type CanonicalModel struct {
    LineageID string
    ReleaseID string
    Vendor    string
    Family    string
    Variant   string
    Confidence float64
    Reason string
}

type NormalizeInput struct {
    ProviderID string
    RawModelID string
    Hints map[string]string // provider_name, model_permaslug, etc.
}

func NormalizeModel(in NormalizeInput, cfg NormalizationConfig) CanonicalModel
```

Normalization pipeline:

1. Pre-normalize tokenization:
- lowercase, trim
- strip prefixes like `models/`
- preserve original raw ID separately

2. Parse explicit vendor prefixes when present:
- `anthropic/claude-...`
- `openai/gpt-...`
- etc.

3. Detect alias/snapshot markers:
- date suffixes (`YYYY-MM-DD`, `YYYYMMDD`)
- tags like `latest`, `preview-*`

4. Family-specific transforms:
- Claude reorder normalization (`claude-4.6-opus` -> `claude-opus-4.6`)
- stable extraction for GPT/Gemini/Mistral style IDs

5. Resolve canonical IDs:
- lineage: snapshot-agnostic
- release: include snapshot if detected

6. Confidence scoring:
- `1.00` explicit user override
- `0.95` explicit permaslug/official snapshot field
- `0.90` explicit vendor prefix + valid family parse
- `0.75` heuristic family parse
- `<0.70` unresolved fallback (`unknown/<normalized-raw>`)

Safety rule:

- Merge across providers only when confidence >= configured threshold (default `0.80`).

## 5.3 Overrides and Dynamic Behavior

Add config block (optional):

```json
"model_normalization": {
  "enabled": true,
  "group_by": "lineage",
  "min_confidence": 0.8,
  "overrides": [
    {
      "provider": "cursor",
      "raw_model_id": "claude-4.6-opus-high-thinking",
      "canonical_lineage_id": "anthropic/claude-opus-4.6"
    }
  ]
}
```

Principles:

1. Dynamic first: unknown models are still surfaced automatically.
2. Overrides only refine grouping when needed.
3. No hard failure on unrecognized IDs.

## 6) Provider Integration Plan

## 6.1 Phase 1 (No provider rewrites required)

Implement fallback extractor from current metrics to bootstrap `ModelUsage`.

New core helper:

```go
func BuildModelUsageFromSnapshotMetrics(s UsageSnapshot) []ModelUsageRecord
```

This parses existing patterns:

- `model_<name>_input_tokens`
- `model_<name>_output_tokens`
- `model_<name>_cost(_usd)`
- `input_tokens_<name>` / `output_tokens_<name>`

Mark fallback records with `RawSource = "metrics_fallback"` and lower confidence.

## 6.2 Phase 2 (Provider-native, lossless)

Add a shared helper for providers:

```go
func AppendModelUsageRecord(snap *core.UsageSnapshot, rec core.ModelUsageRecord)
```

Incrementally adopt in providers that already have raw model IDs:

1. `internal/providers/openrouter/openrouter.go`
- use raw `model`, `model_permaslug`, `provider_name`

2. `internal/providers/claude_code/claude_code.go`
- use raw JSONL model IDs before sanitization

3. `internal/providers/codex/codex.go`
- use raw session model IDs

4. `internal/providers/gemini_cli/gemini_cli.go`
- use raw message model IDs

5. `internal/providers/copilot/copilot.go`
- use raw session model fields

6. `internal/providers/cursor/cursor.go`
- use raw `ModelIntent`

All providers continue emitting existing metric keys for compatibility.

## 7) Analytics and Intelligence Changes

## 7.1 Cross-provider model table

In `internal/tui/analytics.go`, move model aggregation source from metric-key parsing to `ModelUsage` records.

New behavior:

1. Group by `CanonicalLineageID` (default).
2. Show totals (tokens/cost/requests).
3. Show provider/account split for each canonical model.

Example output concept:

- `anthropic/claude-opus-4.6`
- total input/output/tokens/cost
- split:
  - `cursor-work`: 62%
  - `openrouter`: 28%
  - `claude-code-local`: 10%

## 7.2 Window-aware aggregation

Do not mix incompatible windows silently.

Window bucketing:

- `session`
- `today`
- `7d`
- `30d`
- `billing-cycle`
- `all-time`
- `unknown`

Default analytics window selection:

1. Prefer `7d` if present for >=2 sources.
2. Else `today`, else `billing-cycle`, else `all-time`.
3. Always show selected window label in section title.

## 7.3 Intelligence metrics (derived)

For each canonical model (selected window):

1. Provider concentration (% by provider/account)
2. Cost efficiency (`$/1K tokens`) where cost exists
3. Input/output ratio
4. Cached/reasoning share when available
5. Confidence indicator (high/medium/low)

## 8) File-by-File Implementation Plan

## New files

1. `internal/core/model_usage.go`
- new `ModelUsageRecord` type + helpers

2. `internal/core/modelnormalize/normalize.go`
- canonicalization engine

3. `internal/core/modelnormalize/rules.go`
- provider/family parsing rules

4. `internal/core/modelnormalize/window.go`
- window normalization/bucketing

5. `internal/core/modelnormalize/normalize_test.go`
- parser and confidence tests

## Modified files

1. `internal/core/types.go`
- add `ModelUsage []ModelUsageRecord`

2. `internal/core/snapshot_normalize.go`
- run fallback extractor when `ModelUsage` is empty
- normalize model records into canonical IDs

3. `internal/config/config.go`
- add `ModelNormalizationConfig`

4. `internal/tui/analytics.go`
- switch model table extraction to `ModelUsage`
- add provider split rendering

5. `internal/providers/openrouter/openrouter.go` (phase 2)
6. `internal/providers/claude_code/claude_code.go` (phase 2)
7. `internal/providers/codex/codex.go` (phase 2)
8. `internal/providers/gemini_cli/gemini_cli.go` (phase 2)
9. `internal/providers/copilot/copilot.go` (phase 2)
10. `internal/providers/cursor/cursor.go` (phase 2)

## 9) Backward Compatibility

1. Existing config remains valid.
2. Existing `Metrics`/`Raw` consumers continue to work.
3. Existing UI sections continue to render.
4. New model normalization can be toggled via config (`enabled`).

## 10) Testing Strategy

## Unit tests (core)

1. Snapshot alias parsing:
- `claude-opus-4-6-20260219` -> lineage `anthropic/claude-opus-4.6`, release `...@20260219`

2. Alias parsing:
- `gpt-4.1` and `gpt-4.1-2025-04-14` map to same lineage, different release

3. Vendor-prefix parsing:
- `anthropic/claude-opus-4.6` recognized vendor and lineage

4. Cursor-style intent normalization:
- `claude-4.6-opus-high-thinking` -> lineage `anthropic/claude-opus-4.6`

5. Unknown model fallback:
- unresolved IDs stay distinct and do not merge aggressively

## Integration tests

1. Multi-snapshot cross-provider aggregation with mixed raw IDs.
2. Window-separation correctness.
3. Config override precedence over heuristics.
4. Analytics rendering includes provider split rows.

## Regression tests

1. Existing analytics behavior still works when `ModelUsage` absent.
2. Existing providers without phase-2 changes still show model data.

## 11) Rollout Plan

## Milestone A: Core scaffolding

- Add data types, normalizer, fallback extractor, tests.
- No provider changes.
- Analytics can already use normalized grouping via fallback.

## Milestone B: Provider-native records

- Add `AppendModelUsageRecord` calls provider-by-provider.
- Improve confidence and raw fidelity.

## Milestone C: UI intelligence

- Add provider split and confidence indicators.
- Add window selection controls if needed.

## Milestone D: Optional overrides UX

- Expose model override editing in settings modal (optional).

## 12) Risks and Mitigations

1. Risk: over-merging distinct models.
- Mitigation: confidence threshold + lineage/release split + overrides.

2. Risk: under-merging aliases.
- Mitigation: provider-native records + explicit rules + user overrides.

3. Risk: window mismatch leading to misleading totals.
- Mitigation: explicit window bucketing and labels.

4. Risk: performance overhead.
- Mitigation: normalization is O(records), records are small per snapshot.

## 13) Acceptance Criteria

This design is complete when the implementation can answer, in analytics:

1. "Total tokens for `anthropic/claude-opus-4.6` in selected window"
2. "Per-provider/account split for that canonical model"
3. "Raw source IDs contributing to that canonical model"
4. "Confidence level and reason for canonical mapping"

without requiring a static model catalog and while preserving existing dynamic autodiscovery.
