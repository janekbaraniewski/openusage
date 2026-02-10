package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

func TestFetch_ParsesHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "150")
		w.Header().Set("x-ratelimit-reset-requests", "30s")
		w.Header().Set("x-ratelimit-limit-tokens", "40000")
		w.Header().Set("x-ratelimit-remaining-tokens", "35000")
		w.Header().Set("x-ratelimit-reset-tokens", "30s")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "gpt-4.1-mini"}`))
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:         "test-openai",
		Provider:   "openai",
		APIKeyEnv:  "TEST_OPENAI_KEY",
		ProbeModel: "gpt-4.1-mini",
		BaseURL:    server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 200 {
		t.Errorf("rpm limit = %v, want 200", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 150 {
		t.Errorf("rpm remaining = %v, want 150", rpm.Remaining)
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 40000 {
		t.Errorf("tpm limit = %v, want 40000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 35000 {
		t.Errorf("tpm remaining = %v, want 35000", tpm.Remaining)
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	os.Unsetenv("TEST_OPENAI_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_MISSING",
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
		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	os.Setenv("TEST_OPENAI_KEY", "test-key")
	defer os.Unsetenv("TEST_OPENAI_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-openai",
		Provider:  "openai",
		APIKeyEnv: "TEST_OPENAI_KEY",
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
