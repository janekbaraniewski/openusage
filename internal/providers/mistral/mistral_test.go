package mistral

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// newTestServer creates an httptest.Server that routes requests to the
// appropriate Mistral API endpoint handler.
func newTestServer(
	subscriptionHandler func(w http.ResponseWriter, r *http.Request),
	usageHandler func(w http.ResponseWriter, r *http.Request),
	modelsHandler func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/billing/subscription":
			subscriptionHandler(w, r)
		case r.URL.Path == "/billing/usage":
			usageHandler(w, r)
		case r.URL.Path == "/models":
			modelsHandler(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetch_FullSuccess(t *testing.T) {
	server := newTestServer(
		// /billing/subscription
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id": "sub_123",
				"plan": "team",
				"monthly_budget": 500.00,
				"credit_balance": 42.50
			}`))
		},
		// /billing/usage
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"object": "list",
				"data": [
					{"model": "mistral-large", "input_tokens": 100000, "output_tokens": 50000, "total_cost": 12.50},
					{"model": "mistral-small", "input_tokens": 200000, "output_tokens": 75000, "total_cost": 3.25}
				],
				"total_cost": 15.75
			}`))
		},
		// /models — rate limit headers
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ratelimit-limit", "100")
			w.Header().Set("ratelimit-remaining", "95")
			w.Header().Set("ratelimit-reset", "30")
			w.Header().Set("x-ratelimit-limit-requests", "60")
			w.Header().Set("x-ratelimit-remaining-requests", "55")
			w.Header().Set("x-ratelimit-reset-requests", "30s")
			w.Header().Set("x-ratelimit-limit-tokens", "1000000")
			w.Header().Set("x-ratelimit-remaining-tokens", "950000")
			w.Header().Set("x-ratelimit-reset-tokens", "30s")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "mistral-large"}]}`))
		},
	)
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-mistral",
		Provider: "mistral",
		Token:    "test-api-key",
		BaseURL:  server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// Verify plan in Raw
	if plan, ok := snap.Raw["plan"]; !ok || plan != "team" {
		t.Errorf("Raw[plan] = %q, want %q", plan, "team")
	}

	// Verify monthly_budget metric
	budget, ok := snap.Metrics["monthly_budget"]
	if !ok {
		t.Fatal("missing monthly_budget metric")
	}
	if budget.Limit == nil || *budget.Limit != 500.00 {
		t.Errorf("monthly_budget limit = %v, want 500.00", budget.Limit)
	}
	if budget.Unit != "EUR" {
		t.Errorf("monthly_budget unit = %q, want %q", budget.Unit, "EUR")
	}
	if budget.Window != "1mo" {
		t.Errorf("monthly_budget window = %q, want %q", budget.Window, "1mo")
	}

	// Verify credit_balance metric
	balance, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance metric")
	}
	if balance.Remaining == nil || *balance.Remaining != 42.50 {
		t.Errorf("credit_balance remaining = %v, want 42.50", balance.Remaining)
	}
	if balance.Unit != "EUR" {
		t.Errorf("credit_balance unit = %q, want %q", balance.Unit, "EUR")
	}
	if balance.Window != "current" {
		t.Errorf("credit_balance window = %q, want %q", balance.Window, "current")
	}

	// Verify monthly_spend metric (with limit linked from budget)
	spend, ok := snap.Metrics["monthly_spend"]
	if !ok {
		t.Fatal("missing monthly_spend metric")
	}
	if spend.Used == nil || *spend.Used != 15.75 {
		t.Errorf("monthly_spend used = %v, want 15.75", spend.Used)
	}
	if spend.Limit == nil || *spend.Limit != 500.00 {
		t.Errorf("monthly_spend limit = %v, want 500.00 (linked from budget)", spend.Limit)
	}
	if spend.Remaining == nil || *spend.Remaining != 484.25 {
		t.Errorf("monthly_spend remaining = %v, want 484.25", spend.Remaining)
	}
	if spend.Unit != "EUR" {
		t.Errorf("monthly_spend unit = %q, want %q", spend.Unit, "EUR")
	}
	if spend.Window != "1mo" {
		t.Errorf("monthly_spend window = %q, want %q", spend.Window, "1mo")
	}

	// Verify monthly_input_tokens
	inputTokens, ok := snap.Metrics["monthly_input_tokens"]
	if !ok {
		t.Fatal("missing monthly_input_tokens metric")
	}
	if inputTokens.Used == nil || *inputTokens.Used != 300000 {
		t.Errorf("monthly_input_tokens used = %v, want 300000", inputTokens.Used)
	}
	if inputTokens.Unit != "tokens" {
		t.Errorf("monthly_input_tokens unit = %q, want %q", inputTokens.Unit, "tokens")
	}
	if inputTokens.Window != "1mo" {
		t.Errorf("monthly_input_tokens window = %q, want %q", inputTokens.Window, "1mo")
	}

	// Verify monthly_output_tokens
	outputTokens, ok := snap.Metrics["monthly_output_tokens"]
	if !ok {
		t.Fatal("missing monthly_output_tokens metric")
	}
	if outputTokens.Used == nil || *outputTokens.Used != 125000 {
		t.Errorf("monthly_output_tokens used = %v, want 125000", outputTokens.Used)
	}
	if outputTokens.Unit != "tokens" {
		t.Errorf("monthly_output_tokens unit = %q, want %q", outputTokens.Unit, "tokens")
	}

	// Verify rate limit metrics — rpm (from ratelimit-* headers)
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 100 {
		t.Errorf("rpm limit = %v, want 100", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 95 {
		t.Errorf("rpm remaining = %v, want 95", rpm.Remaining)
	}
	if rpm.Unit != "requests" {
		t.Errorf("rpm unit = %q, want %q", rpm.Unit, "requests")
	}
	if rpm.Window != "1m" {
		t.Errorf("rpm window = %q, want %q", rpm.Window, "1m")
	}

	// Verify rate limit metrics — rpm_alt (from x-ratelimit-*-requests headers)
	rpmAlt, ok := snap.Metrics["rpm_alt"]
	if !ok {
		t.Fatal("missing rpm_alt metric")
	}
	if rpmAlt.Limit == nil || *rpmAlt.Limit != 60 {
		t.Errorf("rpm_alt limit = %v, want 60", rpmAlt.Limit)
	}
	if rpmAlt.Remaining == nil || *rpmAlt.Remaining != 55 {
		t.Errorf("rpm_alt remaining = %v, want 55", rpmAlt.Remaining)
	}
	if rpmAlt.Unit != "requests" {
		t.Errorf("rpm_alt unit = %q, want %q", rpmAlt.Unit, "requests")
	}

	// Verify rate limit metrics — tpm (from x-ratelimit-*-tokens headers)
	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 1000000 {
		t.Errorf("tpm limit = %v, want 1000000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 950000 {
		t.Errorf("tpm remaining = %v, want 950000", tpm.Remaining)
	}
	if tpm.Unit != "tokens" {
		t.Errorf("tpm unit = %q, want %q", tpm.Unit, "tokens")
	}
	if tpm.Window != "1m" {
		t.Errorf("tpm window = %q, want %q", tpm.Window, "1m")
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	p := New()
	acct := core.AccountConfig{
		ID:        "test-mistral",
		Provider:  "mistral",
		APIKeyEnv: "MISTRAL_TEST_MISSING_KEY_XYZZY",
		// No Token, no env var set → RequireAPIKey should return StatusAuth
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_SubscriptionError(t *testing.T) {
	server := newTestServer(
		// /billing/subscription — 500 error
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal server error"}`))
		},
		// /billing/usage — succeeds
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"object": "list",
				"data": [
					{"model": "mistral-large", "input_tokens": 50000, "output_tokens": 25000, "total_cost": 8.00}
				],
				"total_cost": 8.00
			}`))
		},
		// /models — succeeds with rate limit headers
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-ratelimit-limit-requests", "60")
			w.Header().Set("x-ratelimit-remaining-requests", "58")
			w.Header().Set("x-ratelimit-reset-requests", "30s")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": [{"id": "mistral-large"}]}`))
		},
	)
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-mistral",
		Provider: "mistral",
		Token:    "test-api-key",
		BaseURL:  server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// Should still be OK — subscription failure is non-fatal
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK (subscription error is non-fatal)", snap.Status)
	}

	// Verify subscription_error recorded in Raw
	subErr, ok := snap.Raw["subscription_error"]
	if !ok {
		t.Fatal("missing subscription_error in Raw")
	}
	if subErr == "" {
		t.Error("subscription_error is empty, expected error message")
	}

	// Verify usage data still collected
	spend, ok := snap.Metrics["monthly_spend"]
	if !ok {
		t.Fatal("missing monthly_spend metric despite successful usage fetch")
	}
	if spend.Used == nil || *spend.Used != 8.00 {
		t.Errorf("monthly_spend used = %v, want 8.00", spend.Used)
	}
	// No budget available → spend should not have a linked limit
	if spend.Limit != nil {
		t.Errorf("monthly_spend limit = %v, want nil (no subscription data)", spend.Limit)
	}

	// Verify token metrics still collected
	inputTokens, ok := snap.Metrics["monthly_input_tokens"]
	if !ok {
		t.Fatal("missing monthly_input_tokens metric")
	}
	if inputTokens.Used == nil || *inputTokens.Used != 50000 {
		t.Errorf("monthly_input_tokens used = %v, want 50000", inputTokens.Used)
	}

	// Verify rate limit metrics still collected
	rpmAlt, ok := snap.Metrics["rpm_alt"]
	if !ok {
		t.Fatal("missing rpm_alt metric despite successful models fetch")
	}
	if rpmAlt.Limit == nil || *rpmAlt.Limit != 60 {
		t.Errorf("rpm_alt limit = %v, want 60", rpmAlt.Limit)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	server := newTestServer(
		// /billing/subscription — succeeds
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id": "sub_456",
				"plan": "free",
				"monthly_budget": 100.00,
				"credit_balance": 10.00
			}`))
		},
		// /billing/usage — succeeds
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"object": "list",
				"data": [
					{"model": "mistral-small", "input_tokens": 10000, "output_tokens": 5000, "total_cost": 0.50}
				],
				"total_cost": 0.50
			}`))
		},
		// /models — 429 rate limited
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": "rate limit exceeded"}`))
		},
	)
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "test-mistral",
		Provider: "mistral",
		Token:    "test-api-key",
		BaseURL:  server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// The fetchRateLimits code sets StatusLimited on 429, but then checks
	// "if snap.Status == core.StatusOK" which is false (it's LIMITED),
	// so it falls through to FinalizeStatus which keeps LIMITED.
	if snap.Status != core.StatusLimited {
		t.Errorf("Status = %v, want LIMITED", snap.Status)
	}

	// Subscription data should still be collected
	if plan, ok := snap.Raw["plan"]; !ok || plan != "free" {
		t.Errorf("Raw[plan] = %q, want %q", snap.Raw["plan"], "free")
	}

	budget, ok := snap.Metrics["monthly_budget"]
	if !ok {
		t.Fatal("missing monthly_budget metric despite successful subscription fetch")
	}
	if budget.Limit == nil || *budget.Limit != 100.00 {
		t.Errorf("monthly_budget limit = %v, want 100.00", budget.Limit)
	}

	// Usage data should still be collected
	spend, ok := snap.Metrics["monthly_spend"]
	if !ok {
		t.Fatal("missing monthly_spend metric despite successful usage fetch")
	}
	if spend.Used == nil || *spend.Used != 0.50 {
		t.Errorf("monthly_spend used = %v, want 0.50", spend.Used)
	}

	// Credit balance should still be collected
	balance, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance metric despite successful subscription fetch")
	}
	if balance.Remaining == nil || *balance.Remaining != 10.00 {
		t.Errorf("credit_balance remaining = %v, want 10.00", balance.Remaining)
	}
}
