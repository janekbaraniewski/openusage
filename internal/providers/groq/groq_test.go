package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Standard per-minute rate limit headers
		w.Header().Set("x-ratelimit-limit-requests", "30")
		w.Header().Set("x-ratelimit-remaining-requests", "25")
		w.Header().Set("x-ratelimit-reset-requests", "30s")
		w.Header().Set("x-ratelimit-limit-tokens", "15000")
		w.Header().Set("x-ratelimit-remaining-tokens", "14000")
		w.Header().Set("x-ratelimit-reset-tokens", "30s")

		// Daily rate limit headers (Groq-specific)
		w.Header().Set("x-ratelimit-limit-requests-day", "14400")
		w.Header().Set("x-ratelimit-remaining-requests-day", "14000")
		w.Header().Set("x-ratelimit-reset-requests-day", "1h30m")
		w.Header().Set("x-ratelimit-limit-tokens-day", "500000")
		w.Header().Set("x-ratelimit-remaining-tokens-day", "480000")
		w.Header().Set("x-ratelimit-reset-tokens-day", "1h30m")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": [{"id": "llama3-70b"}]}`))
	}))
	defer server.Close()

	os.Setenv("TEST_GROQ_KEY", "test-key")
	defer os.Unsetenv("TEST_GROQ_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-groq",
		Provider:  "groq",
		APIKeyEnv: "TEST_GROQ_KEY",
		BaseURL:   server.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("Status = %v, want OK", snap.Status)
	}

	// Verify per-minute request metrics (rpm)
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 30 {
		t.Errorf("rpm limit = %v, want 30", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 25 {
		t.Errorf("rpm remaining = %v, want 25", rpm.Remaining)
	}
	if rpm.Unit != "requests" {
		t.Errorf("rpm unit = %q, want %q", rpm.Unit, "requests")
	}
	if rpm.Window != "1m" {
		t.Errorf("rpm window = %q, want %q", rpm.Window, "1m")
	}

	// Verify per-minute token metrics (tpm)
	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 15000 {
		t.Errorf("tpm limit = %v, want 15000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 14000 {
		t.Errorf("tpm remaining = %v, want 14000", tpm.Remaining)
	}
	if tpm.Unit != "tokens" {
		t.Errorf("tpm unit = %q, want %q", tpm.Unit, "tokens")
	}

	// Verify daily request metrics (rpd)
	rpd, ok := snap.Metrics["rpd"]
	if !ok {
		t.Fatal("missing rpd metric")
	}
	if rpd.Limit == nil || *rpd.Limit != 14400 {
		t.Errorf("rpd limit = %v, want 14400", rpd.Limit)
	}
	if rpd.Remaining == nil || *rpd.Remaining != 14000 {
		t.Errorf("rpd remaining = %v, want 14000", rpd.Remaining)
	}
	if rpd.Unit != "requests" {
		t.Errorf("rpd unit = %q, want %q", rpd.Unit, "requests")
	}
	if rpd.Window != "1d" {
		t.Errorf("rpd window = %q, want %q", rpd.Window, "1d")
	}

	// Verify daily token metrics (tpd)
	tpd, ok := snap.Metrics["tpd"]
	if !ok {
		t.Fatal("missing tpd metric")
	}
	if tpd.Limit == nil || *tpd.Limit != 500000 {
		t.Errorf("tpd limit = %v, want 500000", tpd.Limit)
	}
	if tpd.Remaining == nil || *tpd.Remaining != 480000 {
		t.Errorf("tpd remaining = %v, want 480000", tpd.Remaining)
	}
	if tpd.Unit != "tokens" {
		t.Errorf("tpd unit = %q, want %q", tpd.Unit, "tokens")
	}
	if tpd.Window != "1d" {
		t.Errorf("tpd window = %q, want %q", tpd.Window, "1d")
	}
}

func TestFetch_AuthRequired_MissingKey(t *testing.T) {
	os.Unsetenv("TEST_GROQ_MISSING")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-groq",
		Provider:  "groq",
		APIKeyEnv: "TEST_GROQ_MISSING",
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_AuthRequired_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API Key","type":"invalid_api_key"}}`))
	}))
	defer server.Close()

	os.Setenv("TEST_GROQ_KEY", "invalid-key")
	defer os.Unsetenv("TEST_GROQ_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-groq",
		Provider:  "groq",
		APIKeyEnv: "TEST_GROQ_KEY",
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

func TestFetch_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "30")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit reached"}}`))
	}))
	defer server.Close()

	os.Setenv("TEST_GROQ_KEY", "test-key")
	defer os.Unsetenv("TEST_GROQ_KEY")

	p := New()
	acct := core.AccountConfig{
		ID:        "test-groq",
		Provider:  "groq",
		APIKeyEnv: "TEST_GROQ_KEY",
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

func TestBuildStatusMessage(t *testing.T) {
	tests := []struct {
		name string
		snap core.UsageSnapshot
		want string
	}{
		{
			name: "both rpm and rpd",
			snap: func() core.UsageSnapshot {
				s := core.NewUsageSnapshot("groq", "test")
				lRPM, rRPM := 30.0, 25.0
				s.Metrics["rpm"] = core.Metric{Limit: &lRPM, Remaining: &rRPM, Unit: "requests", Window: "1m"}
				lRPD, rRPD := 14400.0, 14000.0
				s.Metrics["rpd"] = core.Metric{Limit: &lRPD, Remaining: &rRPD, Unit: "requests", Window: "1d"}
				return s
			}(),
			want: "Remaining: 25/30 RPM, 14000/14400 RPD",
		},
		{
			name: "rpm only",
			snap: func() core.UsageSnapshot {
				s := core.NewUsageSnapshot("groq", "test")
				lRPM, rRPM := 30.0, 25.0
				s.Metrics["rpm"] = core.Metric{Limit: &lRPM, Remaining: &rRPM, Unit: "requests", Window: "1m"}
				return s
			}(),
			want: "Remaining: 25/30 RPM",
		},
		{
			name: "rpd only",
			snap: func() core.UsageSnapshot {
				s := core.NewUsageSnapshot("groq", "test")
				lRPD, rRPD := 14400.0, 14000.0
				s.Metrics["rpd"] = core.Metric{Limit: &lRPD, Remaining: &rRPD, Unit: "requests", Window: "1d"}
				return s
			}(),
			want: "Remaining: 14000/14400 RPD",
		},
		{
			name: "no metrics",
			snap: core.NewUsageSnapshot("groq", "test"),
			want: "OK",
		},
		{
			name: "metrics missing limit",
			snap: func() core.UsageSnapshot {
				s := core.NewUsageSnapshot("groq", "test")
				rRPM := 25.0
				s.Metrics["rpm"] = core.Metric{Remaining: &rRPM, Unit: "requests", Window: "1m"}
				return s
			}(),
			want: "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStatusMessage(tt.snap)
			if got != tt.want {
				t.Errorf("buildStatusMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
