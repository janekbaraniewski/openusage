package muse_code

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func writeQuotaTokens(t *testing.T, params map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "quota-tokens.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validQuotaParams() map[string]string {
	return map[string]string{
		"fb_dtsg": "test-dtsg",
		"lsd":     "test-lsd",
		"doc_id":  "test-doc",
		"__s":     "test-s",
	}
}

func quotaTestAcct(tokensPath string) core.AccountConfig {
	acct := core.AccountConfig{ID: "muse-code", Provider: "muse_code", Auth: "local"}
	if tokensPath != "" {
		acct.SetPath("quota_tokens_file", tokensPath)
	}
	acct.SetPath("team_id", "team-123")
	return acct
}

func stubQuotaSession(t *testing.T, sess config.BrowserSession, ok bool, err error) {
	t.Helper()
	prev := loadQuotaSession
	loadQuotaSession = func(ctx context.Context, acct core.AccountConfig) (config.BrowserSession, bool, error) {
		return sess, ok, err
	}
	t.Cleanup(func() { loadQuotaSession = prev })
}

// roundTripFunc adapts a func to http.RoundTripper so tests exercise the full
// request path without binding a socket.
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

func parsePostedForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		t.Fatalf("parse posted form: %v", err)
	}
	return form
}

func TestEnrichQuota_NotConfigured(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	t.Setenv("MUSE_QUOTA_TOKENS_FILE", "")
	t.Setenv("MUSE_QUOTA_TEAM_ID", "")
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK
	snap.Message = "spend summary"

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if _, ok := snap.Metrics["muse.session"]; ok {
		t.Error("unexpected muse.session meter without configuration")
	}
	if snap.Diagnostics["muse_quota"] == "" {
		t.Error("expected muse_quota diagnostic hint")
	}
	if snap.Message != "spend summary" {
		t.Errorf("message changed: %q", snap.Message)
	}
}

func TestEnrichQuota_MissingTeamID(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	t.Setenv("MUSE_QUOTA_TEAM_ID", "")
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK
	acct := core.AccountConfig{ID: "muse-code"}
	acct.SetPath("quota_tokens_file", writeQuotaTokens(t, validQuotaParams()))

	enrichQuota(context.Background(), acct, &snap)

	if snap.Diagnostics["muse_quota"] == "" {
		t.Error("expected team_id hint diagnostic")
	}
}

func TestEnrichQuota_NoSession(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	stubQuotaSession(t, config.BrowserSession{}, false, nil)
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK
	snap.Message = "spend summary"

	enrichQuota(context.Background(), quotaTestAcct(writeQuotaTokens(t, validQuotaParams())), &snap)

	hint := snap.Diagnostics["muse_quota_auth"]
	if !strings.Contains(hint, "dev.meta.ai") {
		t.Errorf("auth hint = %q, want dev.meta.ai revisit guidance", hint)
	}
	if snap.Status != core.StatusOK || snap.Message != "spend summary" {
		t.Errorf("local snapshot disturbed: status=%v message=%q", snap.Status, snap.Message)
	}
}

func TestEnrichQuota_BadTokensFile(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), quotaTestAcct(writeQuotaTokens(t, map[string]string{"lsd": "x"})), &snap)

	if snap.Diagnostics["muse_quota_error"] == "" {
		t.Error("expected quota error diagnostic for tokens missing fb_dtsg")
	}
}

const quotaFixtureResponse = `{"data":{"team":{` +
	`"subscription_quota_usage":{"tier":"Muse Code Everyday Usage","as_of":1788000000,` +
	`"window_weighted_used":"42.5","window_weighted_limit":"100.0","window_resets_at":1788100000,` +
	`"weekly_weighted_used":"10","weekly_weighted_limit":"200","weekly_resets_at":1788600000},` +
	`"available_models":[{"model_id":"muse-spark-1.3"}]}}}`

func TestEnrichQuota_Success(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	var gotCookie, gotFriendly, gotVariables string
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		gotCookie = r.Header.Get("Cookie")
		form := parsePostedForm(t, r)
		gotFriendly = form.Get("fb_api_req_friendly_name")
		gotVariables = form.Get("variables")
		return quotaTestResponse(http.StatusOK, quotaFixtureResponse), nil
	})
	stubQuotaSession(t, config.BrowserSession{Domain: "dev.meta.ai", CookieName: "llm_sess", Value: "sess-value"}, true, nil)

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK
	snap.Message = "spend summary"

	enrichQuota(context.Background(), quotaTestAcct(writeQuotaTokens(t, validQuotaParams())), &snap)

	if !strings.Contains(gotCookie, "llm_sess=sess-value") {
		t.Errorf("cookie header = %q", gotCookie)
	}
	if gotFriendly != "LLMDCUsageQuery" {
		t.Errorf("friendly name = %q", gotFriendly)
	}
	var vars map[string]any
	if err := json.Unmarshal([]byte(gotVariables), &vars); err != nil {
		t.Fatalf("variables not JSON: %v", err)
	}
	if vars["team_id"] != "team-123" {
		t.Errorf("variables team_id = %v", vars["team_id"])
	}

	sess, ok := snap.Metrics["muse.session"]
	if !ok || sess.Used == nil || *sess.Used != 42.5 || sess.Limit == nil || *sess.Limit != 100 {
		t.Errorf("muse.session = %+v", sess)
	}
	weekly, ok := snap.Metrics["muse.weekly"]
	if !ok || weekly.Used == nil || *weekly.Used != 10 || weekly.Limit == nil || *weekly.Limit != 200 {
		t.Errorf("muse.weekly = %+v", weekly)
	}
	if _, ok := snap.Resets["muse.session"]; !ok {
		t.Error("muse.session reset missing")
	}
	if !strings.Contains(snap.Message, "quota session 42%") {
		t.Errorf("message = %q, want quota summary suffix", snap.Message)
	}
	if got := snap.Attributes["muse_quota_tier"]; got != "Muse Code Everyday Usage" {
		t.Errorf("tier = %q", got)
	}
	if got := snap.Raw["plan_name"]; got != "Everyday Usage" {
		t.Errorf("plan_name = %q, want Everyday Usage", got)
	}
}

func TestEnrichQuota_EnvFallback(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusOK, quotaFixtureResponse), nil
	})
	stubQuotaSession(t, config.BrowserSession{Domain: "dev.meta.ai", CookieName: "llm_sess", Value: "sess-value"}, true, nil)
	tokensPath := writeQuotaTokens(t, validQuotaParams())
	t.Setenv("MUSE_QUOTA_TOKENS_FILE", tokensPath)
	t.Setenv("MUSE_QUOTA_TEAM_ID", "team-env")

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if _, ok := snap.Metrics["muse.session"]; !ok {
		t.Errorf("env-configured quota produced no meters, diagnostics=%v", snap.Diagnostics)
	}
}

func TestEnrichQuota_Unauthorized(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusUnauthorized, ""), nil
	})
	stubQuotaSession(t, config.BrowserSession{Domain: "dev.meta.ai", CookieName: "llm_sess", Value: "stale"}, true, nil)

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), quotaTestAcct(writeQuotaTokens(t, validQuotaParams())), &snap)

	hint := snap.Diagnostics["muse_quota_auth"]
	if !strings.Contains(hint, "Chrome") {
		t.Errorf("auth hint = %q, want Chrome revisit guidance", hint)
	}
	if _, ok := snap.Metrics["muse.session"]; ok {
		t.Error("unexpected meter on 401")
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

	// No tokens file or team_id: the SSE probe needs neither.
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

func TestEnrichQuota_GraphQLErrors(t *testing.T) {
	stubMuseAPIKey(t, "", false)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		return quotaTestResponse(http.StatusOK, `{"data":null,"errors":[{"message":"session expired"}]}`), nil
	})
	stubQuotaSession(t, config.BrowserSession{Domain: "dev.meta.ai", CookieName: "llm_sess", Value: "stale"}, true, nil)

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), quotaTestAcct(writeQuotaTokens(t, validQuotaParams())), &snap)

	if snap.Diagnostics["muse_quota_auth"] == "" {
		t.Error("expected auth diagnostic for GraphQL error payload")
	}
}
