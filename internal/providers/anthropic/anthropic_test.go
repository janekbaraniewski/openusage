package anthropic

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
		w.Header().Set("anthropic-ratelimit-requests-limit", "1000")
		w.Header().Set("anthropic-ratelimit-requests-remaining", "900")
		w.Header().Set("anthropic-ratelimit-requests-reset", "2025-06-01T00:00:00Z")
		w.Header().Set("anthropic-ratelimit-tokens-limit", "100000")
		w.Header().Set("anthropic-ratelimit-tokens-remaining", "80000")
		w.Header().Set("anthropic-ratelimit-tokens-reset", "2025-06-01T00:00:00Z")
		w.WriteHeader(http.StatusBadRequest) // missing body is expected
		w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	os.Setenv("TEST_ANTHROPIC_KEY", "test-key")
	defer os.Unsetenv("TEST_ANTHROPIC_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-anthropic",
		Provider:  "anthropic",
		APIKeyEnv: "TEST_ANTHROPIC_KEY",
		BaseURL:   server.URL,
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
	if rpm.Limit == nil || *rpm.Limit != 1000 {
		t.Errorf("rpm limit = %v, want 1000", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 900 {
		t.Errorf("rpm remaining = %v, want 900", rpm.Remaining)
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 100000 {
		t.Errorf("tpm limit = %v, want 100000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 80000 {
		t.Errorf("tpm remaining = %v, want 80000", tpm.Remaining)
	}
}

func TestFetch_AuthRequired(t *testing.T) {
	os.Unsetenv("TEST_ANTHROPIC_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-anthropic",
		Provider:  "anthropic",
		APIKeyEnv: "TEST_ANTHROPIC_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}
