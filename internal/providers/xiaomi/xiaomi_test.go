package xiaomi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// stubSession replaces the browser-session seams so Fetch never touches a
// real cookie store. An empty token means "no session configured".
func stubSession(t *testing.T, token, userID string) {
	t.Helper()
	origLoad, origUser := loadSession, readUserID
	t.Cleanup(func() { loadSession, readUserID = origLoad, origUser })
	loadSession = func(context.Context, core.AccountConfig) (config.BrowserSession, bool, error) {
		if token == "" {
			return config.BrowserSession{}, false, nil
		}
		return config.BrowserSession{
			Domain:        cookieDomain,
			CookieName:    serviceTokenCookie,
			Value:         token,
			SourceBrowser: "firefox",
		}, true, nil
	}
	readUserID = func(context.Context, string) string { return userID }
}

// serveConsole starts a fake console that answers each route with the given
// body and records the Cookie header it received.
func serveConsole(t *testing.T, routes map[string]string, status int) *string {
	t.Helper()
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(baseURLEnvVar, srv.URL)
	t.Setenv(cookieEnvVar, "")
	return &gotCookie
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
	stubSession(t, "tok123", "42")
	gotCookie := serveConsole(t, map[string]string{
		"/balance":          balanceBody,
		"/usage":            usageBody,
		"/tokenPlan/detail": planDetailBody,
		"/tokenPlan/usage":  planUsageBody,
	}, http.StatusOK)

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if want := "api-platform_serviceToken=tok123; userId=42"; *gotCookie != want {
		t.Errorf("Cookie header = %q, want %q", *gotCookie, want)
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
	stubSession(t, "tok123", "42")
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
	gotCookie := serveConsole(t, map[string]string{"/balance": balanceBody, "/usage": usageBody}, http.StatusOK)
	t.Setenv(cookieEnvVar, "Cookie: api-platform_serviceToken=pasted; userId=7")

	snap := fetch(t)

	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (%s), want OK", snap.Status, snap.Message)
	}
	if want := "api-platform_serviceToken=pasted; userId=7"; *gotCookie != want {
		t.Errorf("Cookie header = %q, want %q", *gotCookie, want)
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

func TestFetchExpiredSession(t *testing.T) {
	for name, status := range map[string]int{"login envelope": http.StatusOK, "http 401": http.StatusUnauthorized} {
		t.Run(name, func(t *testing.T) {
			stubSession(t, "stale", "42")
			serveConsole(t, map[string]string{"/balance": loginBody}, status)

			snap := fetch(t)

			if snap.Status != core.StatusAuth {
				t.Fatalf("status = %v (%s), want AUTH", snap.Status, snap.Message)
			}
			if !strings.Contains(snap.Message, "log in again") {
				t.Errorf("message = %q", snap.Message)
			}
		})
	}
}

func TestFetchRateLimited(t *testing.T) {
	stubSession(t, "tok123", "42")
	serveConsole(t, map[string]string{"/balance": `{}`}, http.StatusTooManyRequests)

	snap := fetch(t)

	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %v, want LIMITED", snap.Status)
	}
}

func TestFetchUsageFailureKeepsBalance(t *testing.T) {
	stubSession(t, "tok123", "42")
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
