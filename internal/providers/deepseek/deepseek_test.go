package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

func TestFetch_BalanceAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/user/balance":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"is_available": true,
				"balance_infos": [{
					"currency": "CNY",
					"total_balance": "42.50",
					"granted_balance": "10.00",
					"topped_up_balance": "32.50"
				}]
			}`))
		case "/v1/models":
			w.Header().Set("x-ratelimit-limit-requests", "60")
			w.Header().Set("x-ratelimit-remaining-requests", "55")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_DEEPSEEK_KEY", "test-key")
	defer os.Unsetenv("TEST_DEEPSEEK_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-deepseek",
		Provider:  "deepseek",
		APIKeyEnv: "TEST_DEEPSEEK_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK; message = %s", snap.Status, snap.Message)
	}

	balance, ok := snap.Metrics["total_balance"]
	if !ok {
		t.Fatal("missing total_balance metric")
	}
	if balance.Remaining == nil || *balance.Remaining != 42.50 {
		t.Errorf("total_balance remaining = %v, want 42.50", balance.Remaining)
	}

	granted, ok := snap.Metrics["granted_balance"]
	if !ok {
		t.Fatal("missing granted_balance metric")
	}
	if granted.Remaining == nil || *granted.Remaining != 10.00 {
		t.Errorf("granted_balance = %v, want 10.00", granted.Remaining)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 60 {
		t.Errorf("rpm limit = %v, want 60", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 55 {
		t.Errorf("rpm remaining = %v, want 55", rpm.Remaining)
	}
}

func TestFetch_TokenFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user/balance":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"is_available": true, "balance_infos": [{"currency": "CNY", "total_balance": "10.00", "granted_balance": "0", "topped_up_balance": "10.00"}]}`))
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:      "test-token",
		Token:   "direct-token",
		BaseURL: server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}
}
