package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func zenModelsBody() string {
	return `{
		"object": "list",
		"data": [
			{"id": "minimax-m2.7", "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "kimi-k2.6",   "object": "model", "created": 1, "owned_by": "opencode"},
			{"id": "glm-5",       "object": "model", "created": 1, "owned_by": "opencode"}
		]
	}`
}

func startFakeZen(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	if status == 0 {
		status = http.StatusOK
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsPath {
			http.NotFound(w, r)
			return
		}
		// Verify the request carries Bearer auth — the provider would lose its
		// reason for existing if it forgot to attach it.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing bearer", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func newAcct(t *testing.T, baseURL string) core.AccountConfig {
	t.Helper()
	t.Setenv("TEST_OPENCODE_KEY", "sk-zen-test-1234567890")
	return core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_KEY",
		BaseURL:   baseURL,
	}
}

func TestFetch_Success_AuthOKExposesModels(t *testing.T) {
	server := startFakeZen(t, http.StatusOK, zenModelsBody())
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %s (msg=%q), want OK", snap.Status, snap.Message)
	}
	if got := snap.Attributes["available_models_count"]; got != "3" {
		t.Errorf("available_models_count = %q, want 3", got)
	}
	if got := snap.Attributes["available_models"]; !strings.Contains(got, "minimax-m2.7") || !strings.Contains(got, "glm-5") {
		t.Errorf("available_models missing expected ids: %q", got)
	}
	if got := snap.Attributes["auth_scope"]; got != "zen" {
		t.Errorf("auth_scope = %q, want zen", got)
	}
	if !strings.Contains(snap.Message, "3") {
		t.Errorf("message should reference the model count: %q", snap.Message)
	}
}

func TestFetch_AuthRequired_NoKey(t *testing.T) {
	acct := core.AccountConfig{
		ID:        "opencode",
		Provider:  "opencode",
		APIKeyEnv: "TEST_OPENCODE_MISSING",
	}
	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthFailed_401(t *testing.T) {
	server := startFakeZen(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("status = %s, want AUTH on 401", snap.Status)
	}
	if !strings.Contains(snap.Message, "401") {
		t.Errorf("message = %q, expected to mention 401", snap.Message)
	}
}

func TestFetch_RateLimited_429(t *testing.T) {
	server := startFakeZen(t, http.StatusTooManyRequests, `{}`)
	defer server.Close()

	snap, err := New().Fetch(context.Background(), newAcct(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("status = %s, want LIMITED on 429", snap.Status)
	}
}
