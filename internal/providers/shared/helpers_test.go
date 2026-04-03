package shared

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func float64Ptr(v float64) *float64 { return &v }

// ---------------------------------------------------------------------------
// CreateStandardRequest
// ---------------------------------------------------------------------------

func TestCreateStandardRequest_ContextPropagated(t *testing.T) {
	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("k"), "v")
	req, err := CreateStandardRequest(ctx, "https://api.example.com", "/v1/models", "sk-test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Context().Value(ctxKey("k")) != "v" {
		t.Error("context value not propagated to request")
	}
}

func TestCreateStandardRequest_HeadersSet(t *testing.T) {
	headers := map[string]string{
		"X-Custom":     "value1",
		"Content-Type": "application/json",
	}
	req, err := CreateStandardRequest(context.Background(), "https://api.example.com", "/v1/models", "sk-test", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, v := range headers {
		if got := req.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
}

func TestCreateStandardRequest_BearerAuthAdded(t *testing.T) {
	req, err := CreateStandardRequest(context.Background(), "https://api.example.com", "/v1/models", "sk-test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Bearer sk-test"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestCreateStandardRequest_AuthorizationNotOverwritten(t *testing.T) {
	customAuth := "X-Custom-Key my-key"
	headers := map[string]string{
		"Authorization": customAuth,
	}
	req, err := CreateStandardRequest(context.Background(), "https://api.example.com", "/v1/models", "sk-test", headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != customAuth {
		t.Errorf("Authorization = %q, want %q (should not be overwritten)", got, customAuth)
	}
}

func TestCreateStandardRequest_InvalidURL(t *testing.T) {
	_, err := CreateStandardRequest(context.Background(), "://bad-url", "/v1/models", "sk-test", nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// ProcessStandardResponse
// ---------------------------------------------------------------------------

func TestProcessStandardResponse_SnapshotFields(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
	}
	resp.Header.Set("X-Request-Id", "req-123")

	acct := core.AccountConfig{ID: "acct-1"}
	snap, err := ProcessStandardResponse(resp, acct, "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want %q", snap.ProviderID, "openai")
	}
	if snap.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, want %q", snap.AccountID, "acct-1")
	}
}

func TestProcessStandardResponse_HeadersRedacted(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
	}
	resp.Header.Set("Authorization", "Bearer sk-supersecret1234")
	resp.Header.Set("X-Request-Id", "req-123")

	acct := core.AccountConfig{ID: "acct-1"}
	snap, err := ProcessStandardResponse(resp, acct, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Authorization should be redacted (not the raw value)
	if raw, ok := snap.Raw["Authorization"]; ok {
		if raw == "Bearer sk-supersecret1234" {
			t.Error("Authorization header should be redacted in Raw")
		}
	}
	// Non-sensitive header should be present
	if snap.Raw["X-Request-Id"] != "req-123" {
		t.Errorf("X-Request-Id = %q, want %q", snap.Raw["X-Request-Id"], "req-123")
	}
}

func TestProcessStandardResponse_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantStatus core.Status
	}{
		{"200 OK", http.StatusOK, ""},
		{"401 Unauthorized", http.StatusUnauthorized, core.StatusAuth},
		{"403 Forbidden", http.StatusForbidden, core.StatusAuth},
		{"429 Too Many Requests", http.StatusTooManyRequests, core.StatusLimited},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{},
			}
			acct := core.AccountConfig{ID: "acct-1"}
			snap, err := ProcessStandardResponse(resp, acct, "test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if snap.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", snap.Status, tt.wantStatus)
			}
		})
	}
}

func TestProcessStandardResponse_429RetryAfter(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}
	resp.Header.Set("Retry-After", "60")

	acct := core.AccountConfig{ID: "acct-1"}
	snap, err := ProcessStandardResponse(resp, acct, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Raw["retry_after"] != "60" {
		t.Errorf("retry_after = %q, want %q", snap.Raw["retry_after"], "60")
	}
}

// ---------------------------------------------------------------------------
// ApplyStandardRateLimits
// ---------------------------------------------------------------------------

func TestApplyStandardRateLimits_RPMAndTPM(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
	}
	resp.Header.Set("x-ratelimit-limit-requests", "200")
	resp.Header.Set("x-ratelimit-remaining-requests", "150")
	resp.Header.Set("x-ratelimit-reset-requests", "30s")
	resp.Header.Set("x-ratelimit-limit-tokens", "40000")
	resp.Header.Set("x-ratelimit-remaining-tokens", "35000")
	resp.Header.Set("x-ratelimit-reset-tokens", "1m")

	snap := core.NewUsageSnapshot("test", "acct-1")
	ApplyStandardRateLimits(resp, &snap)

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 200 {
		t.Errorf("rpm.Limit = %v, want 200", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 150 {
		t.Errorf("rpm.Remaining = %v, want 150", rpm.Remaining)
	}
	if rpm.Unit != "requests" {
		t.Errorf("rpm.Unit = %q, want %q", rpm.Unit, "requests")
	}
	if rpm.Window != "1m" {
		t.Errorf("rpm.Window = %q, want %q", rpm.Window, "1m")
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 40000 {
		t.Errorf("tpm.Limit = %v, want 40000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 35000 {
		t.Errorf("tpm.Remaining = %v, want 35000", tpm.Remaining)
	}
	if tpm.Unit != "tokens" {
		t.Errorf("tpm.Unit = %q, want %q", tpm.Unit, "tokens")
	}

	if _, ok := snap.Resets["rpm_reset"]; !ok {
		t.Error("missing rpm_reset in Resets")
	}
	if _, ok := snap.Resets["tpm_reset"]; !ok {
		t.Error("missing tpm_reset in Resets")
	}
}

func TestApplyStandardRateLimits_MissingHeaders(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
	}
	snap := core.NewUsageSnapshot("test", "acct-1")
	ApplyStandardRateLimits(resp, &snap)

	if len(snap.Metrics) != 0 {
		t.Errorf("expected no metrics with missing headers, got %d", len(snap.Metrics))
	}
}

func TestApplyStandardRateLimits_PartialHeaders(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
	}
	resp.Header.Set("x-ratelimit-limit-requests", "100")
	// no remaining or reset for requests, no token headers at all

	snap := core.NewUsageSnapshot("test", "acct-1")
	ApplyStandardRateLimits(resp, &snap)

	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("expected rpm metric with at least limit header")
	}
	if rpm.Limit == nil || *rpm.Limit != 100 {
		t.Errorf("rpm.Limit = %v, want 100", rpm.Limit)
	}
	if rpm.Remaining != nil {
		t.Errorf("rpm.Remaining = %v, want nil", rpm.Remaining)
	}

	if _, ok := snap.Metrics["tpm"]; ok {
		t.Error("should not have tpm metric when no token headers present")
	}
}

// ---------------------------------------------------------------------------
// FinalizeStatus
// ---------------------------------------------------------------------------

func TestFinalizeStatus_EmptyStatus(t *testing.T) {
	snap := core.NewUsageSnapshot("test", "acct-1")
	FinalizeStatus(&snap)
	if snap.Status != core.StatusOK {
		t.Errorf("Status = %q, want %q", snap.Status, core.StatusOK)
	}
	if snap.Message != "OK" {
		t.Errorf("Message = %q, want %q", snap.Message, "OK")
	}
}

func TestFinalizeStatus_PreservedStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  core.Status
		message string
	}{
		{"auth", core.StatusAuth, "check API key"},
		{"limited", core.StatusLimited, "rate limited"},
		{"error", core.StatusError, "something broke"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("test", "acct-1")
			snap.Status = tt.status
			snap.Message = tt.message
			FinalizeStatus(&snap)
			if snap.Status != tt.status {
				t.Errorf("Status = %q, want %q", snap.Status, tt.status)
			}
			if snap.Message != tt.message {
				t.Errorf("Message = %q, want %q", snap.Message, tt.message)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RequireAPIKey
// ---------------------------------------------------------------------------

func TestRequireAPIKey_KeyPresent(t *testing.T) {
	os.Setenv("TEST_REQUIRE_KEY", "sk-present")
	defer os.Unsetenv("TEST_REQUIRE_KEY")

	acct := core.AccountConfig{
		ID:        "acct-1",
		APIKeyEnv: "TEST_REQUIRE_KEY",
	}
	key, authSnap := RequireAPIKey(acct, "openai")
	if key != "sk-present" {
		t.Errorf("key = %q, want %q", key, "sk-present")
	}
	if authSnap != nil {
		t.Error("expected nil snapshot when key is present")
	}
}

func TestRequireAPIKey_KeyMissing(t *testing.T) {
	os.Unsetenv("TEST_REQUIRE_KEY_MISSING")

	acct := core.AccountConfig{
		ID:        "acct-1",
		APIKeyEnv: "TEST_REQUIRE_KEY_MISSING",
	}
	key, authSnap := RequireAPIKey(acct, "openai")
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
	if authSnap == nil {
		t.Fatal("expected auth snapshot when key is missing")
	}
	if authSnap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want %q", authSnap.Status, core.StatusAuth)
	}
	if authSnap.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want %q", authSnap.ProviderID, "openai")
	}
	if authSnap.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, want %q", authSnap.AccountID, "acct-1")
	}
}

func TestRequireAPIKey_TokenTakesPrecedence(t *testing.T) {
	os.Setenv("TEST_REQUIRE_KEY_ENV", "env-key")
	defer os.Unsetenv("TEST_REQUIRE_KEY_ENV")

	acct := core.AccountConfig{
		ID:        "acct-1",
		APIKeyEnv: "TEST_REQUIRE_KEY_ENV",
		Token:     "runtime-token",
	}
	key, authSnap := RequireAPIKey(acct, "openai")
	if key != "runtime-token" {
		t.Errorf("key = %q, want %q (Token should take precedence)", key, "runtime-token")
	}
	if authSnap != nil {
		t.Error("expected nil snapshot when token is present")
	}
}

// ---------------------------------------------------------------------------
// ResolveBaseURL
// ---------------------------------------------------------------------------

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		name       string
		acctURL    string
		defaultURL string
		want       string
	}{
		{"account URL takes precedence", "https://custom.api.com", "https://default.api.com", "https://custom.api.com"},
		{"default used as fallback", "", "https://default.api.com", "https://default.api.com"},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := core.AccountConfig{BaseURL: tt.acctURL}
			got := ResolveBaseURL(acct, tt.defaultURL)
			if got != tt.want {
				t.Errorf("ResolveBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FetchJSON
// ---------------------------------------------------------------------------

func TestFetchJSON_SuccessfulDecode(t *testing.T) {
	type testPayload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test")
		}
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testPayload{Name: "test", Count: 42})
	}))
	defer server.Close()

	var out testPayload
	status, headers, err := FetchJSON(context.Background(), server.URL, "sk-test", &out, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if out.Name != "test" || out.Count != 42 {
		t.Errorf("decoded = %+v, want {Name:test Count:42}", out)
	}
	if headers.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("header X-Custom-Header = %q, want %q", headers.Get("X-Custom-Header"), "custom-value")
	}
}

func TestFetchJSON_NonOKStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"401 Unauthorized", http.StatusUnauthorized},
		{"403 Forbidden", http.StatusForbidden},
		{"429 Rate Limited", http.StatusTooManyRequests},
		{"500 Internal Error", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(`{"error": "test"}`))
			}))
			defer server.Close()

			var out map[string]string
			status, headers, err := FetchJSON(context.Background(), server.URL, "sk-test", &out, server.Client())
			if err == nil {
				t.Fatal("expected error for non-200 status")
			}
			if status != tt.statusCode {
				t.Errorf("status = %d, want %d", status, tt.statusCode)
			}
			if headers == nil {
				t.Error("expected headers even on error")
			}
		})
	}
}

func TestFetchJSON_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-valid-json`))
	}))
	defer server.Close()

	var out map[string]string
	_, _, err := FetchJSON(context.Background(), server.URL, "sk-test", &out, server.Client())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchJSON_NilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	var out map[string]bool
	status, _, err := FetchJSON(context.Background(), server.URL, "sk-test", &out, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if !out["ok"] {
		t.Error("expected ok=true in decoded output")
	}
}

func TestFetchJSON_NilOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": "ignored"}`))
	}))
	defer server.Close()

	status, _, err := FetchJSON(context.Background(), server.URL, "sk-test", nil, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// ProbeRateLimits
// ---------------------------------------------------------------------------

func TestProbeRateLimits_200WithHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-probe" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer sk-probe")
		}
		w.Header().Set("x-ratelimit-limit-requests", "500")
		w.Header().Set("x-ratelimit-remaining-requests", "499")
		w.Header().Set("x-ratelimit-limit-tokens", "100000")
		w.Header().Set("x-ratelimit-remaining-tokens", "99000")
		w.Header().Set("X-Request-Id", "req-abc")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": "model-1"}`))
	}))
	defer server.Close()

	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), server.URL, "sk-probe", &snap, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status should not be set (caller uses FinalizeStatus later)
	if snap.Status == core.StatusAuth || snap.Status == core.StatusLimited {
		t.Errorf("Status = %q, expected empty for 200", snap.Status)
	}

	// Raw should contain redacted headers
	if _, ok := snap.Raw["X-Request-Id"]; !ok {
		t.Error("expected X-Request-Id in Raw")
	}

	// Rate limits should be parsed
	rpm, ok := snap.Metrics["rpm"]
	if !ok {
		t.Fatal("missing rpm metric")
	}
	if rpm.Limit == nil || *rpm.Limit != 500 {
		t.Errorf("rpm.Limit = %v, want 500", rpm.Limit)
	}
	if rpm.Remaining == nil || *rpm.Remaining != 499 {
		t.Errorf("rpm.Remaining = %v, want 499", rpm.Remaining)
	}

	tpm, ok := snap.Metrics["tpm"]
	if !ok {
		t.Fatal("missing tpm metric")
	}
	if tpm.Limit == nil || *tpm.Limit != 100000 {
		t.Errorf("tpm.Limit = %v, want 100000", tpm.Limit)
	}
	if tpm.Remaining == nil || *tpm.Remaining != 99000 {
		t.Errorf("tpm.Remaining = %v, want 99000", tpm.Remaining)
	}
}

func TestProbeRateLimits_401AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid key"}`))
	}))
	defer server.Close()

	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), server.URL, "bad-key", &snap, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want %q", snap.Status, core.StatusAuth)
	}

	// Rate limits should NOT be parsed on auth error (early return)
	if len(snap.Metrics) != 0 {
		t.Errorf("expected no metrics on auth error, got %d", len(snap.Metrics))
	}
}

func TestProbeRateLimits_403Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": "forbidden"}`))
	}))
	defer server.Close()

	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), server.URL, "bad-key", &snap, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Errorf("Status = %q, want %q", snap.Status, core.StatusAuth)
	}
}

func TestProbeRateLimits_429RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "200")
		w.Header().Set("x-ratelimit-remaining-requests", "0")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), server.URL, "sk-test", &snap, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Errorf("Status = %q, want %q", snap.Status, core.StatusLimited)
	}
	if snap.Raw["retry_after"] != "30" {
		t.Errorf("retry_after = %q, want %q", snap.Raw["retry_after"], "30")
	}
	// Rate limits should still be parsed on 429 (not an auth error)
	if _, ok := snap.Metrics["rpm"]; !ok {
		t.Error("expected rpm metric to be parsed on 429")
	}
}

func TestProbeRateLimits_NilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-ratelimit-limit-requests", "100")
		w.Header().Set("x-ratelimit-remaining-requests", "99")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), server.URL, "sk-test", &snap, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := snap.Metrics["rpm"]; !ok {
		t.Error("expected rpm metric when using nil client")
	}
}

func TestProbeRateLimits_RequestError(t *testing.T) {
	snap := core.NewUsageSnapshot("test-provider", "acct-1")
	err := ProbeRateLimits(context.Background(), "http://127.0.0.1:0/unreachable", "sk-test", &snap, nil)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}
