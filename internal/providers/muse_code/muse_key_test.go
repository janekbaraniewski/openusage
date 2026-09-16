package muse_code

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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

func stubMuseKeychainBlob(t *testing.T, raw []byte, ok bool) {
	t.Helper()
	prev := readMuseSecretBlobFromKeychain
	readMuseSecretBlobFromKeychain = func(ctx context.Context) ([]byte, bool) { return raw, ok }
	t.Cleanup(func() { readMuseSecretBlobFromKeychain = prev })
}

func resetMuseOAuthTokenCache(t *testing.T) {
	t.Helper()
	museOAuthTokenCache.Lock()
	prev := museOAuthTokenCache
	museOAuthTokenCache.token, museOAuthTokenCache.ok, museOAuthTokenCache.set = "", false, false
	museOAuthTokenCache.Unlock()
	t.Cleanup(func() {
		museOAuthTokenCache.Lock()
		museOAuthTokenCache = prev
		museOAuthTokenCache.Unlock()
	})
}

// useRealMuseOAuthLoader restores the real OAuth loader (the fetch-test
// stubs pin it off) while keeping the API-key loader stubbed off and the
// memo cleared, so refresh tests exercise real file reads under a fake
// HOME without touching real credentials or the real keychain.
func useRealMuseOAuthLoader(t *testing.T) {
	t.Helper()
	orig := loadMuseOAuthToken
	stubMuseAPIKey(t, "", false)
	loadMuseOAuthToken = orig
	t.Cleanup(func() { loadMuseOAuthToken = orig })
	resetMuseOAuthTokenCache(t)
}

func plantMuseCLIAuthFile(t *testing.T, home, token string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "muse")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir cli auth: %v", err)
	}
	doc := `{"providers":{"meta":{"access_token":"` + token + `"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(doc), 0o600); err != nil {
		t.Fatalf("write cli auth: %v", err)
	}
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
	// Fake HOME plus a stubbed keychain: the 401 refresh path uses real
	// file/keychain readers, which must never see real credentials here.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(os.TempDir(), "muse-quota-memory-test-"+strings.Replace(t.Name(), "/", "-", -1)+".json"))
	stubMuseKeychainBlob(t, nil, false)
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

func TestOAuthOwnedRoundTripPreservesAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(home, "muse-quota-memory.json"))
	if err := writeMuseOwnedKeys(map[string]string{"apiKey": "k"}); err != nil {
		t.Fatalf("seed owned file: %v", err)
	}
	if err := saveMuseOAuthTokenToOwnedFile("o"); err != nil {
		t.Fatalf("save oauth: %v", err)
	}
	token, ok := loadMuseOAuthTokenFromOwnedFile()
	if !ok || token != "o" {
		t.Errorf("oauth = %q, %v; want o, true", token, ok)
	}
	key, ok := loadMuseAPIKeyFromFile()
	if !ok || key != "k" {
		t.Errorf("api key = %q, %v; want k, true", key, ok)
	}
	info, err := os.Stat(filepath.Join(home, ".config", "openusage", "muse.json"))
	if err != nil {
		t.Fatalf("stat owned file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("owned file perm = %o, want 600", info.Mode().Perm())
	}
}

func TestOAuthOwnedRefusesMalformed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(home, "muse-quota-memory.json"))
	dir := filepath.Join(home, ".config", "openusage")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Raw-string key file (a supported apiKey shape): must not be clobbered.
	if err := os.WriteFile(filepath.Join(dir, "muse.json"), []byte("raw-key"), 0o600); err != nil {
		t.Fatalf("seed raw file: %v", err)
	}
	if err := saveMuseOAuthTokenToOwnedFile("o"); err == nil {
		t.Error("save over unparseable file should refuse")
	}
	if token, ok := loadMuseOAuthTokenFromOwnedFile(); ok || token != "" {
		t.Errorf("oauth = %q, %v; want empty", token, ok)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "muse.json")); string(raw) != "raw-key" {
		t.Errorf("raw file clobbered: %q", raw)
	}
}

func TestOAuthFirstBootstrapsFromCLIFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(home, "muse-quota-memory.json"))
	plantMuseCLIAuthFile(t, home, "fresh-token")
	useRealMuseOAuthLoader(t)
	stubMuseKeychainBlob(t, nil, false)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/muse-code/key") {
			t.Errorf("unexpected path %s on first boot", r.URL.Path)
			return quotaTestResponse(http.StatusInternalServerError, ""), nil
		}
		return quotaTestResponse(http.StatusOK, keySubscriptionFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if got := snap.Raw["plan_name"]; got != "Test Usage" {
		t.Errorf("plan_name = %q", got)
	}
	// The CLI-file token must now be cached in the owned file for later
	// boots, which never touch CLI sources again.
	token, ok := loadMuseOAuthTokenFromOwnedFile()
	if !ok || token != "fresh-token" {
		t.Errorf("cached oauth = %q, %v; want fresh-token", token, ok)
	}
}

func TestOAuthRefresh_StaleOwnedRebootstraps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(home, "muse-quota-memory.json"))
	if err := writeMuseOwnedKeys(map[string]string{"oauthToken": "stale-token"}); err != nil {
		t.Fatalf("seed owned file: %v", err)
	}
	plantMuseCLIAuthFile(t, home, "fresh-token")
	useRealMuseOAuthLoader(t)
	stubMuseKeychainBlob(t, nil, false)
	var probeCalled bool
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			probeCalled = true
			return quotaTestResponse(http.StatusInternalServerError, ""), nil
		}
		if strings.HasSuffix(r.Header.Get("Authorization"), "stale-token") {
			return quotaTestResponse(http.StatusUnauthorized, `{"title":"Authentication Error","status":401}`), nil
		}
		if !strings.HasSuffix(r.Header.Get("Authorization"), "fresh-token") {
			t.Errorf("retry used wrong token")
			return quotaTestResponse(http.StatusUnauthorized, ""), nil
		}
		return quotaTestResponse(http.StatusOK, keySubscriptionFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if probeCalled {
		t.Error("probe ran despite a successful refresh retry")
	}
	if got := snap.Raw["plan_name"]; got != "Test Usage" {
		t.Errorf("plan_name = %q", got)
	}
	if _, ok := snap.Diagnostics["muse_quota_oauth"]; ok {
		t.Error("stale 401 diagnostic survived the successful retry")
	}
	token, ok := loadMuseOAuthTokenFromOwnedFile()
	if !ok || token != "fresh-token" {
		t.Errorf("cached oauth = %q, %v; want fresh-token", token, ok)
	}
}

func TestOAuthRefresh_NoRefreshFallsBackToProbe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MUSE_QUOTA_MEMORY_PATH", filepath.Join(home, "muse-quota-memory.json"))
	if err := writeMuseOwnedKeys(map[string]string{"oauthToken": "stale-token"}); err != nil {
		t.Fatalf("seed owned file: %v", err)
	}
	// No CLI auth file and a stubbed keychain: nothing to re-bootstrap from.
	useRealMuseOAuthLoader(t)
	// The probe fallback needs an API key; useRealMuseOAuthLoader leaves
	// it stubbed off, so re-enable just that loader (the OAuth var stays
	// real — stubMuseAPIKey would re-pin it off).
	prevAPI := loadMuseAPIKey
	loadMuseAPIKey = func(ctx context.Context) (string, bool) { return "test-key", true }
	t.Cleanup(func() { loadMuseAPIKey = prevAPI })
	stubMuseKeychainBlob(t, nil, false)
	stubQuotaTransport(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/muse-code/key") {
			return quotaTestResponse(http.StatusUnauthorized, `{"title":"Authentication Error","status":401}`), nil
		}
		return quotaTestResponse(http.StatusOK, subscriptionSSEFixture), nil
	})

	snap := core.NewUsageSnapshot("muse_code", "muse-code")
	snap.Status = core.StatusOK

	enrichQuota(context.Background(), core.AccountConfig{ID: "muse-code"}, &snap)

	if sess, ok := snap.Metrics["muse.session"]; !ok || sess.Used == nil || *sess.Used != 42 {
		t.Errorf("muse.session = %+v, want probe fallback 42", sess)
	}
	if hint := snap.Diagnostics["muse_quota_oauth"]; !strings.Contains(hint, "muse login") {
		t.Errorf("oauth hint = %q, want re-authenticate guidance", hint)
	}
	// The stale copy must be gone so the next boot doesn't retry it blindly.
	if token, ok := loadMuseOAuthTokenFromOwnedFile(); ok || token != "" {
		t.Errorf("stale owned copy survived: %q", token)
	}
}
