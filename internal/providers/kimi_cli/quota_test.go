package kimi_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const quotaFixture = `{
  "limits": [{"detail":{"limit":"100","used":"99","remaining":"1"}}],
  "usages": {
    "limit_5h": {"used_ratio":0.46,"reset_time":"2026-09-19T03:35:45Z"},
    "limit_month_total": {"used_ratio":0.2421,"reset_time":"2026-10-19T00:00:00Z"},
    "limit_month_code": {"used_ratio":0,"reset_time":"2026-10-19T00:00:00Z"}
  }
}`

func TestQuotaTimeoutRecoversWithSamePercentages(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "credentials.json")
	before := writeCredentials(t, path, "active", now.Add(time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected credential mutation request: %s", r.Method)
		}
		_, _ = w.Write([]byte(quotaFixture))
	}))
	defer srv.Close()
	p := New()
	p.clock = fixedClock{t: now}
	acct := quotaAccount("one", path, srv.URL)
	first, err := p.Fetch(context.Background(), acct)
	if err != nil || first.Attributes["quota_state"] != "fresh" || first.Attributes["quota_fetched_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("initial quota failed: %+v err=%v", first, err)
	}
	p.clock = fixedClock{t: now.Add(quotaCacheTTL + time.Second)}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	failed, err := p.Fetch(ctx, acct)
	if err != nil || failed.Attributes["quota_state"] != "unavailable" || failed.Diagnostics["quota_error"] == "" || failed.Metrics["usage_five_hour"].Used != nil {
		t.Fatalf("timeout state failed: %+v err=%v", failed, err)
	}
	p.clock = fixedClock{t: now.Add(2*quotaCacheTTL + 2*time.Second)}
	if changed, err := p.HasChanged(acct, time.Now()); err != nil || !changed {
		t.Fatal("timed-out quota was not retried after TTL")
	}
	recovered, err := p.Fetch(context.Background(), acct)
	if err != nil || recovered.Attributes["quota_state"] != "fresh" || len(recovered.Diagnostics) != 0 || recovered.Attributes["quota_fetched_at"] == first.Attributes["quota_fetched_at"] {
		t.Fatalf("recovery state failed: %+v err=%v", recovered, err)
	}
	if *recovered.Metrics["usage_five_hour"].Used != *first.Metrics["usage_five_hour"].Used {
		t.Fatal("fixture should recover with unchanged percentages")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("OpenUsage changed CLI credentials")
	}
}

func writeCredentials(t *testing.T, path, token string, expiresAt time.Time) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"access_token": token, "refresh_token": "cli-owned",
		"expires_at": expiresAt.Unix(), "scope": "kimi-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return body
}

func quotaAccount(id, path, baseURL string) core.AccountConfig {
	acct := core.AccountConfig{ID: id, Provider: ID, Auth: "local"}
	acct.SetPath(PathHintCredentialsPathKey, path)
	acct.SetPath(PathHintUsageAPIBaseKey, baseURL)
	return acct
}

func TestFetch_QuotaReadOnly(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/usages" ||
			r.Header.Get("Authorization") != "Bearer active-token" {
			t.Errorf("unexpected quota request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(quotaFixture))
	}))
	defer srv.Close()

	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	home := t.TempDir()
	setHome(t, home)
	path := filepath.Join(home, ".kimi-code", "credentials", "account.json")
	before := writeCredentials(t, path, "active-token", now.Add(time.Hour))
	p := New()
	p.clock = fixedClock{t: now}
	snap, err := p.Fetch(context.Background(), quotaAccount("one", path, srv.URL))
	if err != nil || snap.Status != core.StatusOK {
		t.Fatalf("quota fetch: status=%v err=%v", snap.Status, err)
	}
	if m := snap.Metrics["usage_five_hour"]; m.Used == nil || *m.Used != 46 {
		t.Errorf("5h quota = %+v, want 46%%", m)
	}
	if m := snap.Metrics["usage_monthly"]; m.Used == nil || *m.Used < 24.2 || *m.Used > 24.22 {
		t.Errorf("monthly quota = %+v", m)
	}
	if _, ok := snap.Metrics["rate_limit_primary"]; ok {
		t.Error("request-rate limit must not displace subscription quota")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("OpenUsage modified Kimi Code credentials")
	}
	if requests.Load() != 1 {
		t.Errorf("requests = %d, want one GET", requests.Load())
	}
}

func TestFetch_ExpiredTokenWaitsForCLI(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer new-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(quotaFixture))
	}))
	defer srv.Close()

	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "credentials.json")
	before := writeCredentials(t, path, "expired-token", now.Add(-time.Minute))
	p := New()
	p.clock = fixedClock{t: now}
	acct := quotaAccount("one", path, srv.URL)
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil || snap.Diagnostics["quota"] != "access token expired" || requests.Load() != 0 {
		t.Fatalf("expired token: status=%v diagnostics=%v requests=%d err=%v", snap.Status, snap.Diagnostics, requests.Load(), err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("expired credential file was changed")
	}

	oldMtime := credentialMtime(path)
	writeCredentials(t, path, "new-token", now.Add(time.Hour))
	newMtime := oldMtime.Add(time.Second)
	if err := os.Chtimes(path, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}
	changed, err := p.HasChanged(acct, now)
	if err != nil || !changed {
		t.Fatalf("CLI credential rotation should trigger a fetch: changed=%v err=%v", changed, err)
	}
	snap, err = p.Fetch(context.Background(), acct)
	if err != nil || snap.Metrics["usage_five_hour"].Used == nil || requests.Load() != 1 {
		t.Fatalf("CLI-rotated token not used: metrics=%v requests=%d err=%v", snap.Metrics, requests.Load(), err)
	}
}

func TestFetch_RereadsCLIChangeAfterUnauthorized(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentials(t, path, "old-token", now.Add(time.Hour))
	rotated := []byte(fmt.Sprintf(`{"access_token":"new-token","refresh_token":"cli-owned","expires_at":%d}`, now.Add(time.Hour).Unix()))
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		if r.Header.Get("Authorization") == "Bearer old-token" {
			if err := os.WriteFile(path, rotated, 0o600); err != nil {
				t.Errorf("CLI rotation fixture: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(quotaFixture))
	}))
	defer srv.Close()

	p := New()
	p.clock = fixedClock{t: now}
	snap, err := p.Fetch(context.Background(), quotaAccount("one", path, srv.URL))
	if err != nil || snap.Metrics["usage_five_hour"].Used == nil || requests.Load() != 2 {
		t.Fatalf("retry after CLI rotation: metrics=%v requests=%d err=%v", snap.Metrics, requests.Load(), err)
	}
	creds, err := readCredentials(path)
	if err != nil || creds.AccessToken != "new-token" {
		t.Fatal("OpenUsage replaced the CLI's new credential")
	}
}

func TestFetch_QuotaCacheIsAccountScoped(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		ratio := 0.2
		if r.Header.Get("Authorization") == "Bearer second" {
			ratio = 0.8
		}
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":%g}}}`, ratio)
	}))
	defer srv.Close()

	path1 := filepath.Join(t.TempDir(), "first.json")
	path2 := filepath.Join(t.TempDir(), "second.json")
	writeCredentials(t, path1, "first", now.Add(time.Hour))
	writeCredentials(t, path2, "second", now.Add(time.Hour))
	accounts := []core.AccountConfig{
		quotaAccount("first", path1, srv.URL), quotaAccount("second", path2, srv.URL),
	}
	p := New()
	p.clock = fixedClock{t: now}
	var wg sync.WaitGroup
	for i, acct := range accounts {
		wg.Add(1)
		go func(i int, acct core.AccountConfig) {
			defer wg.Done()
			snap, err := p.Fetch(context.Background(), acct)
			want := float64((i*6 + 2) * 10)
			if err != nil || snap.Metrics["usage_five_hour"].Used == nil ||
				*snap.Metrics["usage_five_hour"].Used != want {
				t.Errorf("account %s quota = %+v, want %.0f; err=%v", acct.ID, snap.Metrics["usage_five_hour"], want, err)
			}
		}(i, acct)
	}
	wg.Wait()
	for _, acct := range accounts {
		if _, err := p.Fetch(context.Background(), acct); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Errorf("requests = %d, want one per account within TTL", requests.Load())
	}
}

func TestHasChanged_QuotaExpiresWithoutLocalChanges(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	var ratio atomic.Int32
	ratio.Store(20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"usages":{"limit_5h":{"used_ratio":%g}}}`, float64(ratio.Load())/100)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentials(t, path, "active", now.Add(time.Hour))
	acct := quotaAccount("one", path, srv.URL)
	acct.SetPath(PathHintSessionsDirKey, t.TempDir())
	p := New()
	p.clock = fixedClock{t: now}
	if _, err := p.Fetch(context.Background(), acct); err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	if changed, _ := p.HasChanged(acct, since); changed {
		t.Fatal("unchanged local data should use the quota cache")
	}
	ratio.Store(80)
	p.clock = fixedClock{t: now.Add(quotaCacheTTL + time.Second)}
	if changed, _ := p.HasChanged(acct, since); !changed {
		t.Fatal("expired quota cache should bypass local change detection")
	}
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil || snap.Metrics["usage_five_hour"].Used == nil || *snap.Metrics["usage_five_hour"].Used != 80 {
		t.Fatalf("stale quota after TTL: metric=%+v err=%v", snap.Metrics["usage_five_hour"], err)
	}
}

func TestFetch_QuotaErrorKeepsLocalStats(t *testing.T) {
	now := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	setHome(t, t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentials(t, path, "active", now.Add(time.Hour))
	dir := t.TempDir()
	wire := filepath.Join(dir, "group", "session", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(wire), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wire, []byte(`{"timestamp":1735689600,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":10}}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	acct := quotaAccount("one", path, srv.URL)
	acct.SetPath(PathHintSessionsDirKey, dir)
	p := New()
	p.clock = fixedClock{t: now}
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil || snap.Status != core.StatusOK || snap.Diagnostics["quota_error"] == "" ||
		snap.Metrics["total_input_tokens"].Used == nil {
		t.Fatalf("local stats should survive quota HTTP error: status=%v metrics=%v diagnostics=%v err=%v", snap.Status, snap.Metrics, snap.Diagnostics, err)
	}
}

func TestApplyQuota_ResetInPastZeroesUtilization(t *testing.T) {
	var response kimiUsagesResponse
	if err := json.Unmarshal([]byte(`{"usages":{"limit_5h":{"used_ratio":0.9,"reset_time":"2020-01-01T00:00:00Z"}}}`), &response); err != nil {
		t.Fatal(err)
	}
	snap := core.NewUsageSnapshot(ID, "one")
	applyQuotaToSnapshot(&snap, &response, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	if m := snap.Metrics["usage_five_hour"]; m.Used == nil || *m.Used != 0 {
		t.Errorf("expired quota = %+v, want 0%%", m)
	}
}

func TestResolveCredentialsPath_OnlyKimiCode(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	old := filepath.Join(home, ".kimi", "credentials", "old.json")
	writeCredentials(t, old, "old", time.Now().Add(time.Hour))
	if got := resolveCredentialsPath(core.AccountConfig{}); got != "" {
		t.Errorf("legacy CLI credentials selected: %q", got)
	}
	code := filepath.Join(home, ".kimi-code", "credentials", "code.json")
	writeCredentials(t, code, "code", time.Now().Add(time.Hour))
	if got := resolveCredentialsPath(core.AccountConfig{}); got != code {
		t.Errorf("code credentials = %q, want %q", got, code)
	}
	acct := core.AccountConfig{}
	acct.SetPath(PathHintCredentialsPathKey, filepath.Join(home, "missing.json"))
	if got := resolveCredentialsPath(acct); got != filepath.Join(home, "missing.json") {
		t.Errorf("missing override silently fell back: %q", got)
	}
}
