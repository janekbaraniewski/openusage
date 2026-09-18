package kimi_cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const quotaFixture = `{
  "limits": [
    {"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
     "detail": {"limit": "100", "used": "46", "remaining": "54", "resetTime": "2026-09-19T03:35:46Z"}}
  ],
  "usages": {
    "limit_5h": {"used_ratio": 0.46, "reset_time": "2026-09-19T03:35:45Z"},
    "limit_month_total": {"used_ratio": 0.2421, "reset_time": "2026-10-19T00:00:00Z"},
    "limit_month_code": {"used_ratio": 0, "reset_time": "2026-10-19T00:00:00Z"}
  }
}`

func writeCredentials(t *testing.T, dir string, expiresAt int64) string {
	t.Helper()
	path := filepath.Join(dir, "kimi-code-env-test.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir credentials: %v", err)
	}
	body := map[string]any{
		"access_token":  "test-access-token",
		"refresh_token": "test-refresh-token",
		"expires_at":    expiresAt,
		"token_type":    "Bearer",
		"scope":         "kimi-code",
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return path
}

func quotaAccount(credPath, baseURL, oauthHost string) core.AccountConfig {
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath(PathHintCredentialsPathKey, credPath)
	acct.SetPath(PathHintUsageAPIBaseKey, baseURL)
	acct.SetPath(PathHintOAuthHostKey, oauthHost)
	return acct
}

func TestFetch_QuotaHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(quotaFixture))
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	credPath := writeCredentials(t, home, time.Now().Add(time.Hour).Unix())

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	acct := quotaAccount(credPath, srv.URL, srv.URL)
	acct.SetPath(PathHintSessionsDirKey, filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, want OK (quota without sessions)", snap.Status)
	}

	fiveHour := snap.Metrics["usage_five_hour"]
	if fiveHour.Used == nil || *fiveHour.Used != 46 {
		t.Errorf("usage_five_hour used = %v, want 46", fiveHour.Used)
	}
	if fiveHour.Unit != "%" || fiveHour.Window != "5h" {
		t.Errorf("usage_five_hour unit/window = %q/%q", fiveHour.Unit, fiveHour.Window)
	}
	if fiveHour.Limit == nil || *fiveHour.Limit != 100 {
		t.Errorf("usage_five_hour limit = %v, want 100", fiveHour.Limit)
	}
	if _, ok := snap.Resets["usage_five_hour"]; !ok {
		t.Error("missing usage_five_hour reset")
	}

	monthly := snap.Metrics["usage_monthly"]
	if monthly.Used == nil || *monthly.Used < 24.2 || *monthly.Used > 24.22 {
		t.Errorf("usage_monthly used = %v, want ~24.21", monthly.Used)
	}
	if monthly.Window != "30d" {
		t.Errorf("usage_monthly window = %q, want 30d", monthly.Window)
	}

	rate := snap.Metrics["rate_limit_primary"]
	if rate.Used == nil || *rate.Used != 46 || rate.Limit == nil || *rate.Limit != 100 {
		t.Errorf("rate_limit_primary = used %v limit %v, want 46/100", rate.Used, rate.Limit)
	}
	if rate.Window != "5h" {
		t.Errorf("rate_limit_primary window = %q, want 5h", rate.Window)
	}
}

func TestFetch_QuotaRefreshesExpiredToken(t *testing.T) {
	var sawRefresh bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") == "" || r.Form.Get("refresh_token") != "test-refresh-token" {
				t.Errorf("unexpected refresh form: %v", r.Form)
			}
			sawRefresh = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":3600,"token_type":"Bearer"}`))
		case "/usages":
			if r.Header.Get("Authorization") != "Bearer rotated-access" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(quotaFixture))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	credPath := writeCredentials(t, home, time.Now().Add(-time.Hour).Unix())

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	acct := quotaAccount(credPath, srv.URL, srv.URL)
	acct.SetPath(PathHintSessionsDirKey, filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sawRefresh {
		t.Fatal("expected token refresh for expired credentials")
	}
	if _, ok := snap.Metrics["usage_five_hour"]; !ok {
		t.Fatal("usage_five_hour missing after refreshed fetch")
	}

	// The rotated tokens must be persisted back, or the single-use refresh
	// token would be lost for the CLI itself.
	data, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read back credentials: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("credentials after refresh not JSON: %v", err)
	}
	if back["access_token"] != "rotated-access" || back["refresh_token"] != "rotated-refresh" {
		t.Error("rotated tokens were not persisted to the credentials file")
	}
	if back["scope"] != "kimi-code" {
		t.Error("refresh write-back dropped unrelated credentials fields")
	}
}

func TestFetch_QuotaHTTPErrorDegradesToDiagnostic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	credPath := writeCredentials(t, home, time.Now().Add(time.Hour).Unix())

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	acct := quotaAccount(credPath, srv.URL, srv.URL)
	acct.SetPath(PathHintSessionsDirKey, filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := snap.Metrics["usage_five_hour"]; ok {
		t.Error("usage_five_hour must not be set on HTTP error")
	}
	if snap.Diagnostics["quota_error"] == "" {
		t.Error("expected quota_error diagnostic")
	}
}

func TestFetch_QuotaNoCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath(PathHintSessionsDirKey, filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Diagnostics["quota"] == "" {
		t.Error("expected quota diagnostic for missing credentials")
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want UNKNOWN without sessions and quota", snap.Status)
	}
}

func TestApplyQuota_ResetInPastZeroesUtilization(t *testing.T) {
	var resp kimiUsagesResponse
	if err := json.Unmarshal([]byte(`{"usages":{"limit_5h":{"used_ratio":0.9,"reset_time":"2020-01-01T00:00:00Z"}}}`), &resp); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	snap := core.NewUsageSnapshot(ID, "kimi_cli")
	applyQuotaToSnapshot(&snap, &resp, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	m := snap.Metrics["usage_five_hour"]
	if m.Used == nil || *m.Used != 0 {
		t.Errorf("expired window should report 0%%, got %v", m.Used)
	}
}

func TestResolveCredentialsPath_PicksNewestAcrossDataDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	old := writeCredentials(t, filepath.Join(home, ".kimi", "credentials"), 0)
	time.Sleep(10 * time.Millisecond)
	newer := writeCredentials(t, filepath.Join(home, ".kimi-code", "credentials"), 0)
	// Guarantee ordering even on coarse-mtime filesystems.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli"}
	if got := resolveCredentialsPath(acct); got != newer {
		t.Errorf("resolveCredentialsPath = %q, want newest %q", got, newer)
	}
	if !strings.HasSuffix(newer, ".kimi-code/credentials/kimi-code-env-test.json") {
		t.Errorf("unexpected path shape: %q", newer)
	}
}
