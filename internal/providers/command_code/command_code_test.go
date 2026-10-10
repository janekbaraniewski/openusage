package command_code

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const testKeyEnv = "TEST_COMMAND_CODE_API_KEY"

// serverBehavior lets each test flip the response for one route.
type serverBehavior struct {
	unauthorized       bool
	creditsStatus      int // override for /alpha/billing/credits (0 = 200)
	creditsBody        string
	summarySinceStatus int
	limitedWindow      bool
	orgID              string
}

func newTestServer(t *testing.T, b serverBehavior) *httptest.Server {
	t.Helper()
	resetAt := time.Now().Add(3 * time.Hour).UnixMilli()
	weeklyReset := time.Now().Add(72 * time.Hour).UnixMilli()

	creditsBody := fmt.Sprintf(`{
		"credits":{"belowThreshold":false,"creditThreshold":0,"monthlyCredits":59.4,"purchasedCredits":0,"freeCredits":0},
		"windowLimits":{
			"limited":true,
			"fiveHour":{"used":0.12,"cap":14,"exceeded":false,"resetAt":%d},
			"weekly":{"used":10.5,"cap":35,"exceeded":%v,"resetAt":%d}
		},
		"sandboxAccess":false,"sandboxMinutes":null
	}`, resetAt, b.limitedWindow, weeklyReset)
	if b.creditsBody != "" {
		creditsBody = b.creditsBody
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/alpha/whoami", func(w http.ResponseWriter, r *http.Request) {
		if b.unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		org := "null"
		if b.orgID != "" {
			org = fmt.Sprintf(`{"id":%q,"login":"acme","name":"Acme"}`, b.orgID)
		}
		fmt.Fprintf(w, `{"success":true,"user":{"id":"u1","name":"Test User","email":"user@example.com","userName":"test-user"},"org":%s}`, org)
	})
	mux.HandleFunc("/alpha/billing/credits", func(w http.ResponseWriter, r *http.Request) {
		if b.unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if b.orgID != "" && r.URL.Query().Get("orgId") != b.orgID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if b.creditsStatus != 0 {
			w.WriteHeader(b.creditsStatus)
			return
		}
		fmt.Fprint(w, creditsBody)
	})
	mux.HandleFunc("/alpha/billing/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if b.unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"id":"sub_1","status":"active","planId":"individual-goat","currentPeriodStart":"2026-10-01T01:14:01.000Z","currentPeriodEnd":"2026-11-01T01:14:01.000Z"}}`)
	})
	mux.HandleFunc("/alpha/usage/summary", func(w http.ResponseWriter, r *http.Request) {
		if b.unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if since := r.URL.Query().Get("since"); since != "" {
			// The real endpoint only accepts the "Z" RFC3339 form.
			if !strings.HasSuffix(since, "Z") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if b.summarySinceStatus != 0 {
				w.WriteHeader(b.summarySinceStatus)
				return
			}
			fmt.Fprint(w, `{"totalCount":100,"totalCost":1.5,"totalTokens":1000,"totalTokensIn":800,"totalTokensOut":200,"periodBasis":"billing-period"}`)
			return
		}
		fmt.Fprint(w, `{"totalCount":5000,"totalCost":10.5,"totalTokens":100000,"totalTokensIn":90000,"totalTokensOut":10000,"periodBasis":"billing-period"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testAccount(t *testing.T, baseURL, keyEnv string) core.AccountConfig {
	t.Helper()
	acct := core.AccountConfig{
		ID:        "test-command-code",
		Provider:  ID,
		APIKeyEnv: keyEnv,
		BaseURL:   baseURL,
	}
	// Point the auth-file fallback at a path that does not exist so the real
	// ~/.commandcode/auth.json on the developer machine can't leak into tests.
	acct.SetHint("auth_file", filepath.Join(t.TempDir(), "auth.json"))
	return acct
}

func approx(t *testing.T, got *float64, want float64, label string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: nil, want %v", label, want)
	}
	if math.Abs(*got-want) > 1e-6 {
		t.Fatalf("%s = %v, want %v", label, *got, want)
	}
}

func TestFetch_Success(t *testing.T) {
	srv := newTestServer(t, serverBehavior{})
	t.Setenv(testKeyEnv, "test-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK (message=%q)", snap.Status, snap.Message)
	}

	bal, ok := snap.Metrics["credit_balance"]
	if !ok {
		t.Fatal("missing credit_balance")
	}
	approx(t, bal.Remaining, 59.4, "credit_balance.Remaining")
	approx(t, bal.Limit, 70, "credit_balance.Limit")

	five, ok := snap.Metrics["usage_five_hour"]
	if !ok {
		t.Fatal("missing usage_five_hour")
	}
	approx(t, five.Used, 0.12/14*100, "usage_five_hour.Used")
	approx(t, five.Limit, 100, "usage_five_hour.Limit")
	if five.Unit != "%" {
		t.Errorf("usage_five_hour.Unit = %q, want %%", five.Unit)
	}
	if _, ok := snap.Resets["usage_five_hour"]; !ok {
		t.Error("missing usage_five_hour reset")
	}

	week, ok := snap.Metrics["usage_seven_day"]
	if !ok {
		t.Fatal("missing usage_seven_day")
	}
	approx(t, week.Used, 10.5/35*100, "usage_seven_day.Used")

	if got, _ := snap.MetaValue("plan_name"); got != "GOAT" {
		t.Errorf("plan_name = %q, want GOAT", got)
	}
	if got, _ := snap.MetaValue("account_email"); got != "user@example.com" {
		t.Errorf("account_email = %q", got)
	}
	approx(t, snap.Metrics["monthly_spend"].Used, 10.5, "monthly_spend")
	approx(t, snap.Metrics["today_spend"].Used, 1.5, "today_spend")
	approx(t, snap.Metrics["requests"].Used, 5000, "requests")
	approx(t, snap.Metrics["total_tokens"].Used, 100000, "total_tokens")
}

func TestFetch_AuthRequired(t *testing.T) {
	acct := testAccount(t, "http://127.0.0.1:1", "TEST_COMMAND_CODE_MISSING")

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_InvalidKey(t *testing.T) {
	srv := newTestServer(t, serverBehavior{unauthorized: true})
	t.Setenv(testKeyEnv, "bad-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("Status = %v, want AUTH_REQUIRED", snap.Status)
	}
}

func TestFetch_RateLimited(t *testing.T) {
	srv := newTestServer(t, serverBehavior{creditsStatus: http.StatusTooManyRequests})
	t.Setenv(testKeyEnv, "test-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("Status = %v, want LIMITED", snap.Status)
	}
}

func TestFetch_WindowExceeded(t *testing.T) {
	srv := newTestServer(t, serverBehavior{limitedWindow: true})
	t.Setenv(testKeyEnv, "test-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("Status = %v, want LIMITED", snap.Status)
	}
	if !strings.Contains(snap.Message, "weekly") {
		t.Errorf("Message = %q, want weekly limit", snap.Message)
	}
}

func TestFetch_ServerError(t *testing.T) {
	srv := newTestServer(t, serverBehavior{creditsStatus: http.StatusInternalServerError})
	t.Setenv(testKeyEnv, "test-key")

	_, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err == nil {
		t.Fatal("expected error for HTTP 500 credits response")
	}
}

func TestFetch_MalformedCreditsJSON(t *testing.T) {
	srv := newTestServer(t, serverBehavior{creditsBody: `{"credits":`})
	t.Setenv(testKeyEnv, "test-key")

	_, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err == nil {
		t.Fatal("expected error for malformed credits JSON")
	}
}

func TestFetch_TodaySummaryFailureIsNonFatal(t *testing.T) {
	srv := newTestServer(t, serverBehavior{summarySinceStatus: http.StatusInternalServerError})
	t.Setenv(testKeyEnv, "test-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}
	if _, ok := snap.Metrics["today_spend"]; ok {
		t.Error("today_spend should be absent when today's summary fails")
	}
	if _, ok := snap.Metrics["monthly_spend"]; !ok {
		t.Error("monthly_spend should still be present")
	}
	if _, ok := snap.Diagnostics["today_summary_error"]; !ok {
		t.Error("expected today_summary_error diagnostic")
	}
}

func TestFetch_UsesOrgID(t *testing.T) {
	srv := newTestServer(t, serverBehavior{orgID: "org_123"})
	t.Setenv(testKeyEnv, "test-key")

	snap, err := New().Fetch(context.Background(), testAccount(t, srv.URL, testKeyEnv))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}
	if got, _ := snap.MetaValue("organization_name"); got != "Acme" {
		t.Errorf("organization_name = %q, want Acme", got)
	}
}

func TestFetch_AuthFileFallback(t *testing.T) {
	srv := newTestServer(t, serverBehavior{})
	t.Setenv(testKeyEnv, "")

	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authFile, []byte(`{"apiKey":"file-key","userName":"test-user"}`), 0o600); err != nil {
		t.Fatalf("writing auth file: %v", err)
	}

	acct := testAccount(t, srv.URL, testKeyEnv)
	acct.SetHint("auth_file", authFile)

	snap, err := New().Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %v, want OK", snap.Status)
	}
	if got, _ := snap.MetaValue("credential_source"); got != "~/.commandcode/auth.json" {
		t.Errorf("credential_source = %q", got)
	}
}
