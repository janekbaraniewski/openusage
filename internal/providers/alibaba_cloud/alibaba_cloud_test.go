package alibaba_cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func float64Ptr(v float64) *float64 { return &v }
func intPtr(v int) *int             { return &v }

func TestFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quotas" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"code": "Success",
			"message": "Success",
			"data": {
				"available": 100.50,
				"credits": 500.25,
				"spend_limit": 1000.0,
				"daily_spend": 15.30,
				"monthly_spend": 250.75,
				"usage": 1500000.0,
				"tokens_used": 1500000,
				"requests_used": 450,
				"rate_limit": {
					"rpm": 60,
					"tpm": 100000
				},
				"models": {
					"qwen-plus": {
						"rpm": 20,
						"tpm": 50000,
						"used": 500000.0,
						"limit": 1000000.0
					},
					"qwen-max": {
						"rpm": 10,
						"tpm": 50000,
						"used": 1000000.0,
						"limit": 2000000.0
					}
				},
				"billing_period": {
					"start": "2026-02-01",
					"end": "2026-03-01"
				}
			},
			"request_id": "test-123"
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_KEY", "test-key-value")
	defer os.Unsetenv("TEST_ALIBABA_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want %q", snap.Status, core.StatusOK)
	}

	if snap.Message != "OK" {
		t.Errorf("Message = %q, want OK", snap.Message)
	}

	// Check available_balance metric
	balanceMetric, ok := snap.Metrics["available_balance"]
	if !ok {
		t.Fatal("missing available_balance metric")
	}
	if balanceMetric.Limit == nil || *balanceMetric.Limit != 100.50 {
		t.Errorf("available_balance = %v, want 100.50", balanceMetric.Limit)
	}

	// Check credits metric
	creditsMetric, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance metric")
	}
	if creditsMetric.Limit == nil || *creditsMetric.Limit != 500.25 {
		t.Errorf("credit_balance = %v, want 500.25", creditsMetric.Limit)
	}

	// Check rate limits
	rpmMetric, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpmMetric.Limit == nil || *rpmMetric.Limit != 60 {
		t.Errorf("rpm = %v, want 60", rpmMetric.Limit)
	}

	// Check spending metrics
	dailySpend, ok := snap.Metrics["daily_spend"]
	if !ok {
		t.Fatal("missing daily_spend metric")
	}
	if dailySpend.Used == nil || *dailySpend.Used != 15.30 {
		t.Errorf("daily_spend = %v, want 15.30", dailySpend.Used)
	}

	// Check attributes
	startDate, ok := snap.Attributes["billing_cycle_start"]
	if !ok {
		t.Fatal("missing billing_cycle_start attribute")
	}
	if startDate != "2026-02-01" {
		t.Errorf("billing_cycle_start = %q, want 2026-02-01", startDate)
	}

	// Check per-model metrics
	qwenPlusUsage, ok := snap.Metrics["model_qwen-plus_used"]
	if !ok {
		t.Fatal("missing model_qwen-plus_used metric")
	}
	if qwenPlusUsage.Used == nil || *qwenPlusUsage.Used != 500000.0 {
		t.Errorf("model_qwen-plus_used = %v, want 500000.0", qwenPlusUsage.Used)
	}
}

func TestFetch_AuthRequired_MissingKey(t *testing.T) {
	os.Unsetenv("TEST_ALIBABA_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthRequired_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{
			"code": "InvalidApiKey",
			"message": "Invalid API-key provided.",
			"request_id": "test-123"
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_INVALID", "invalid-key")
	defer os.Unsetenv("TEST_ALIBABA_INVALID")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_INVALID",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{
			"code": "TooManyRequests",
			"message": "Rate limit exceeded",
			"request_id": "test-456"
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_RATELIMIT", "test-key")
	defer os.Unsetenv("TEST_ALIBABA_RATELIMIT")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_RATELIMIT",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusLimited {
		t.Errorf("Status = %q, want LIMITED", snap.Status)
	}
}

func TestFetch_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_ERROR", "test-key")
	defer os.Unsetenv("TEST_ALIBABA_ERROR")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_ERROR",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusError {
		t.Errorf("Status = %q, want ERROR", snap.Status)
	}
}

func TestFetch_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_MALFORMED", "test-key")
	defer os.Unsetenv("TEST_ALIBABA_MALFORMED")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_MALFORMED",
		BaseURL:   server.URL,
	}

	_, err := p.Fetch(context.Background(), acct)
	if err == nil {
		t.Fatal("expected Fetch() to error on malformed JSON")
	}
}

func TestFetch_CustomBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"code": "Success",
			"data": {
				"available": 50.0,
				"credits": 250.0,
				"rate_limit": {"rpm": 60, "tpm": 100000}
			}
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_CUSTOM", "test-key")
	defer os.Unsetenv("TEST_ALIBABA_CUSTOM")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_CUSTOM",
		BaseURL:   server.URL, // custom base URL override
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK", snap.Status)
	}

	balanceMetric, ok := snap.Metrics["available_balance"]
	if !ok {
		t.Fatal("missing available_balance metric")
	}
	if balanceMetric.Limit == nil || *balanceMetric.Limit != 50.0 {
		t.Errorf("available_balance = %v, want 50.0", balanceMetric.Limit)
	}
}

func TestFetch_PartialData(t *testing.T) {
	// Test that provider gracefully handles partial/minimal response data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"code": "Success",
			"data": {
				"available": 100.0
			}
		}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ALIBABA_PARTIAL", "test-key")
	defer os.Unsetenv("TEST_ALIBABA_PARTIAL")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-alibaba",
		Provider:  "alibaba_cloud",
		APIKeyEnv: "TEST_ALIBABA_PARTIAL",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want OK", snap.Status)
	}

	// Should have parsed the available balance
	balanceMetric, ok := snap.Metrics["available_balance"]
	if !ok {
		t.Fatal("missing available_balance metric")
	}
	if balanceMetric.Limit == nil || *balanceMetric.Limit != 100.0 {
		t.Errorf("available_balance = %v, want 100.0", balanceMetric.Limit)
	}
}
