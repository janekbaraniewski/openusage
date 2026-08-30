package antigravity

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestFetchQuotaFromAPI(t *testing.T) {
	quotaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:retrieveUserQuotaSummary" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("User-Agent"); got != "antigravity" {
			t.Errorf("User-Agent = %q, want antigravity", got)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{
			"groups": [
				{
					"displayName": "Gemini Models",
					"buckets": [
						{"bucketId":"gemini-weekly","remainingFraction":0.3787134,"resetTime":"2030-09-02T03:31:19Z","window":"weekly"},
						{"bucketId":"gemini-5h","remainingFraction":0,"resetTime":"2030-08-29T16:49:43Z","window":"5h"}
					]
				},
				{
					"displayName": "Claude and GPT models",
					"buckets": [
						{"bucketId":"3p-weekly","remainingFraction":0,"resetTime":"2030-09-02T23:42:57Z","window":"weekly"},
						{"bucketId":"3p-5h","remainingFraction":0.6616888,"resetTime":"2030-08-29T19:39:50Z","window":"5h"}
					]
				}
			]
		}`))
	}))
	defer quotaServer.Close()

	configDir := t.TempDir()
	writeTestToken(t, filepath.Join(configDir, oauthTokenFile), "access-token", "2030-01-01T00:00:00Z", "refresh-token")

	p := New()
	p.HTTPClient = quotaServer.Client()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity-chaos",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"config_dir": configDir,
		},
		RuntimeHints: map[string]string{
			"box_name":       "chaos",
			"quota_endpoint": quotaServer.URL + "/v1internal",
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %q, want %q", snap.Status, core.StatusLimited)
	}
	if got := *snap.Metrics["quota_gemini_5h"].Remaining; got != 0 {
		t.Errorf("quota_gemini_5h = %v, want 0", got)
	}
	if got := *snap.Metrics["quota_gemini_weekly"].Remaining; math.Abs(got-37.87134) > 0.001 {
		t.Errorf("quota_gemini_weekly = %v, want ~37.87134", got)
	}
	if got := *snap.Metrics["quota_claude_5h"].Remaining; math.Abs(got-66.16888) > 0.001 {
		t.Errorf("quota_claude_5h = %v, want ~66.16888", got)
	}
	if snap.Raw["quota_source"] != "retrieveUserQuotaSummary" {
		t.Errorf("quota_source = %q", snap.Raw["quota_source"])
	}
	if snap.Attributes["box"] != "chaos" {
		t.Errorf("box = %q, want chaos", snap.Attributes["box"])
	}
}

func TestFetchMissingTokenIsAuth(t *testing.T) {
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := p.Fetch(ctx, core.AccountConfig{
		ID:       "antigravity",
		Provider: "antigravity",
		Binary:   filepath.Join(t.TempDir(), "no-such-agy"),
		ProviderPaths: map[string]string{
			"config_dir": t.TempDir(),
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %q, want %q", snap.Status, core.StatusAuth)
	}
}

func TestFetchUsesValidAccessToken(t *testing.T) {
	quotaHits := 0
	quotaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quotaHits++
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{
			"groups":[{"buckets":[
				{"bucketId":"gemini-5h","remainingFraction":0.5,"resetTime":"2030-01-01T00:00:00Z"},
				{"bucketId":"gemini-weekly","remainingFraction":0.9,"resetTime":"2030-01-08T00:00:00Z"}
			]}]
		}`))
	}))
	defer quotaServer.Close()

	configDir := t.TempDir()
	writeTestToken(t, filepath.Join(configDir, oauthTokenFile), "new-access", time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), "refresh-token")

	p := New()
	p.HTTPClient = quotaServer.Client()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity-chaos",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"config_dir": configDir,
		},
		RuntimeHints: map[string]string{
			"quota_endpoint": quotaServer.URL + "/v1internal",
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want ok; msg=%s diag=%v", snap.Status, snap.Message, snap.Diagnostics)
	}
	if quotaHits != 1 {
		t.Fatalf("quota hits = %d, want 1", quotaHits)
	}
	if got := *snap.Metrics["quota_gemini_5h"].Remaining; got != 50 {
		t.Errorf("quota_gemini_5h = %v, want 50", got)
	}
}

func TestProjectQuotaMetrics_AdvancesPastReset(t *testing.T) {
	past := time.Now().UTC().Add(-10 * time.Minute)
	payload := statusLinePayload{
		ReceivedAt: past.Add(-5 * time.Minute),
		Quota: map[string]statusLineQuota{
			"gemini-weekly": {
				RemainingFraction: core.Float64Ptr(0),
				ResetTime:         past.Format(time.RFC3339Nano),
			},
		},
	}
	var snap core.UsageSnapshot
	snap.Metrics = make(map[string]core.Metric)
	snap.Resets = make(map[string]time.Time)

	projectQuotaMetrics(&snap, payload)

	if rem := metricRemaining(t, snap, "quota_gemini_weekly"); rem != 100 {
		t.Fatalf("quota_gemini_weekly remaining = %v, want 100 after reset passed", rem)
	}
	reset, ok := snap.Resets["quota_gemini_weekly"]
	if !ok {
		t.Fatal("missing quota_gemini_weekly reset")
	}
	if !reset.After(time.Now().UTC()) {
		t.Fatalf("reset time = %v, expected future timestamp after advancing weekly period", reset)
	}
}

func TestQuotaMapFromSummary(t *testing.T) {
	summary := quotaSummaryResponse{
		Groups: []quotaGroup{{
			Buckets: []quotaBucket{
				{BucketID: "gemini-5h", RemainingFraction: core.Float64Ptr(0.5), ResetTime: "2030-01-01T00:00:00Z"},
			},
		}},
	}
	got := quotaMapFromSummary(summary)
	if len(got) != 1 || got["gemini-5h"].RemainingFraction == nil || *got["gemini-5h"].RemainingFraction != 0.5 {
		t.Fatalf("unexpected map: %+v", got)
	}
}

func TestEnrichSnapshotsOverlaysLiveQuota(t *testing.T) {
	quotaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"groups":[{"buckets":[
				{"bucketId":"gemini-5h","remainingFraction":0.42,"resetTime":"2030-01-01T00:00:00Z"},
				{"bucketId":"gemini-weekly","remainingFraction":0.8,"resetTime":"2030-01-08T00:00:00Z"}
			]}]
		}`))
	}))
	defer quotaServer.Close()

	configDir := t.TempDir()
	writeTestToken(t, filepath.Join(configDir, oauthTokenFile), "access-token", "2030-01-01T00:00:00Z", "refresh-token")

	p := New()
	p.HTTPClient = quotaServer.Client()
	snaps := map[string]core.UsageSnapshot{
		"antigravity-chaos": {
			ProviderID: "antigravity",
			AccountID:  "antigravity-chaos",
			Status:     core.StatusUnknown,
			Metrics:    map[string]core.Metric{},
		},
	}
	p.EnrichSnapshots(context.Background(), []core.AccountConfig{{
		ID:       "antigravity-chaos",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"config_dir": configDir,
		},
		RuntimeHints: map[string]string{
			"quota_endpoint": quotaServer.URL + "/v1internal",
		},
	}}, snaps)

	snap := snaps["antigravity-chaos"]
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want ok", snap.Status)
	}
	if got := *snap.Metrics["quota_gemini_5h"].Remaining; got != 42 {
		t.Fatalf("quota_gemini_5h = %v, want 42", got)
	}
}

func TestParseHookPayloadRejected(t *testing.T) {
	if _, err := New().ParseHookPayload([]byte(`{}`), shared.TelemetryCollectOptions{}); err == nil {
		t.Fatal("expected status-line hook rejection")
	}
}

func writeTestToken(t *testing.T, path, access, expiry, refresh string) {
	t.Helper()
	payload := oauthTokenFilePayload{
		Token: oauthToken{
			AccessToken:  access,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			Expiry:       expiry,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func metricRemaining(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	metric, ok := snap.Metrics[key]
	if !ok || metric.Remaining == nil {
		t.Fatalf("metric %q missing remaining value: %+v", key, metric)
	}
	return *metric.Remaining
}
