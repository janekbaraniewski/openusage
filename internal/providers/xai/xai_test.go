package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api-key":
			resp := apiKeyResponse{
				Name:             "my-test-key",
				APIKeyID:         "key-123",
				TeamID:           "team-456",
				RemainingBalance: float64Ptr(42.50),
				SpentBalance:     float64Ptr(7.50),
				TotalGranted:     float64Ptr(50.00),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case "/models":
			w.Header().Set("x-ratelimit-limit-requests", "60")
			w.Header().Set("x-ratelimit-remaining-requests", "55")
			w.Header().Set("x-ratelimit-reset-requests", "30s")
			w.Header().Set("x-ratelimit-limit-tokens", "100000")
			w.Header().Set("x-ratelimit-remaining-tokens", "99000")
			w.Header().Set("x-ratelimit-reset-tokens", "30s")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_XAI_KEY", "test-key")
	defer os.Unsetenv("TEST_XAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-xai",
		Provider:  "xai",
		APIKeyEnv: "TEST_XAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// Verify credits metric from /api-key response.
	credits, ok := snap.Metrics["credits"]
	if !ok {
		t.Fatal("missing credits metric")
	}
	if credits.Remaining == nil || *credits.Remaining != 42.50 {
		t.Errorf("credits remaining = %v, want 42.50", credits.Remaining)
	}
	if credits.Used == nil || *credits.Used != 7.50 {
		t.Errorf("credits used = %v, want 7.50", credits.Used)
	}
	if credits.Limit == nil || *credits.Limit != 50.00 {
		t.Errorf("credits limit = %v, want 50.00", credits.Limit)
	}
	if credits.Unit != "USD" {
		t.Errorf("credits unit = %q, want USD", credits.Unit)
	}

	// Verify raw metadata from /api-key response.
	if v := snap.Raw["api_key_name"]; v != "my-test-key" {
		t.Errorf("raw api_key_name = %q, want %q", v, "my-test-key")
	}
	if v := snap.Raw["team_id"]; v != "team-456" {
		t.Errorf("raw team_id = %q, want %q", v, "team-456")
	}

	// Verify rate limit metrics from /models response.
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

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 100000 {
		t.Errorf("tpm limit = %v, want 100000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 99000 {
		t.Errorf("tpm remaining = %v, want 99000", tpm.Remaining)
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	os.Unsetenv("TEST_XAI_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-xai",
		Provider:  "xai",
		APIKeyEnv: "TEST_XAI_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_APIKeyInfoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api-key":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal server error"}`))
		case "/models":
			w.Header().Set("x-ratelimit-limit-requests", "60")
			w.Header().Set("x-ratelimit-remaining-requests", "55")
			w.Header().Set("x-ratelimit-limit-tokens", "100000")
			w.Header().Set("x-ratelimit-remaining-tokens", "99000")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": []}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	os.Setenv("TEST_XAI_KEY", "test-key")
	defer os.Unsetenv("TEST_XAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-xai",
		Provider:  "xai",
		APIKeyEnv: "TEST_XAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	// api-key failed, so credits metric should be absent.
	if _, ok := snap.Metrics["credits"]; ok {
		t.Error("expected no credits metric when /api-key returns 500")
	}

	// Error should be recorded in raw.
	if v := snap.Raw["api_key_info_error"]; v == "" {
		t.Error("expected api_key_info_error in raw")
	}

	// Rate limits from /models should still be present.
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric (should still be present from /models)")
	}
	if rpm.Limit == nil || *rpm.Limit != 60 {
		t.Errorf("rpm limit = %v, want 60", rpm.Limit)
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric (should still be present from /models)")
	}
	if tpm.Limit == nil || *tpm.Limit != 100000 {
		t.Errorf("tpm limit = %v, want 100000", tpm.Limit)
	}

	// Status should be OK from rate limits (FinalizeStatus sets it).
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK (rate limits succeeded)", snap.Status)
	}
}

func float64Ptr(f float64) *float64 { return &f }
