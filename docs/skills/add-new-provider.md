# Skill: Add New Provider to OpenUsage

> **Invocation**: When a user asks to add, create, or implement a new AI provider.
> The user may supply the provider name as an argument (e.g. "add z.ai provider").

You are implementing a new AI usage/quota provider for the OpenUsage TUI dashboard.
This is a multi-step process. Follow every step precisely.

---

## Phase 0 — Quiz the User (MANDATORY)

Before writing any code, you MUST gather all of the following information.
Ask these questions conversationally but DO NOT proceed until every answer is obtained.
If the user doesn't know an answer, research it yourself (check the provider's docs, API reference, etc).

### Questions to ask:

1. **Provider name & ID**
   - What is the human-readable name? (e.g. "OpenAI", "DeepSeek", "Gemini CLI")
   - What should the snake_case provider ID be? (e.g. `openai`, `deep_seek`, `gemini_cli`)

2. **Authentication method** — which of these applies?
   - `api_key` — user sets an env var like `PROVIDER_API_KEY` (most common for API providers)
   - `oauth` — OAuth flow with stored credentials (e.g. Gemini CLI)
   - `cli` — shells out to a CLI binary (e.g. GitHub Copilot via `gh`)
   - `local` — reads local files/databases (e.g. Claude Code stats)
   - `token` — extracted from local storage (e.g. Cursor IDE token from SQLite)

3. **If API key auth**: What is the env var name? (e.g. `XAI_API_KEY`)

4. **Data source** — how do we get usage data?
   - HTTP API with rate-limit headers (probe a lightweight endpoint like `/v1/models`)
   - Dedicated usage/balance REST endpoint (e.g. DeepSeek `/user/balance`)
   - Local files (stats JSON, session files, SQLite databases)
   - CLI subprocess output
   - Combination of the above

5. **What metrics are available?** Try to identify:
   - Rate limits: RPM, TPM, RPD, TPD (from headers or API)
   - Spending: balance, credits, daily/weekly/monthly spend
   - Usage: messages, tokens (input/output/reasoning), sessions, tool calls
   - Account metadata: plan name, email, org, billing cycle

6. **API documentation URL** — link to the provider's rate-limit or usage docs

7. **Base URL** — the API base (e.g. `https://api.openai.com/v1`)

8. **Probe model** (if using header probing) — a cheap/default model to use for the probe request (e.g. `gpt-4.1-mini`)

9. **Color role** for the dashboard tile — pick one that doesn't conflict with existing providers:
   - `green` (OpenAI), `peach` (Anthropic), `lavender` (Cursor), `blue` (Gemini CLI)
   - `sky` (DeepSeek), `teal` (xAI), `yellow` (Groq), `sapphire` (Mistral)
   - `rosewater` (OpenRouter), `maroon` (Copilot), `flamingo` (Codex), `auto` (Claude Code)

10. **Does the provider support per-model usage breakdowns?** (for the Analytics tab)

---

## Phase 1 — Research

Before coding, look up the provider's API docs to understand:

- Exact HTTP endpoints, methods, headers
- Response JSON schemas
- Rate-limit header names and formats
- Any balance/credits/usage endpoints
- Authentication header format (`Bearer`, `x-api-key`, etc.)
- Error response codes and their meaning (401, 403, 429)

Document your findings in a brief summary before proceeding.

---

## Phase 2 — Create the Provider Package

### 2.1 Directory structure

Create `internal/providers/<provider_id>/` with these files:

```
internal/providers/<provider_id>/
├── <provider_id>.go       # Provider struct + Fetch() implementation
├── <provider_id>_test.go  # Tests
└── widget.go              # Dashboard widget configuration (only if customizing beyond defaults)
```

### 2.2 Provider implementation (`<provider_id>.go`)

The provider MUST:

1. **Define a `Provider` struct** that embeds `providerbase.Base`:

```go
package <provider_id>

import (
    "context"
    "fmt"
    "net/http"
    "time"

    "github.com/janekbaraniewski/openusage/internal/core"
    "github.com/janekbaraniewski/openusage/internal/parsers"
    "github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
    defaultBaseURL = "https://api.<provider>.com/v1"
)

type Provider struct {
    providerbase.Base
}
```

2. **Implement a `New()` constructor** that registers the `ProviderSpec`:

```go
func New() *Provider {
    return &Provider{
        Base: providerbase.New(core.ProviderSpec{
            ID: "<provider_id>",
            Info: core.ProviderInfo{
                Name:         "<Provider Name>",
                Capabilities: []string{"headers"},
                DocURL:       "https://docs.<provider>.com/rate-limits",
            },
            Auth: core.ProviderAuthSpec{
                Type:             core.ProviderAuthTypeAPIKey,
                APIKeyEnv:        "<PROVIDER_API_KEY>",
                DefaultAccountID: "<provider_id>",
            },
            Setup: core.ProviderSetupSpec{
                Quickstart: []string{"Set <PROVIDER_API_KEY> to a valid API key."},
            },
            Dashboard: dashboardWidget(),
        }),
    }
}
```

3. **Implement the `Fetch()` method** — this is the core data collection logic.

Key rules for `Fetch()`:
- First param is `context.Context` — pass it to all HTTP requests via `http.NewRequestWithContext`
- Second param is `core.AccountConfig` — use `acct.ResolveAPIKey()` for API key, `acct.BaseURL` for custom base URL, `acct.Binary` for CLI path
- Return `(core.UsageSnapshot, error)`
- For auth failures: return a valid snapshot with `Status: core.StatusAuth` and `err == nil`
- For rate limiting: return snapshot with `Status: core.StatusLimited` and `err == nil`
- For fatal errors (network failure, bad request): return `(core.UsageSnapshot{}, err)`
- Always prefix error messages with provider name: `fmt.Errorf("<provider_id>: creating request: %w", err)`
- Initialize all maps: `Metrics: make(map[string]core.Metric)`, `Resets: make(map[string]time.Time)`, etc.

#### Pattern A: HTTP header probing (simplest — for providers that expose rate-limit headers)

```go
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
    apiKey := acct.ResolveAPIKey()
    if apiKey == "" {
        return core.UsageSnapshot{
            ProviderID: p.ID(),
            AccountID:  acct.ID,
            Timestamp:  time.Now(),
            Status:     core.StatusAuth,
            Message:    "no API key found (set <ENV_VAR> or configure token)",
        }, nil
    }

    baseURL := acct.BaseURL
    if baseURL == "" {
        baseURL = defaultBaseURL
    }

    url := baseURL + "/models/<default_model>"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
    if err != nil {
        return core.UsageSnapshot{}, fmt.Errorf("<provider_id>: creating request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return core.UsageSnapshot{}, fmt.Errorf("<provider_id>: request failed: %w", err)
    }
    defer resp.Body.Close()

    snap := core.UsageSnapshot{
        ProviderID: p.ID(),
        AccountID:  acct.ID,
        Timestamp:  time.Now(),
        Metrics:    make(map[string]core.Metric),
        Resets:     make(map[string]time.Time),
        Raw:        parsers.RedactHeaders(resp.Header),
    }

    switch resp.StatusCode {
    case http.StatusUnauthorized, http.StatusForbidden:
        snap.Status = core.StatusAuth
        snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
        return snap, nil
    case http.StatusTooManyRequests:
        snap.Status = core.StatusLimited
        snap.Message = "rate limited (HTTP 429)"
    }

    parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
        "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
    parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
        "x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

    if snap.Status == "" {
        snap.Status = core.StatusOK
        snap.Message = "OK"
    }

    return snap, nil
}
```

#### Pattern B: REST API + balance endpoint (like DeepSeek)

Split into helper methods: `fetchBalance()`, `fetchRateLimits()`, etc.
Parse JSON responses into `core.Metric` entries.
Use `snap.SetAttribute("key", "value")` for account metadata.

#### Pattern C: Local file readers (like Claude Code, Codex)

Read from known paths using `acct.Binary` or `acct.ExtraData["config_dir"]`.
Parse JSON/SQLite data. Populate metrics from parsed data.

### 2.3 Metric keys — naming conventions

| Category | Key pattern | Unit | Window | Examples |
|----------|------------|------|--------|----------|
| Rate limits | `rpm`, `tpm`, `rpd`, `tpd` | `requests`/`tokens` | `1m`/`1d` | `rpm`, `tpm` |
| Spending | `total_cost_usd`, `today_api_cost`, `7d_api_cost`, `monthly_spend` | `USD` | `current`/`today`/`7d`/`month` | `credit_balance` |
| Credits | `credit_balance`, `credits`, `plan_spend` | `USD`/`credits` | `current` | `credit_balance` |
| Usage counts | `messages_today`, `sessions_today`, `tool_calls_today` | `messages`/`sessions`/`calls` | `today` | `messages_today` |
| Token counts | `tokens_today`, `input_tokens`, `output_tokens` | `tokens` | varies | `today_input_tokens` |
| Plan | `plan_percent_used`, `spend_limit` | `%`/`USD` | varies | `plan_percent_used` |
| Per-model | `model_<model_name>_<metric>` | varies | varies | `model_gpt4_cost` |

### 2.4 Attribute keys — naming conventions

Use `snap.SetAttribute()` for metadata displayed in the details panel:

| Key | Description | Example value |
|-----|-------------|---------------|
| `account_email` | Account email | `user@example.com` |
| `account_name` | Account/key name | `My API Key` |
| `plan_name` | Plan tier name | `Pro`, `Free`, `Team` |
| `plan_type` | Plan type | `prepaid`, `postpaid` |
| `billing_cycle_start` | Billing period start | `2025-01-01` |
| `billing_cycle_end` | Billing period end | `2025-02-01` |
| `cli_version` | Tool version | `1.2.3` |
| `auth_type` | How auth was resolved | `api_key`, `oauth` |

### 2.5 ModelUsage records (for Analytics tab)

If the provider returns per-model breakdowns, populate `snap.ModelUsage`:

```go
snap.ModelUsage = append(snap.ModelUsage, core.ModelUsageRecord{
    RawModelID:   "gpt-4o-2025-01-01",
    ProviderSlug: "<provider_id>",
    InputTokens:  1234,
    OutputTokens: 567,
    TotalCost:    0.0042,
    RequestCount: 15,
})
```

### 2.6 DailySeries (for Analytics charts)

If the provider has historical daily data, populate `snap.DailySeries`:

```go
snap.DailySeries = map[string][]core.TimePoint{
    "cost": {
        {Date: "2025-01-15", Value: 1.23},
        {Date: "2025-01-16", Value: 2.34},
    },
}
```

---

## Phase 3 — Dashboard Widget Configuration

### 3.1 When to use defaults vs custom widget

- **Use defaults** (via `providerbase.DefaultDashboard(providerbase.WithColorRole(...))`) for simple header-probing providers with just RPM/TPM.
- **Create `widget.go`** when the provider has rich metrics (credits, spending, activity, per-model data).

### 3.2 Custom widget (`widget.go`)

```go
package <provider_id>

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
    cfg := core.DefaultDashboardWidget()

    cfg.ColorRole = core.DashboardColorRole<Color>

    // Gauge priority — which metrics show as gauge bars in the tile (need Limit+Remaining or Limit+Used)
    cfg.GaugePriority = []string{
        "credit_balance", "spend_limit", "rpm", "tpm",
    }
    cfg.GaugeMaxLines = 2

    // Compact rows — summary pills shown in the tile (2-3 rows, 3-5 segments each)
    cfg.CompactRows = []core.DashboardCompactRow{
        {Label: "Credits", Keys: []string{"credit_balance", "plan_spend", "monthly_spend"}, MaxSegments: 4},
        {Label: "Usage", Keys: []string{"rpm", "tpm", "rpd", "tpd"}, MaxSegments: 4},
        {Label: "Activity", Keys: []string{"messages_today", "sessions_today", "requests_today"}, MaxSegments: 4},
    }

    // Metric label overrides for the detail panel
    cfg.MetricLabelOverrides["custom_metric"] = "Custom Metric Label"

    // Compact label overrides for tile pills (keep very short: 3-6 chars)
    cfg.CompactMetricLabelOverrides["custom_metric"] = "short"

    // Hide noisy metrics from the tile
    cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")
    cfg.SuppressZeroMetricKeys = []string{"some_usually_zero_metric"}

    // Raw groups — metadata sections in the detail panel
    cfg.RawGroups = append(cfg.RawGroups, core.DashboardRawGroup{
        Label: "API Key Info",
        Keys:  []string{"key_name", "key_type", "expires_at"},
    })

    return cfg
}
```

### 3.3 Widget design principles

- **Gauges**: Only metrics with both `Limit` and `Remaining` (or `Limit` and `Used`) render as gauge bars. Put the most meaningful resource-constraint metric first in `GaugePriority`.
- **Compact rows**: The tile shows 2-3 rows of compact pills. Design rows covering Credits/Spending, Rate Limits/Usage, and Activity/Tokens.
- **Color**: Choose a color role that doesn't clash with neighboring providers (see the map in Phase 0 Q9).
- **Detail panel**: The default sections (Usage, Spending, Tokens, Activity) work for most providers. Customize `DetailWidget.Sections` only if the provider has a unique data layout.

---

## Phase 4 — Register the Provider

### 4.1 Add to registry

Edit `internal/providers/registry.go` — import the new package and add `<provider_id>.New()` to the `AllProviders()` slice.

### 4.2 Add auto-detection (if applicable)

#### For API key providers

Edit `internal/detect/detect.go` — add to the `envKeyMapping` slice:

```go
{"<PROVIDER_API_KEY>", "<provider_id>", "<account_id>"},
```

#### For CLI/local tool providers

Add a `detect<ProviderName>(result *Result)` function that uses `findBinary()`, checks config dirs, and calls `addAccount()`. Then call it from `AutoDetect()`.

### 4.3 Add example config

Update `configs/example_settings.json` — add an account entry to the `accounts` array:

```json
{
    "id": "<provider_id>",
    "provider": "<provider_id>",
    "api_key_env": "<PROVIDER_API_KEY>"
}
```

---

## Phase 5 — Write Tests

### 5.1 Required test cases (minimum 3)

1. **`TestFetch_Success`** — happy path with mocked HTTP server returning expected headers/JSON
2. **`TestFetch_AuthRequired`** — missing API key returns `StatusAuth`
3. **`TestFetch_RateLimited`** — HTTP 429 returns `StatusLimited`

### 5.2 Test template

```go
package <provider_id>

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/janekbaraniewski/openusage/internal/core"
)

func float64Ptr(v float64) *float64 { return &v }

func TestFetch_Success(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("x-ratelimit-limit-requests", "100")
        w.Header().Set("x-ratelimit-remaining-requests", "95")
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"ok": true}`))
    }))
    defer server.Close()

    os.Setenv("TEST_<PROVIDER>_KEY", "test-key-value")
    defer os.Unsetenv("TEST_<PROVIDER>_KEY")

    p := New()
    acct := core.AccountConfig{
        ID:        "test-<provider_id>",
        Provider:  "<provider_id>",
        APIKeyEnv: "TEST_<PROVIDER>_KEY",
        BaseURL:   server.URL,
    }

    snap, err := p.Fetch(context.Background(), acct)
    if err != nil {
        t.Fatalf("Fetch() error: %v", err)
    }
    if snap.Status != core.StatusOK {
        t.Errorf("Status = %v, want OK", snap.Status)
    }

    metric, ok := snap.Metrics["rpm"]
    if !ok {
        t.Fatal("missing rpm metric")
    }
    if metric.Limit == nil || *metric.Limit != 100 {
        t.Errorf("rpm limit = %v, want 100", metric.Limit)
    }
}

func TestFetch_AuthRequired(t *testing.T) {
    os.Unsetenv("TEST_<PROVIDER>_MISSING")

    p := New()
    acct := core.AccountConfig{
        ID:        "test-<provider_id>",
        Provider:  "<provider_id>",
        APIKeyEnv: "TEST_<PROVIDER>_MISSING",
    }

    snap, err := p.Fetch(context.Background(), acct)
    if err != nil {
        t.Fatalf("Fetch() error: %v", err)
    }
    if snap.Status != core.StatusAuth {
        t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
    }
}

func TestFetch_RateLimited(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusTooManyRequests)
        w.Write([]byte(`{"error": "rate limited"}`))
    }))
    defer server.Close()

    os.Setenv("TEST_<PROVIDER>_KEY", "test-key-value")
    defer os.Unsetenv("TEST_<PROVIDER>_KEY")

    p := New()
    acct := core.AccountConfig{
        ID:        "test-<provider_id>",
        Provider:  "<provider_id>",
        APIKeyEnv: "TEST_<PROVIDER>_KEY",
        BaseURL:   server.URL,
    }

    snap, err := p.Fetch(context.Background(), acct)
    if err != nil {
        t.Fatalf("Fetch() error: %v", err)
    }
    if snap.Status != core.StatusLimited {
        t.Errorf("Status = %v, want LIMITED", snap.Status)
    }
}
```

### 5.3 Additional test cases for rich providers

- `TestFetch_ParsesBalance` — if the provider has a balance endpoint
- `TestFetch_ParsesUsage` — if it parses usage/generation data
- `TestFetch_ServerError` — HTTP 500 handling
- `TestFetch_MalformedJSON` — graceful handling of bad response bodies
- `TestFetch_CustomBaseURL` — ensure `acct.BaseURL` override works

---

## Phase 6 — Verify

After implementation, run these commands:

```bash
go build ./cmd/openusage
go test ./internal/providers/<provider_id>/ -v
go test ./internal/providers/... -v
make test
make vet
```

---

## Checklist

Before marking the provider as done, verify ALL items:

- [ ] `Provider` struct embeds `providerbase.Base`
- [ ] `New()` constructor fills in complete `ProviderSpec` (ID, Info, Auth, Setup, Dashboard)
- [ ] `Fetch()` handles: missing key -> `StatusAuth`, HTTP 401/403 -> `StatusAuth`, HTTP 429 -> `StatusLimited`
- [ ] `Fetch()` uses `http.NewRequestWithContext(ctx, ...)` for all HTTP calls
- [ ] `Fetch()` wraps errors with provider name prefix
- [ ] All maps initialized with `make()`
- [ ] Provider registered in `internal/providers/registry.go`
- [ ] Auto-detection added in `internal/detect/detect.go` (env key or tool detection)
- [ ] Example config entry added to `configs/example_settings.json`
- [ ] At least 3 tests: success, auth-required, rate-limited
- [ ] Tests use `httptest.NewServer`, `TEST_`-prefixed env vars, no external calls
- [ ] `go build ./cmd/openusage` succeeds
- [ ] `go test ./internal/providers/<provider_id>/ -v` passes
- [ ] `make vet` passes
- [ ] Dashboard widget has a unique `ColorRole` not conflicting with existing providers
- [ ] Widget `CompactRows` designed with 2-3 meaningful rows
- [ ] Widget `GaugePriority` puts the most useful metric first
