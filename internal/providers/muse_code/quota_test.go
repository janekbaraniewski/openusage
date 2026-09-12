package muse_code

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubMuseAPIKey(t *testing.T, key string, ok bool) {
	t.Helper()
	prev := loadMuseAPIKey
	loadMuseAPIKey = func(ctx context.Context) (string, bool) { return key, ok }
	t.Cleanup(func() { loadMuseAPIKey = prev })
}

func stubQuotaTransport(t *testing.T, fn roundTripFunc) {
	t.Helper()
	prev := quotaHTTPClient
	quotaHTTPClient = func() *http.Client { return &http.Client{Transport: fn} }
	t.Cleanup(func() { quotaHTTPClient = prev })
}

func quotaTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const subscriptionSSEFixture = "event: response.created\n" +
	"data: {\"type\":\"response.created\"}\n" +
	"\n" +
	"event: response.subscription_usage\n" +
	"data: {\"subscription\":{\"tier\":\"tier-123\",\"weekly\":{\"resets_at\":1789344000,\"used_percent\":49},\"window\":{\"resets_at\":1788858956,\"used_percent\":42,\"window_duration_mins\":300}},\"type\":\"response.subscription_usage\"}\n" +
	"\n" +
	"data: [DONE]\n"

func TestEnrichQuota_SubscriptionUsageSSE(t *testing.T) {
	stubMuseAPIKey(t, "test-key", true)
	var gotAuth, gotPath, gotAccept string
	var gotBody map[string]any
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read probe body: %v", err)
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Fatalf("probe body not JSON: %v", err)
		}
		return quotaTestResponse(http.StatusOK, subscriptionSSEFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK
	snap.Message = "spend summary"

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if !strings.HasPrefix(gotAuth, "Bearer ") || !strings.HasSuffix(gotAuth, "test-key") {
		t.Errorf("auth header has wrong scheme or key")
	}
	if gotPath != "/v1/responses" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("accept = %q", gotAccept)
	}
	if gotBody["stream"] != true || gotBody["model"] != responsesProbeModel {
		t.Errorf("probe body = %v", gotBody)
	}
	sess, ok := snap.Metrics["muse.session"]
	if !ok || sess.Used == nil || *sess.Used != 42 || sess.Limit == nil || *sess.Limit != 100 {
		t.Errorf("muse.session = %+v", sess)
	}
	weekly, ok := snap.Metrics["muse.weekly"]
	if !ok || weekly.Used == nil || *weekly.Used != 49 || weekly.Limit == nil || *weekly.Limit != 100 {
		t.Errorf("muse.weekly = %+v", weekly)
	}
	if _, ok := snap.Resets["muse.session"]; !ok {
		t.Error("muse.session reset missing")
	}
	if !strings.Contains(snap.Message, "quota session 42%") || !strings.Contains(snap.Message, "quota weekly 49%") {
		t.Errorf("message = %q, want quota summary suffix", snap.Message)
	}
	if got := snap.Attributes["muse_quota_tier"]; got != "tier-123" {
		t.Errorf("tier = %q", got)
	}
	if got := snap.Raw["plan_name"]; got != "tier-123" {
		t.Errorf("plan_name = %q, want opaque tier passthrough", got)
	}
}

func TestEnrichQuota_SubscriptionUsageUnauthorized(t *testing.T) {
	stubMuseAPIKey(t, "stale-key", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusUnauthorized, `{"error":{"message":"Unauthorized"}}`), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if hint := snap.Diagnostics["muse_quota_auth"]; !strings.Contains(hint, "muse login") {
		t.Errorf("auth hint = %q, want re-authenticate guidance", hint)
	}
	if _, ok := snap.Metrics["muse.session"]; ok {
		t.Error("unexpected meter on 401")
	}
}

func TestEnrichQuota_SubscriptionUsageNoEvent(t *testing.T) {
	stubMuseAPIKey(t, "test-key", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusOK, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\ndata: [DONE]\n"), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if snap.Diagnostics["muse_quota_error"] == "" {
		t.Error("expected quota error diagnostic for missing usage event")
	}
	if _, ok := snap.Metrics["muse.session"]; ok {
		t.Error("unexpected meter without usage event")
	}
}

func TestQuotaPlanName(t *testing.T) {
	for in, want := range map[string]string{
		"Muse Code Everyday Usage":     "Everyday Usage",
		"Muse Code High Usage":         "High Usage",
		"Muse Code Power Usage":        "Power Usage",
		"27681393394859588":            "27681393394859588",
		"  Muse Code Everyday Usage  ": "Everyday Usage",
		"":                             "",
	} {
		if got := quotaPlanName(in); got != want {
			t.Errorf("quotaPlanName(%q) = %q, want %q", in, got, want)
		}
	}
}
