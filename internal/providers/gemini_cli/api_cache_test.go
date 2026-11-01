package gemini_cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// cachingTestAccount points Binary at a missing path so Fetch does not shell
// out to a locally installed gemini binary.
func cachingTestAccount(id, configDir string) core.AccountConfig {
	acct := testGeminiCLIAccount(id, configDir)
	acct.Binary = filepath.Join(configDir, "no-such-gemini-binary")
	return acct
}

type codeAssistFixture struct {
	tokenCalls atomic.Int32
	loadCalls  atomic.Int32
	quotaCalls atomic.Int32

	mu         sync.Mutex
	authTokens []string
	issued     int
	// rejectToken makes Code Assist endpoints answer 401 for that bearer token.
	rejectToken string
	// tokenStatus overrides the token endpoint status when non-zero.
	tokenStatus int
}

func (f *codeAssistFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			f.tokenCalls.Add(1)
			f.mu.Lock()
			status := f.tokenStatus
			f.issued++
			n := f.issued
			f.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"invalid_grant","error_description":"leaked-secret-value"}`)
				return
			}
			fmt.Fprintf(w, `{"access_token":"ya29.refreshed-%d","expires_in":3600,"token_type":"Bearer"}`, n)
			return
		}

		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Lock()
		f.authTokens = append(f.authTokens, bearer)
		reject := f.rejectToken != "" && bearer == f.rejectToken
		f.mu.Unlock()
		if reject {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":401}}`)
			return
		}

		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			f.loadCalls.Add(1)
			fmt.Fprint(w, `{"cloudaicompanionProject":"proj-1","currentTier":{"id":"free-tier","name":"Free Tier"}}`)
		case "/v1internal:retrieveUserQuota":
			f.quotaCalls.Add(1)
			fmt.Fprint(w, `{"buckets":[{"modelId":"gemini-2.5-pro","remainingFraction":0.5,"resetTime":"2099-01-01T00:00:00Z","tokenType":"requests"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *codeAssistFixture) lastAuthToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.authTokens) == 0 {
		return ""
	}
	return f.authTokens[len(f.authTokens)-1]
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newCachingTestProvider(t *testing.T, srv *httptest.Server, clock *testClock) *Provider {
	t.Helper()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT_ID", "")
	p := New()
	p.tokenEndpoint = srv.URL + "/token"
	p.codeAssistEndpoint = srv.URL
	p.clock = clock.Now
	return p
}

func writeTestCreds(t *testing.T, accessToken string, expiry time.Time) string {
	t.Helper()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "oauth_creds.json"), oauthCreds{
		AccessToken:  accessToken,
		ExpiryDate:   expiry.UnixMilli(),
		RefreshToken: "1//refresh-token-test",
	})
	return dir
}

func TestFetch_ReusesValidCredsFileTokenAndCachesLoadCodeAssist(t *testing.T) {
	fx := &codeAssistFixture{}
	srv := fx.server(t)
	clock := &testClock{now: time.Now()}
	p := newCachingTestProvider(t, srv, clock)
	dir := writeTestCreds(t, "ya29.from-file", clock.Now().Add(time.Hour))
	acct := cachingTestAccount("gemini-a", dir)

	for i := 0; i < 3; i++ {
		snap, err := p.Fetch(context.Background(), acct)
		if err != nil {
			t.Fatalf("Fetch() error: %v", err)
		}
		if snap.Raw["quota_api_error"] != "" {
			t.Fatalf("poll %d quota_api_error = %q", i+1, snap.Raw["quota_api_error"])
		}
		if snap.Raw["project_id"] != "proj-1" || snap.Raw["tier_name"] != "Free Tier" {
			t.Errorf("poll %d project/tier = %q/%q, want proj-1/Free Tier (cached metadata must still apply)", i+1, snap.Raw["project_id"], snap.Raw["tier_name"])
		}
		if snap.Raw["oauth_status"] != "valid" {
			t.Errorf("poll %d oauth_status = %q, want valid", i+1, snap.Raw["oauth_status"])
		}
		clock.advance(10 * time.Minute)
	}

	if got := fx.tokenCalls.Load(); got != 0 {
		t.Errorf("token refresh calls = %d, want 0 while the creds file token is valid", got)
	}
	if got := fx.loadCalls.Load(); got != 1 {
		t.Errorf("loadCodeAssist calls = %d, want 1", got)
	}
	if got := fx.quotaCalls.Load(); got != 3 {
		t.Errorf("retrieveUserQuota calls = %d, want 3", got)
	}
	if got := fx.lastAuthToken(); got != "ya29.from-file" {
		t.Errorf("bearer = %q, want ya29.from-file", got)
	}
}

func TestFetch_RefreshesNearExpiryAndReusesRefreshedToken(t *testing.T) {
	fx := &codeAssistFixture{}
	srv := fx.server(t)
	start := time.Now()
	clock := &testClock{now: start}
	p := newCachingTestProvider(t, srv, clock)
	// Inside the 5-minute skew: must not be used.
	dir := writeTestCreds(t, "ya29.nearly-expired", start.Add(3*time.Minute))
	acct := cachingTestAccount("gemini-b", dir)

	poll := func(label string) {
		t.Helper()
		snap, err := p.Fetch(context.Background(), acct)
		if err != nil {
			t.Fatalf("%s: Fetch() error: %v", label, err)
		}
		if snap.Raw["quota_api_error"] != "" {
			t.Fatalf("%s: quota_api_error = %q", label, snap.Raw["quota_api_error"])
		}
	}

	poll("t0")
	if got := fx.tokenCalls.Load(); got != 1 {
		t.Fatalf("t0 token calls = %d, want 1", got)
	}
	if got := fx.lastAuthToken(); got != "ya29.refreshed-1" {
		t.Errorf("t0 bearer = %q, want ya29.refreshed-1", got)
	}

	clock.advance(10 * time.Minute)
	poll("t0+10m")
	if got := fx.tokenCalls.Load(); got != 1 {
		t.Errorf("t0+10m token calls = %d, want 1 (reuse refreshed token)", got)
	}
	if got := fx.lastAuthToken(); got != "ya29.refreshed-1" {
		t.Errorf("t0+10m bearer = %q, want ya29.refreshed-1", got)
	}

	// Refreshed token expires at t0+60m; at t0+56m it is inside the skew.
	clock.advance(46 * time.Minute)
	poll("t0+56m")
	if got := fx.tokenCalls.Load(); got != 2 {
		t.Errorf("t0+56m token calls = %d, want 2", got)
	}
	if got := fx.lastAuthToken(); got != "ya29.refreshed-2" {
		t.Errorf("t0+56m bearer = %q, want ya29.refreshed-2", got)
	}
	if got := fx.loadCalls.Load(); got != 1 {
		t.Errorf("t0+56m loadCodeAssist calls = %d, want 1", got)
	}

	// loadCodeAssist TTL (1h) elapses; the new token is still fresh.
	clock.advance(5 * time.Minute)
	poll("t0+61m")
	if got := fx.tokenCalls.Load(); got != 2 {
		t.Errorf("t0+61m token calls = %d, want 2", got)
	}
	if got := fx.loadCalls.Load(); got != 2 {
		t.Errorf("t0+61m loadCodeAssist calls = %d, want 2 after TTL", got)
	}
}

func TestFetch_RefreshesOnceWhenReusedTokenIsRejected(t *testing.T) {
	fx := &codeAssistFixture{rejectToken: "ya29.revoked"}
	srv := fx.server(t)
	clock := &testClock{now: time.Now()}
	p := newCachingTestProvider(t, srv, clock)
	dir := writeTestCreds(t, "ya29.revoked", clock.Now().Add(time.Hour))
	acct := cachingTestAccount("gemini-c", dir)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Raw["quota_api_error"] != "" {
		t.Fatalf("quota_api_error = %q", snap.Raw["quota_api_error"])
	}
	if got := fx.tokenCalls.Load(); got != 1 {
		t.Errorf("token calls = %d, want 1", got)
	}
	if snap.Raw["oauth_status"] != "valid (refreshed)" {
		t.Errorf("oauth_status = %q, want valid (refreshed)", snap.Raw["oauth_status"])
	}
	if got := fx.lastAuthToken(); got != "ya29.refreshed-1" {
		t.Errorf("bearer = %q, want ya29.refreshed-1", got)
	}
}

func TestFetch_LoadCodeAssistCacheIsPerAccount(t *testing.T) {
	fx := &codeAssistFixture{}
	srv := fx.server(t)
	clock := &testClock{now: time.Now()}
	p := newCachingTestProvider(t, srv, clock)
	dirA := writeTestCreds(t, "ya29.a", clock.Now().Add(time.Hour))
	dirB := writeTestCreds(t, "ya29.b", clock.Now().Add(time.Hour))

	for _, acct := range []struct{ id, dir string }{{"acct-a", dirA}, {"acct-b", dirB}, {"acct-a", dirA}, {"acct-b", dirB}} {
		if _, err := p.Fetch(context.Background(), cachingTestAccount(acct.id, acct.dir)); err != nil {
			t.Fatalf("Fetch(%s) error: %v", acct.id, err)
		}
	}
	if got := fx.loadCalls.Load(); got != 2 {
		t.Errorf("loadCodeAssist calls = %d, want 2 (one per account)", got)
	}
}

func TestFetch_RejectedFileTokenIsNotRetriedOnNextPoll(t *testing.T) {
	fx := &codeAssistFixture{rejectToken: "ya29.revoked"}
	srv := fx.server(t)
	clock := &testClock{now: time.Now()}
	p := newCachingTestProvider(t, srv, clock)
	dir := writeTestCreds(t, "ya29.revoked", clock.Now().Add(time.Hour))
	acct := cachingTestAccount("gemini-d", dir)

	for i := 0; i < 3; i++ {
		if _, err := p.Fetch(context.Background(), acct); err != nil {
			t.Fatalf("Fetch() error: %v", err)
		}
		clock.advance(5 * time.Minute)
	}
	if got := fx.tokenCalls.Load(); got != 1 {
		t.Errorf("token calls = %d, want 1 (refreshed token reused after the 401)", got)
	}
}

func TestRequestAccessToken_ErrorOmitsResponseBody(t *testing.T) {
	fx := &codeAssistFixture{tokenStatus: http.StatusBadRequest}
	srv := fx.server(t)
	_, err := requestAccessToken(context.Background(), "1//secret-refresh", srv.URL+"/token", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "leaked-secret-value") || strings.Contains(msg, "secret-refresh") {
		t.Errorf("error leaks response or token data: %q", msg)
	}
	if !strings.Contains(msg, "invalid_grant") {
		t.Errorf("error = %q, want the OAuth error code", msg)
	}
}

func TestAPICache_BoundedAndConcurrent(t *testing.T) {
	c := newAPICache()
	now := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("k%d-%d", w, i)
				c.storeToken(key, "tok", now.Add(time.Hour))
				c.token(key, now)
				c.storeLoadCodeAssist(key, &loadCodeAssistResponse{}, now)
				c.loadCodeAssist(key, now)
				if i%10 == 0 {
					c.dropToken(key)
				}
			}
		}(w)
	}
	wg.Wait()
	if len(c.tokens) > maxAPICacheEntries || len(c.loads) > maxAPICacheEntries {
		t.Errorf("cache sizes tokens=%d loads=%d exceed bound %d", len(c.tokens), len(c.loads), maxAPICacheEntries)
	}
	if len(c.tokenOrder) != len(c.tokens) || len(c.loadOrder) != len(c.loads) {
		t.Errorf("order/map mismatch: tokens %d/%d loads %d/%d", len(c.tokenOrder), len(c.tokens), len(c.loadOrder), len(c.loads))
	}
}
