package xiaomi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	balanceBody    = `{"code":0,"message":"ok","data":{"balance":"12.50","cashBalance":10,"giftBalance":"2.5","currency":"usd"}}`
	usageBody      = `{"code":0,"data":{"costUsage":{"totalCost":"7.25","currentMonthCost":1.5},"tokenUsage":{"totalToken":120000,"inputToken":90000,"outputToken":30000,"cacheToken":40000},"pluginUsage":{"totalRequestCount":3}}}`
	planDetailBody = `{"code":0,"data":{"planName":"","planCode":"standard","expired":false}}`
	planUsageBody  = `{"code":0,"data":{"monthUsage":{"items":[{"name":"month_total_token","used":1000,"limit":4000}]}}}`
	noPlanBody     = `{"code":40401,"message":"no token plan"}`
	loginBody      = `{"code":401,"loginUrl":"https://account.xiaomi.com/pass/serviceLogin"}`
)

// stubSession replaces the browser seams so Fetch never touches a real
// cookie store, and clears minted sessions left by other tests. An empty
// passToken means "no Xiaomi account session in the browser".
func stubSession(t *testing.T, passToken, userID string) {
	t.Helper()
	origLoad, origRead := loadSession, readAccountCookie
	t.Cleanup(func() { loadSession, readAccountCookie = origLoad, origRead })
	loadSession = func(context.Context, core.AccountConfig) (config.BrowserSession, bool, error) {
		if passToken == "" {
			return config.BrowserSession{}, false, nil
		}
		return config.BrowserSession{
			Domain:        accountDomain,
			CookieName:    passTokenCookie,
			Value:         passToken,
			SourceBrowser: "firefox",
		}, true, nil
	}
	readAccountCookie = func(_ context.Context, name, _ string) string {
		if name == userIDCookie {
			return userID
		}
		return ""
	}
	mintState.Lock()
	mintState.sessions = map[string]string{}
	mintState.failedAt = map[string]time.Time{}
	mintState.Unlock()
}

// fakeXiaomi is a console plus an account sign-in host. The console answers
// each route with the given body once a request carries a Cookie header, and
// 401 with a login URL otherwise. The account host accepts the sign-in when
// acceptLogin is set and redirects to the console's /sts, which issues the
// service token.
type fakeXiaomi struct {
	consoleCookie  string
	accountCookie  string
	mints          int
	acceptLogin    bool
	rejectSessions map[string]bool
}

func serveConsole(t *testing.T, routes map[string]string, status int) *fakeXiaomi {
	t.Helper()
	fake := &fakeXiaomi{acceptLogin: true, rejectSessions: map[string]bool{}}

	var consoleURL string
	account := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.accountCookie = r.Header.Get("Cookie")
		if !fake.acceptLogin {
			w.WriteHeader(http.StatusOK) // the interactive login page
			return
		}
		http.Redirect(w, r, consoleURL+"/sts?sign=abc", http.StatusFound)
	}))
	t.Cleanup(account.Close)
	accountURL := strings.Replace(account.URL, "127.0.0.1", "localhost", 1)

	console := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		if r.URL.Path == "/sts" {
			fake.mints++
			w.Header().Add("Set-Cookie", fmt.Sprintf(`api-platform_serviceToken="tok%d"; Path=/; HttpOnly`, fake.mints))
			w.Header().Add("Set-Cookie", "userId=42; Path=/")
			http.Redirect(w, r, "/balance", http.StatusFound)
			return
		}
		if cookie == "" || fake.rejectSessions[cookie] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprintf(w, `{"code":401,"loginUrl":"%s/pass/serviceLogin?sid=api-platform"}`, accountURL)
			return
		}
		fake.consoleCookie = cookie
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(console.Close)
	consoleURL = console.URL

	origHost := isAccountHost
	t.Cleanup(func() { isAccountHost = origHost })
	isAccountHost = func(host string) bool { return host == "localhost" }

	t.Setenv(baseURLEnvVar, console.URL)
	t.Setenv(cookieEnvVar, "")
	return fake
}

func fetch(t *testing.T) core.UsageSnapshot {
	t.Helper()
	snap, err := New().Fetch(context.Background(), core.AccountConfig{ID: "xiaomi", Provider: "xiaomi"})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	return snap
}

func metricValue(t *testing.T, snap core.UsageSnapshot, key string) core.Metric {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok {
		t.Fatalf("metric %q missing; have %v", key, snap.Metrics)
	}
	return m
}

func TestFetchSuccessWithTokenPlan(t *testing.T) {
	stubSession(t, "pass123", "42")
	fake := serveConsole(t, map[string]string{
		"/balance":          balanceBody,
		"/usage":            usageBody,
		"/tokenPlan/detail": planDetailBody,
		"/tokenPlan/usage":  planUsageBody,
	}, http.StatusOK)

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if want := "passToken=pass123; userId=42"; fake.accountCookie != want {
		t.Errorf("account Cookie header = %q, want %q", fake.accountCookie, want)
	}
	if want := `api-platform_serviceToken="tok1"; userId=42`; fake.consoleCookie != want {
		t.Errorf("console Cookie header = %q, want %q", fake.consoleCookie, want)
	}
	if m := metricValue(t, snap, "available_balance"); m.Remaining == nil || *m.Remaining != 12.5 || m.Unit != "USD" {
		t.Errorf("available_balance = %+v", m)
	}
	if m := metricValue(t, snap, "gift_balance"); m.Remaining == nil || *m.Remaining != 2.5 {
		t.Errorf("gift_balance = %+v", m)
	}
	if m := metricValue(t, snap, "total_spend"); m.Used == nil || *m.Used != 7.25 {
		t.Errorf("total_spend = %+v", m)
	}
	if m := metricValue(t, snap, "month_spend"); m.Used == nil || *m.Used != 1.5 {
		t.Errorf("month_spend = %+v", m)
	}
	if m := metricValue(t, snap, "total_tokens"); m.Used == nil || *m.Used != 120000 {
		t.Errorf("total_tokens = %+v", m)
	}
	plan := metricValue(t, snap, "plan_tokens")
	if plan.Used == nil || *plan.Used != 1000 || plan.Limit == nil || *plan.Limit != 4000 || plan.Remaining == nil || *plan.Remaining != 3000 {
		t.Errorf("plan_tokens = %+v", plan)
	}
	if !strings.Contains(snap.Message, "12.50 USD") {
		t.Errorf("message = %q", snap.Message)
	}
}

func TestFetchPayAsYouGoHasNoPlan(t *testing.T) {
	stubSession(t, "pass123", "42")
	serveConsole(t, map[string]string{
		"/balance":          balanceBody,
		"/usage":            usageBody,
		"/tokenPlan/detail": noPlanBody,
	}, http.StatusOK)

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if _, ok := snap.Metrics["plan_tokens"]; ok {
		t.Error("plan_tokens set for an account without a Token Plan")
	}
	metricValue(t, snap, "available_balance")
}

func TestFetchUsesPastedCookieHeader(t *testing.T) {
	stubSession(t, "", "")
	fake := serveConsole(t, map[string]string{"/balance": balanceBody, "/usage": usageBody}, http.StatusOK)
	t.Setenv(cookieEnvVar, "Cookie: api-platform_serviceToken=pasted; userId=7")

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if want := "api-platform_serviceToken=pasted; userId=7"; fake.consoleCookie != want {
		t.Errorf("Cookie header = %q, want %q", fake.consoleCookie, want)
	}
	if fake.mints != 0 {
		t.Errorf("minted %d sessions with a pasted cookie, want 0", fake.mints)
	}
}

func TestFetchNoSession(t *testing.T) {
	stubSession(t, "", "")
	t.Setenv(cookieEnvVar, "")

	snap := fetch(t)

	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %v, want AUTH", snap.Status)
	}
}

func TestFetchReusesMintedSession(t *testing.T) {
	stubSession(t, "pass123", "42")
	fake := serveConsole(t, map[string]string{"/balance": balanceBody, "/usage": usageBody}, http.StatusOK)

	fetch(t)
	fetch(t)

	if fake.mints != 1 {
		t.Errorf("minted %d sessions over two polls, want 1", fake.mints)
	}
}

func TestFetchRemintsExpiredConsoleSession(t *testing.T) {
	stubSession(t, "pass123", "42")
	fake := serveConsole(t, map[string]string{"/balance": balanceBody, "/usage": usageBody}, http.StatusOK)

	fetch(t)
	fake.rejectSessions[fake.consoleCookie] = true
	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if fake.mints != 2 {
		t.Errorf("minted %d sessions, want 2", fake.mints)
	}
}

func TestFetchAccountSessionRejected(t *testing.T) {
	stubSession(t, "stale", "42")
	fake := serveConsole(t, map[string]string{"/balance": balanceBody}, http.StatusOK)
	fake.acceptLogin = false

	snap := fetch(t)
	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %v (%s), want AUTH", snap.Status, snap.Message)
	}
	if !strings.Contains(snap.Message, "sign in again") {
		t.Errorf("message = %q", snap.Message)
	}

	// A failed sign-in is not retried on the next poll.
	fake.accountCookie = ""
	fetch(t)
	if fake.accountCookie != "" {
		t.Error("sign-in retried inside the retry interval")
	}
}

func TestMintRejectsForeignRedirect(t *testing.T) {
	stubSession(t, "pass123", "42")
	serveConsole(t, nil, http.StatusOK)
	isAccountHost = func(string) bool { return false }

	if _, err := mintConsoleSession(context.Background(), "passToken=pass123"); err == nil {
		t.Fatal("mint followed a login URL on a host that is neither Xiaomi's nor the console's")
	}
}

func TestFetchRateLimited(t *testing.T) {
	stubSession(t, "pass123", "42")
	serveConsole(t, map[string]string{"/balance": `{}`}, http.StatusTooManyRequests)

	snap := fetch(t)

	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %v, want LIMITED", snap.Status)
	}
}

func TestFetchUsageFailureKeepsBalance(t *testing.T) {
	stubSession(t, "pass123", "42")
	serveConsole(t, map[string]string{"/balance": balanceBody}, http.StatusOK)

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	metricValue(t, snap, "available_balance")
	if snap.Raw["console_usage_error"] == "" {
		t.Error("usage failure not recorded in Raw")
	}
}
