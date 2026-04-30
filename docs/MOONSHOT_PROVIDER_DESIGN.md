# Moonshot Provider Design

Date: 2026-04-30
Status: Proposed
Author: Jan Baraniewski

Driven by GitHub issue #79 — add full openusage support for Moonshot AI (Kimi). Targeting "max tier" parity with the richer existing providers (OpenRouter / Cursor level), constrained by what Moonshot's API actually exposes.

## 1. Problem Statement

openusage doesn't track Moonshot AI usage. Users with `MOONSHOT_API_KEY` set get nothing — no tile, no balance, no rate limits, no per-model breakdown.

## 2. Goals

1. Auto-detect `MOONSHOT_API_KEY` and create a Moonshot account on startup, with an out-of-the-box dashboard tile.
2. Surface the prepaid balance broken into `available / voucher / cash` so users can see what's left and what's free vs paid.
3. Surface org-level rate caps (`max_request_per_minute`, `max_token_per_minute`, `max_concurrency`, `max_token_quota`) and the user's auto-tier (`user_group_id`) for context.
4. Support **both** Moonshot variants: `api.moonshot.ai` (international, USD) as the default and `api.moonshot.cn` (China, CNY) for users in China — one provider, configurable base URL.
5. Surface per-model usage and cost from telemetry events automatically (`provider_id=moonshot` events from OpenCode, future hooks) — no plumbing changes required for this, just verify it works once the account exists.
6. Handle auth/rate-limit/error states cleanly with statuses (`AUTH`, `LIMITED`, `ERROR`).
7. Hit the full polish checklist: registry, env-detect, example config, README, providers.md, website card, design doc, tests covering success / auth / 429 / malformed / base-URL override.

## 3. Non-Goals

1. **Perplexity provider** — separate PR, follow-up.
2. **Daily-series chart from REST** — Moonshot's API doesn't expose historical daily usage. The Analytics tab will populate from telemetry only (existing pipeline).
3. **Per-model breakdown from REST** — same; telemetry only.
4. **`/v1/models` enumeration on every poll** — wasteful; we use `/v1/users/me` for limits.
5. **Subscription/plan tracking** — there is no API-platform subscription on Moonshot. The auto-tier (`user_group_id`) is surfaced as an attribute, nothing else.
6. **OAuth** — Moonshot uses API keys only. No OAuth flow.
7. **Token / spend cost computation from raw `usage` blocks** — no public price table API; surfacing telemetry-derived cost only.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | Add `DashboardColorRoleMauve` constant to `internal/core/widget.go`. |
| providers | major | New `internal/providers/moonshot/` package with provider, widget, and tests. |
| TUI | minor | Map `DashboardColorRoleMauve` in `styles.go:ProviderColor` to a theme color. Add `Mauve` field to `Theme` and to all 17 bundled theme JSONs (mechanical). |
| config | none | |
| detect | minor | Add `{"MOONSHOT_API_KEY", "moonshot", "moonshot-ai"}` to `envKeyMapping`. |
| daemon | none | |
| telemetry | none | Existing `provider_id=moonshot` events automatically attribute once the account exists (matcher does direct id matching). No mapping table change needed. |
| CLI | none | |
| docs/website | minor | README provider table row, `docs/providers.md` block, `website/src/App.jsx` provider card, `configs/example_settings.json` entry. |

### Existing Design Doc Overlap

- `docs/skills/add-new-provider.md` — the skill we're following.
- No active design docs overlap.

## 5. Detailed Design

### 5.1 Provider package

`internal/providers/moonshot/` contains:

- `moonshot.go` — `Provider` struct, `New()`, `Fetch()`.
- `widget.go` — custom `dashboardWidget()` for the rich tile.
- `moonshot_test.go` — required tests + parser tests.

```go
// internal/providers/moonshot/moonshot.go
package moonshot

const (
    defaultBaseURL = "https://api.moonshot.ai"
    cnBaseURL      = "https://api.moonshot.cn"
    userInfoPath   = "/v1/users/me"
    balancePath    = "/v1/users/me/balance"
)

type userInfoResponse struct {
    Code   int          `json:"code"`
    Status bool         `json:"status"`
    Data   userInfoData `json:"data"`
}
type userInfoData struct {
    AccessKey    accessKey    `json:"access_key"`
    Organization organization `json:"organization"`
    Project      project      `json:"project"`
    User         userBlock    `json:"user"`
    UserGroupID  string       `json:"user_group_id"`
}
type organization struct {
    ID                  string `json:"id"`
    MaxConcurrency      int    `json:"max_concurrency"`
    MaxRequestPerMinute int    `json:"max_request_per_minute"`
    MaxTokenPerMinute   int    `json:"max_token_per_minute"`
    MaxTokenQuota       int64  `json:"max_token_quota"`
}
// ...

type balanceResponse struct {
    Code   int         `json:"code"`
    Status bool        `json:"status"`
    Data   balanceData `json:"data"`
}
type balanceData struct {
    AvailableBalance float64 `json:"available_balance"`
    VoucherBalance   float64 `json:"voucher_balance"`
    CashBalance      float64 `json:"cash_balance"`
}
```

### 5.2 Provider spec & auto-detection

```go
func New() *Provider {
    return &Provider{
        Base: providerbase.New(core.ProviderSpec{
            ID: "moonshot",
            Info: core.ProviderInfo{
                Name:         "Moonshot",
                Capabilities: []string{"balance_endpoint", "user_info_endpoint"},
                DocURL:       "https://platform.moonshot.ai/docs/api/list",
            },
            Auth: core.ProviderAuthSpec{
                Type:             core.ProviderAuthTypeAPIKey,
                APIKeyEnv:        "MOONSHOT_API_KEY",
                DefaultAccountID: "moonshot-ai",
            },
            Setup: core.ProviderSetupSpec{
                Quickstart: []string{
                    "Set MOONSHOT_API_KEY to a valid Moonshot key from https://platform.moonshot.ai.",
                    "For Moonshot.cn (China), override BaseURL to https://api.moonshot.cn in your account config.",
                },
            },
            Dashboard: dashboardWidget(),
        }),
    }
}
```

`detect.envKeyMapping` gets `{"MOONSHOT_API_KEY", "moonshot", "moonshot-ai"}`.

### 5.3 Region handling: one provider, two base URLs

A single provider with a configurable base URL — same pattern as DeepSeek/Mistral/etc.:

- Default: `https://api.moonshot.ai` (USD).
- User override via `account.base_url = "https://api.moonshot.cn"` in `settings.json` (CNY).
- The provider detects which by base URL string and tags `currency` + `service_region` attributes accordingly.
- Auto-detection from env always creates the `.ai` (international) account; if a user wants `.cn`, they configure a second account manually with the same env or a different `api_key_env`.

This is simpler than two separate providers and matches the codebase's pattern. The trade-off is that a user with both .ai *and* .cn keys needs two manually-configured accounts, but that's an edge case acceptable for v1.

### 5.4 Fetch() flow

```
1. ResolveAPIKey() → if empty, return StatusAuth snapshot (no error).
2. ResolveBaseURL() → defaults to api.moonshot.ai.
3. Build snap = NewUsageSnapshot(p.ID(), acct.ID).
4. SetAttribute service_region (international/china) and currency (USD/CNY).
5. fetchUserInfo(): GET /v1/users/me — populate org limits, tier, ids.
   - On 401/403: snap.Status = StatusAuth, return nil error.
   - On 429: snap.Status = StatusLimited, continue (limits are stale but we still want balance).
   - On 5xx: snap.Status = StatusError, return wrapped error.
6. fetchBalance(): GET /v1/users/me/balance — populate balance metrics.
   - Same status handling. Balance failures don't blow away user-info success.
7. Compute derived signals: balance_zero status promotion, etc.
8. shared.FinalizeStatus(&snap); return.
```

Both endpoints are idempotent GETs with no body and minimal payload; sequential is fine, no need to parallelize.

### 5.5 Metric keys

| Key | Meaning | Limit | Remaining | Used | Unit | Window |
|---|---|---|---|---|---|---|
| `available_balance` | Total spendable | (none) | yes | (none) | USD or CNY | `current` |
| `cash_balance` | Paid topup remaining | (none) | yes | (none) | USD or CNY | `current` |
| `voucher_balance` | Free credits remaining | (none) | yes | (none) | USD or CNY | `current` |
| `rpm` | Org request/min cap | yes | (none) | (none) | requests | `1m` |
| `tpm` | Org token/min cap | yes | (none) | (none) | tokens | `1m` |
| `concurrency_max` | Org concurrent requests | yes | (none) | (none) | requests | `current` |
| `total_token_quota` | Org lifetime token cap | yes | (none) | (none) | tokens | `current` |
| `model_<id>_*` | Per-model usage | from telemetry | from telemetry | from telemetry | varies | varies |

**Note on rate-limit *Remaining***: Moonshot doesn't return per-request remaining values, so we surface the cap as `Limit` only. The dashboard renders `rpm: 200/min` as text rather than a fillable gauge — `core.Metric` already supports this case (gauges only render when both Limit and Remaining are present).

### 5.6 Attributes

| Key | Value | Source |
|---|---|---|
| `account_tier` | `enterprise-tier-1` etc. | `user_group_id` |
| `service_region` | `international` / `china` | derived from base URL |
| `currency` | `USD` / `CNY` | derived from base URL |
| `org_id` | `org-d75c68bd25b647828b1071f3aff4c229` | `organization.id` |
| `project_id` | `proj-...` | `project.id` |
| `access_key_suffix` | last 4 chars of `access_key.id` | for safe display |
| `user_state` | `active` / etc. | `user.user_state` |

### 5.7 Status decision

```
balance.code != 0           → StatusError, message from balance.error/data
user-info 401/403           → StatusAuth
user-info or balance 429    → StatusLimited
user-info or balance 5xx    → StatusError
available_balance <= 0      → StatusLimited, message "balance exhausted"
available_balance < threshold (e.g. 1.0) → StatusNearLimit
otherwise                   → StatusOK, message "Balance: <amount> <currency>"
```

`shared.FinalizeStatus` already implements the OK / NearLimit thresholds via `core.Metric` warn/crit comparisons; we set up the metrics correctly and let it work.

### 5.8 Custom widget

```go
func dashboardWidget() core.DashboardWidget {
    cfg := core.DefaultDashboardWidget()
    cfg.ColorRole = core.DashboardColorRoleMauve

    cfg.GaugePriority = []string{
        // Moonshot doesn't return Remaining for limits, so gauges
        // mostly show balance subdivisions.
        "available_balance", "cash_balance", "voucher_balance",
    }
    cfg.GaugeMaxLines = 2

    cfg.CompactRows = []core.DashboardCompactRow{
        {Label: "Balance", Keys: []string{"available_balance", "cash_balance", "voucher_balance"}, MaxSegments: 4},
        {Label: "Limits",  Keys: []string{"rpm", "tpm", "concurrency_max"}, MaxSegments: 4},
        {Label: "Activity", Keys: []string{"messages_today", "tokens_today", "cost_today"}, MaxSegments: 4}, // populated from telemetry
    }

    cfg.MetricLabelOverrides = map[string]string{
        "available_balance": "Available",
        "cash_balance":      "Cash",
        "voucher_balance":   "Vouchers",
        "rpm":               "Req / min",
        "tpm":               "Tokens / min",
        "concurrency_max":   "Concurrency",
        "total_token_quota": "Token Quota",
    }
    cfg.CompactMetricLabelOverrides = map[string]string{
        "available_balance": "avail",
        "cash_balance":      "cash",
        "voucher_balance":   "vouch",
        "concurrency_max":   "conc",
        "total_token_quota": "tquota",
    }
    cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")

    cfg.RawGroups = append(cfg.RawGroups,
        core.DashboardRawGroup{Label: "Account", Keys: []string{"account_tier", "service_region", "currency", "user_state"}},
        core.DashboardRawGroup{Label: "Org",     Keys: []string{"org_id", "project_id", "access_key_suffix"}},
    )
    return cfg
}
```

### 5.9 Color: add Mauve

`internal/core/widget.go`: add `DashboardColorRoleMauve = "mauve"`.

`internal/tui/themes.go` `Theme` struct: add `Mauve lipgloss.Color`.

`internal/tui/styles.go`: add `colorMauve` global, populate from `t.Mauve` in `applyTheme`, add the Mauve case to `ProviderColor`'s switch.

All 17 bundled theme JSON files: add a `"mauve": "<hex>"` entry. Mauve is part of Catppuccin's official palette so most themes already use mauve-adjacent hues for their accent — defaults can be derived from existing theme aesthetics. For non-Catppuccin themes (Gruvbox, Monokai, etc.) pick a perceptually similar purple/violet that fits the theme.

### 5.10 Error envelope handling

Moonshot returns two distinct error shapes:
- OpenAI-compat: `{"error":{"message":"...","type":"..."}}` (auth errors)
- Moonshot internal: `{"code":5,"error":"url.not_found","message":"...","scode":"0x5","status":false}` (404s)

Both are handled the same: extract message, set status, surface in `snap.Message`.

### 5.N Backward Compatibility

- `MOONSHOT_API_KEY` was not previously detected, so adding it is purely additive.
- New color role `Mauve` doesn't break any existing theme — every bundled theme just gains a new field.
- Telemetry events with `provider_id=moonshot` already exist in DBs of users running OpenCode hooks; once an account is configured, those events auto-attribute (matcher does direct match).

## 6. Alternatives Considered

### A: Two separate providers (`moonshot_ai` + `moonshot_cn`)

Cleaner conceptual separation but doubles provider code, doubles registry entries, doubles tests, and the only real difference is base URL + currency. Rejected — the BaseURL pattern is established (DeepSeek/Mistral/etc.).

### B: Header-probing instead of REST

Moonshot doesn't expose `x-ratelimit-*` headers, so this would yield no useful metrics. Rejected.

### C: Skip the `/v1/users/me` call, just pull balance

Saves one HTTP request but loses RPM/TPM/concurrency/quota — those are the most commonly-asked data points after balance. Two requests every 30s is trivial cost. Rejected.

### D: Compute spend from chat-completion telemetry locally

Possible but requires a hardcoded price table per model, which goes stale. Skipped for v1; the dashboard's existing telemetry pipeline already captures `cost_usd` when OpenCode/etc. emit it. Re-evaluate if telemetry sources don't carry cost.

### E: Surface kimi.com (consumer) Kimi+ subscription

Different auth surface (web cookies, not API keys), unstable, not part of the API platform. Rejected.

## 7. Implementation Tasks

### Task 1: add `DashboardColorRoleMauve` and theme support
Files: `internal/core/widget.go`, `internal/tui/themes.go`, `internal/tui/styles.go`, all 17 files in `internal/tui/bundled_themes/*.json`
Depends on: none
Description: Add the constant, the theme field, the global, the switch case, and a hex value per theme.
Tests: none new — visual only; existing theme tests must still pass.

### Task 2: Moonshot provider package
Files: `internal/providers/moonshot/moonshot.go`, `internal/providers/moonshot/widget.go`, `internal/providers/moonshot/moonshot_test.go`
Depends on: Task 1
Description: Implement provider per Section 5. Tests cover success (both endpoints succeed), auth (missing key, 401), rate limited (429 on user-info or balance), malformed JSON, custom base URL override (.cn), partial failure (user-info OK + balance 5xx — snapshot still shows limits).
Tests:
- `TestFetch_Success_International` (USD, .ai)
- `TestFetch_Success_China` (CNY, .cn — verifies currency + region attribute)
- `TestFetch_AuthRequired_NoKey`
- `TestFetch_AuthRequired_401`
- `TestFetch_RateLimited_429`
- `TestFetch_BalancePartialFailure`
- `TestFetch_MalformedBalanceJSON`

### Task 3: registry + env-detect + example config
Files: `internal/providers/registry.go`, `internal/detect/detect.go`, `internal/detect/detect_test.go`, `configs/example_settings.json`
Depends on: Task 2
Description: Wire the provider into `AllProviders()`, add the env-key mapping entry, add an example account block in `configs/example_settings.json` (single `moonshot-ai` entry; comment hint about `.cn` override).
Tests: detect_test gains a case asserting `MOONSHOT_API_KEY` produces a moonshot account.

### Task 4: docs + website
Files: `README.md`, `docs/providers.md`, `website/src/App.jsx`
Depends on: Task 2
Description: Add Moonshot rows to README's API platform table, a section to `docs/providers.md`, and an entry to `apiPlatforms` in `App.jsx` with `icon("moonshot")`. Verify the icon ships in `website/dist/icons/`.
Tests: none.

### Task 5: end-to-end sanity script
Files: temporary — run during PR development, not committed.
Depends on: Task 2
Description: Build the binary, point it at the test key, confirm the tile renders with balance, limits, and tier, and that telemetry events with `provider_id=moonshot` (if any exist) attribute correctly. Capture a screenshot for the PR description.

### Dependency Graph

```
Task 1 ──┐
         └─→ Task 2 ──┬─→ Task 3
                     ├─→ Task 4
                     └─→ Task 5 (manual verification)
```

Tasks 3 and 4 can run in parallel after Task 2.

## 8. Follow-ups (out of scope for this PR)

- **Generic regional/multi-account UX.** Several providers now have regional or per-tenant variants (Alibaba Cloud, Google Gemini API, Moonshot, future Perplexity if Pro vs API differs). The current "user manually edits settings.json to add a second account" flow doesn't scale well. Design a first-class affordance for "add another account of provider X" in Settings → 5 KEYS, with a region/base-URL picker. Worth its own design doc.
