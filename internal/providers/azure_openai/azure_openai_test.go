package azure_openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_Success(t *testing.T) {
	var gotAPIKey, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("api-key")
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("x-ratelimit-limit-requests", "100")
		w.Header().Set("x-ratelimit-remaining-requests", "95")
		w.Header().Set("x-ratelimit-limit-tokens", "10000")
		w.Header().Set("x-ratelimit-remaining-tokens", "9000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	t.Setenv("TEST_AZURE_OPENAI_KEY", "test-key-value")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}
	if gotAPIKey != "test-key-value" {
		t.Errorf("api-key header = %q, want test-key-value", gotAPIKey)
	}
	if !strings.Contains(gotPath, "/openai/deployments") {
		t.Errorf("path = %q, want it to probe /openai/deployments", gotPath)
	}
	if !strings.Contains(gotPath, "api-version=") {
		t.Errorf("path = %q, want an api-version query", gotPath)
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
	t.Setenv("TEST_AZURE_OPENAI_MISSING", "")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_NoEndpoint(t *testing.T) {
	t.Setenv("TEST_AZURE_OPENAI_KEY", "test-key-value")
	t.Setenv(endpointEnv, "")
	t.Setenv(resourceNameEnv, "")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_KEY",
		// no BaseURL, no AZURE_OPENAI_ENDPOINT, no AZURE_RESOURCE_NAME
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED for missing endpoint", snap.Status)
	}
	if !strings.Contains(snap.Message, resourceNameEnv) {
		t.Errorf("Message = %q, want it to mention %s", snap.Message, resourceNameEnv)
	}
}

func TestResolveEndpoint(t *testing.T) {
	t.Setenv(endpointEnv, "")
	t.Setenv(resourceNameEnv, "")

	t.Run("base_url wins over both env vars", func(t *testing.T) {
		t.Setenv(endpointEnv, "https://from-endpoint-env.openai.azure.com")
		t.Setenv(resourceNameEnv, "from-resource-name")
		got := resolveEndpoint(core.AccountConfig{BaseURL: "https://from-base-url.openai.azure.com/"})
		if want := "https://from-base-url.openai.azure.com"; got != want {
			t.Errorf("resolveEndpoint() = %q, want %q (trailing slash trimmed)", got, want)
		}
	})

	t.Run("endpoint env wins over resource name", func(t *testing.T) {
		t.Setenv(endpointEnv, "https://from-endpoint-env.openai.azure.com")
		t.Setenv(resourceNameEnv, "from-resource-name")
		got := resolveEndpoint(core.AccountConfig{})
		if want := "https://from-endpoint-env.openai.azure.com"; got != want {
			t.Errorf("resolveEndpoint() = %q, want %q", got, want)
		}
	})

	t.Run("resource name builds standard endpoint", func(t *testing.T) {
		t.Setenv(endpointEnv, "")
		t.Setenv(resourceNameEnv, "my-resource")
		got := resolveEndpoint(core.AccountConfig{})
		if want := "https://my-resource.openai.azure.com"; got != want {
			t.Errorf("resolveEndpoint() = %q, want %q", got, want)
		}
	})

	t.Run("nothing configured yields empty", func(t *testing.T) {
		t.Setenv(endpointEnv, "")
		t.Setenv(resourceNameEnv, "")
		if got := resolveEndpoint(core.AccountConfig{}); got != "" {
			t.Errorf("resolveEndpoint() = %q, want empty", got)
		}
	})
}

func TestFetch_EndpointFromEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	t.Setenv("TEST_AZURE_OPENAI_KEY", "test-key-value")
	t.Setenv(endpointEnv, server.URL+"/")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_KEY",
		// no BaseURL — endpoint should resolve from env, trailing slash trimmed
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	t.Setenv("TEST_AZURE_OPENAI_KEY", "test-key-value")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_KEY",
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

func TestFetch_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	t.Setenv("TEST_AZURE_OPENAI_KEY", "bad-key")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-azure",
		Provider:  "azure_openai",
		APIKeyEnv: "TEST_AZURE_OPENAI_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}
