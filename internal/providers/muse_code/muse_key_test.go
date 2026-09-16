package muse_code

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func stubMuseOAuthToken(t *testing.T, token string, ok bool) {
	t.Helper()
	prev := loadMuseOAuthToken
	loadMuseOAuthToken = func(ctx context.Context) (string, bool) { return token, ok }
	t.Cleanup(func() { loadMuseOAuthToken = prev })
}

// Fixture values are invented ("tier-999" / "Test Usage") — never real
// account data. Shape mirrors POST muse-code/key.
const keySubscriptionFixture = `{"subs_tier_id":"tier-999",` +
	`"subs_tier_name":"Muse Code Test Usage",` +
	`"is_subs_upgrade_available":true,` +
	`"subs_usage":{"window":{"used_percent":7,"window_duration_mins":300,"resets_at":1788858956},` +
	`"weekly":{"used_percent":13,"resets_at":1789344000},"tier":"tier-999"}}`

func TestEnrichQuota_KeyEndpointFirst(t *testing.T) {
	stubMuseAPIKey(t, "test-key", true)
	stubMuseOAuthToken(t, "oauth-token", true)
	var gotAuth, gotPath, gotMethod, gotContentType string
	var probeCalled bool
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			probeCalled = true
			return quotaTestResponse(http.StatusOK, subscriptionSSEFixture), nil
		}
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		return quotaTestResponse(http.StatusOK, keySubscriptionFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if probeCalled {
		t.Error("Responses probe ran despite a successful account endpoint call")
	}
	if gotMethod != http.MethodPost || gotPath != "/muse-code/key" {
		t.Errorf("method/path = %s %s, want POST /muse-code/key", gotMethod, gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") || !strings.HasSuffix(gotAuth, "oauth-token") {
		t.Error("auth header has wrong scheme or token")
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q", gotContentType)
	}
	sess, ok := snap.Metrics["muse.session"]
	if !ok || sess.Used == nil || *sess.Used != 7 {
		t.Errorf("muse.session = %+v, want 7", sess)
	}
	weekly, ok := snap.Metrics["muse.weekly"]
	if !ok || weekly.Used == nil || *weekly.Used != 13 {
		t.Errorf("muse.weekly = %+v, want 13", weekly)
	}
	if got := snap.Attributes["muse_quota_tier"]; got != "tier-999" {
		t.Errorf("tier = %q, want stable numeric ID", got)
	}
	if got := snap.Raw["plan_name"]; got != "Test Usage" {
		t.Errorf("plan_name = %q, want server display name with prefix trimmed", got)
	}
}

func TestEnrichQuota_KeyUnauthorizedFallsBackToProbe(t *testing.T) {
	stubMuseAPIKey(t, "test-key", true)
	stubMuseOAuthToken(t, "expired-token", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/muse-code/key") {
			return quotaTestResponse(http.StatusUnauthorized, `{"title":"Authentication Error","status":401}`), nil
		}
		return quotaTestResponse(http.StatusOK, subscriptionSSEFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	sess, ok := snap.Metrics["muse.session"]
	if !ok || sess.Used == nil || *sess.Used != 42 {
		t.Errorf("muse.session = %+v, want probe fallback 42", sess)
	}
	if got := snap.Raw["plan_name"]; got != "tier-123" {
		t.Errorf("plan_name = %q, want probe tier passthrough", got)
	}
	if hint := snap.Diagnostics["muse_quota_oauth"]; !strings.Contains(hint, "muse login") {
		t.Errorf("oauth hint = %q, want re-authenticate guidance", hint)
	}
}

func TestEnrichQuota_KeyServerErrorFallsBackToProbe(t *testing.T) {
	stubMuseAPIKey(t, "test-key", true)
	stubMuseOAuthToken(t, "oauth-token", true)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/muse-code/key") {
			return quotaTestResponse(http.StatusBadGateway, "upstream unavailable"), nil
		}
		return quotaTestResponse(http.StatusOK, subscriptionSSEFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if sess, ok := snap.Metrics["muse.session"]; !ok || sess.Used == nil || *sess.Used != 42 {
		t.Errorf("muse.session = %+v, want probe fallback 42", sess)
	}
	if hint := snap.Diagnostics["muse_quota_key"]; !strings.Contains(hint, "account endpoint failed") {
		t.Errorf("key hint = %q, want transient-failure diagnostic", hint)
	}
}

func TestParseMuseSecretBlob(t *testing.T) {
	key, oauth := parseMuseSecretBlob([]byte(`{"secret_schema_version":1,"api_key":"k","access_token":"o"}`))
	if key != "k" || oauth != "o" {
		t.Errorf("got (%q, %q), want both credentials", key, oauth)
	}
	key, oauth = parseMuseSecretBlob([]byte(`{"api_key":"k"}`))
	if key != "k" || oauth != "" {
		t.Errorf("got (%q, %q), want API key only", key, oauth)
	}
	key, oauth = parseMuseSecretBlob([]byte(`not json`))
	if key != "" || oauth != "" {
		t.Errorf("got (%q, %q), want empty on malformed blob", key, oauth)
	}
}

func TestSubscriptionUsageFromKey_PrefersUsageTier(t *testing.T) {
	var k keySubscription
	k.SubsTierID = "outer-id"
	k.SubsTierName = "Muse Code Test Usage"
	k.SubsUsage.Tier = "inner-id"
	k.SubsUsage.Weekly.UsedPercent = 13
	k.SubsUsage.Weekly.ResetsAt = 1789344000
	k.SubsUsage.Window.UsedPercent = 7
	k.SubsUsage.Window.ResetsAt = 1788858956
	k.SubsUsage.Window.WindowDurationMins = 300

	sub := subscriptionUsageFromKey(&k)
	if sub.Tier != "inner-id" {
		t.Errorf("tier = %q, want subs_usage.tier", sub.Tier)
	}
	if sub.PlanName != "Test Usage" {
		t.Errorf("plan = %q, want trimmed display name", sub.PlanName)
	}
	if sub.Window.WindowDurationMins != 300 {
		t.Errorf("duration = %d", sub.Window.WindowDurationMins)
	}
}

func TestSubscriptionUsageFromKey_FallsBackToOuterTierID(t *testing.T) {
	var k keySubscription
	k.SubsTierID = "outer-id"
	sub := subscriptionUsageFromKey(&k)
	if sub.Tier != "outer-id" {
		t.Errorf("tier = %q, want subs_tier_id", sub.Tier)
	}
	if sub.PlanName != "" {
		t.Errorf("plan = %q, want empty without a display name", sub.PlanName)
	}
}
