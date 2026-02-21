package zen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_MissingAPIKey(t *testing.T) {
	os.Unsetenv("TEST_ZEN_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "zen-test",
		Provider:  "zen",
		APIKeyEnv: "TEST_ZEN_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %q, want %q", snap.Status, core.StatusAuth)
	}
}

func TestFetch_CatalogAndBillingLimited(t *testing.T) {
	resetProbeCache()

	const apiKey = "test-zen-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"type":"AuthError","error":{"type":"AuthError","message":"bad key"}}`))
			return
		}
		if got := r.Header.Get("x-api-key"); got != apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"type":"AuthError","error":{"type":"AuthError","message":"missing x-api-key"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[
				{"id":"glm-5-free","object":"model"},
				{"id":"gpt-5.1-codex-mini","object":"model"},
				{"id":"claude-sonnet-4-5","object":"model"},
				{"id":"unknown-model","object":"model"}
			]}`))
		case "/chat/completions":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id":"chat-1",
				"request_id":"req-chat-1",
				"model":"glm-5-free",
				"cost":"0",
				"usage":{
					"prompt_tokens":12,
					"completion_tokens":3,
					"total_tokens":15,
					"prompt_tokens_details":{"cached_tokens":4}
				}
			}`))
		case "/responses":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{
				"type":"CreditsError",
				"error":{
					"type":"CreditsError",
					"message":"No payment method. Add a payment method here: https://opencode.ai/workspace/wrk_ABC123/billing"
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_ZEN_KEY", apiKey)
	defer os.Unsetenv("TEST_ZEN_KEY")

	now := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	p := New()
	p.httpClient = server.Client()
	p.now = func() time.Time { return now }

	acct := core.AccountConfig{
		ID:        "zen-test",
		Provider:  "zen",
		APIKeyEnv: "TEST_ZEN_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %q, want %q (message=%s)", snap.Status, core.StatusLimited, snap.Message)
	}

	if got := snap.Raw["subscription_status"]; got != "inactive_no_payment_method" {
		t.Fatalf("subscription_status = %q, want inactive_no_payment_method", got)
	}
	if got := snap.Raw["billing_status"]; got != "payment_method_missing" {
		t.Fatalf("billing_status = %q, want payment_method_missing", got)
	}
	if got := snap.Raw["workspace_id"]; got != "wrk_ABC123" {
		t.Fatalf("workspace_id = %q, want wrk_ABC123", got)
	}
	if got := snap.Raw["billing_url"]; !strings.Contains(got, "/workspace/wrk_ABC123/billing") {
		t.Fatalf("billing_url = %q, want workspace billing URL", got)
	}

	assertMetricUsed(t, snap, "models_total", 4)
	assertMetricUsed(t, snap, "models_free", 1)
	assertMetricUsed(t, snap, "models_paid", 2)
	assertMetricUsed(t, snap, "models_unknown", 1)
	assertMetricUsed(t, snap, "endpoint_chat_models", 1)
	assertMetricUsed(t, snap, "endpoint_responses_models", 1)
	assertMetricUsed(t, snap, "endpoint_messages_models", 1)
	assertMetricUsed(t, snap, "free_probe_total_tokens", 15)
	assertMetricUsed(t, snap, "free_probe_cached_tokens", 4)
	assertMetricUsed(t, snap, "billing_payment_method_missing", 1)
}

func TestFetch_BillingActive(t *testing.T) {
	resetProbeCache()

	const apiKey = "test-zen-key-active"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"object":"list","data":[{"id":"glm-5-free","object":"model"},{"id":"gpt-5.1-codex-mini","object":"model"}]}`))
		case "/chat/completions":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id":"chat-2",
				"request_id":"req-chat-2",
				"model":"glm-5-free",
				"cost":"0",
				"usage":{
					"prompt_tokens":8,
					"completion_tokens":2,
					"total_tokens":10,
					"prompt_tokens_details":{"cached_tokens":1}
				}
			}`))
		case "/responses":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"id":"resp-1",
				"model":"gpt-5.1-codex-mini",
				"cost":"0.0015",
				"type":"response",
				"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_ZEN_KEY_ACTIVE", apiKey)
	defer os.Unsetenv("TEST_ZEN_KEY_ACTIVE")

	p := New()
	p.httpClient = server.Client()

	acct := core.AccountConfig{
		ID:        "zen-active",
		Provider:  "zen",
		APIKeyEnv: "TEST_ZEN_KEY_ACTIVE",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want %q (message=%s)", snap.Status, core.StatusOK, snap.Message)
	}
	if got := snap.Raw["subscription_status"]; got != "active" {
		t.Fatalf("subscription_status = %q, want active", got)
	}
	assertMetricUsed(t, snap, "subscription_active", 1)
	assertMetricUsed(t, snap, "billing_probe_total_tokens", 11)
	assertMetricUsed(t, snap, "billing_probe_cost_usd", 0.0015)
}

func assertMetricUsed(t *testing.T, snap core.QuotaSnapshot, key string, want float64) {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok {
		t.Fatalf("missing metric %q", key)
	}
	if m.Used == nil {
		t.Fatalf("metric %q has nil used", key)
	}
	if *m.Used != want {
		t.Fatalf("metric %q used = %v, want %v", key, *m.Used, want)
	}
}
