package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleClient_QueryBillingInfo_RoundTrip(t *testing.T) {
	billing := loadFixture(t, "seroval_c83b78a61468.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the cookie made it through.
		c, err := r.Cookie("auth")
		if err != nil || c.Value != "test-cookie-value" {
			t.Errorf("auth cookie missing/wrong: %v %v", c, err)
		}
		// Verify the action ID is in the URL/headers.
		if got := r.URL.Query().Get("id"); got != rpcBillingInfoID {
			t.Errorf("id query = %q, want %s", got, rpcBillingInfoID)
		}
		if got := r.Header.Get("x-server-id"); got != rpcBillingInfoID {
			t.Errorf("x-server-id = %q", got)
		}
		// Verify the args payload includes the workspace ID.
		args := r.URL.Query().Get("args")
		if !strings.Contains(args, "wrk_TEST123") {
			t.Errorf("args missing workspace id: %q", args)
		}
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(billing)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "wrk_TEST123")
	c.baseURL = server.URL

	got, err := c.QueryBillingInfo(context.Background())
	if err != nil {
		t.Fatalf("QueryBillingInfo error: %v", err)
	}
	if got.Balance != 0 {
		t.Errorf("Balance = %v, want 0 (fresh acc)", got.Balance)
	}
	if got.ReloadAmount != 20 {
		t.Errorf("ReloadAmount = %v, want 20", got.ReloadAmount)
	}
	if got.ReloadTrigger != 5 {
		t.Errorf("ReloadTrigger = %v, want 5", got.ReloadTrigger)
	}
	if got.HasSubscription {
		t.Error("HasSubscription should be false on this fixture")
	}
}

func TestConsoleClient_AuthError_Surfaces401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("session expired"))
	}))
	defer server.Close()

	c := NewConsoleClient("expired-cookie", "auth", "wrk_X")
	c.baseURL = server.URL

	_, err := c.QueryBillingInfo(context.Background())
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	authErr, ok := err.(*ConsoleAuthError)
	if !ok {
		t.Fatalf("expected *ConsoleAuthError, got %T: %v", err, err)
	}
	if authErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
	}
}

func TestConsoleClient_RequiresWorkspaceID(t *testing.T) {
	c := NewConsoleClient("v", "auth", "")
	if _, err := c.QueryBillingInfo(context.Background()); err == nil {
		t.Error("expected error when workspace id missing")
	}
}

func TestConsoleClient_RequiresCookie(t *testing.T) {
	c := NewConsoleClient("", "auth", "wrk_X")
	if _, err := c.QueryBillingInfo(context.Background()); err == nil {
		t.Error("expected error when cookie missing")
	}
}

func TestConsoleClient_QueryUsageMonth_PostsArgsBody(t *testing.T) {
	body := loadFixture(t, "seroval_15702f3a12ff.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("x-server-id"); got != rpcUsageMonthID {
			t.Errorf("x-server-id = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	c := NewConsoleClient("v", "auth", "wrk_X")
	c.baseURL = server.URL

	got, err := c.QueryUsageMonth(context.Background(), 2026, 4, "+02:00")
	if err != nil {
		t.Fatalf("QueryUsageMonth error: %v", err)
	}
	if len(got.Days) != 2 {
		t.Errorf("Days = %d, want 2", len(got.Days))
	}
	if len(got.Keys) != 2 {
		t.Errorf("Keys = %d, want 2", len(got.Keys))
	}
}

func TestConsoleClient_DiscoverWorkspaceID_FromAuthRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/en/workspace/wrk_DISCOVERED/billing", http.StatusFound)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "")
	c.baseURL = server.URL

	workspaceID, err := c.DiscoverWorkspaceID(context.Background())
	if err != nil {
		t.Fatalf("DiscoverWorkspaceID error: %v", err)
	}
	if workspaceID != "wrk_DISCOVERED" {
		t.Fatalf("workspaceID = %q, want wrk_DISCOVERED", workspaceID)
	}
}

func TestConsoleClient_DiscoverWorkspaceID_MissingRedirectID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/auth/authorize", http.StatusFound)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "")
	c.baseURL = server.URL

	if _, err := c.DiscoverWorkspaceID(context.Background()); err == nil {
		t.Fatal("expected missing workspace redirect error")
	}
}

func TestConsoleClient_FetchWorkspaceIDsViaRPC_RoundTrip(t *testing.T) {
	body := loadFixture(t, "seroval_def39973159c.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-server-id"); got != rpcWorkspacesID {
			t.Errorf("x-server-id = %q, want %s", got, rpcWorkspacesID)
		}
		c, err := r.Cookie("auth")
		if err != nil || c.Value != "test-cookie-value" {
			t.Errorf("auth cookie missing/wrong: %v %v", c, err)
		}
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "")
	c.baseURL = server.URL

	workspaceID, err := c.FetchWorkspaceIDsViaRPC(context.Background())
	if err != nil {
		t.Fatalf("FetchWorkspaceIDsViaRPC error: %v", err)
	}
	if workspaceID != "wrk_TESTWORKSPACE123" {
		t.Errorf("workspaceID = %q, want wrk_TESTWORKSPACE123", workspaceID)
	}
}

func TestConsoleClient_FetchWorkspaceIDsViaRPC_FallbackPOST(t *testing.T) {
	body := loadFixture(t, "seroval_def39973159c.txt")
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call (GET) returns empty.
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`;0x00000010;((self.$R=self.$R||{})["server-fn:1"]=[],($R=>$R[0]={})($R["server-fn:1"]))`))
			return
		}
		// Second call (POST) returns workspace IDs.
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "")
	c.baseURL = server.URL

	workspaceID, err := c.FetchWorkspaceIDsViaRPC(context.Background())
	if err != nil {
		t.Fatalf("FetchWorkspaceIDsViaRPC error: %v", err)
	}
	if workspaceID != "wrk_TESTWORKSPACE123" {
		t.Errorf("workspaceID = %q, want wrk_TESTWORKSPACE123", workspaceID)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (GET + POST fallback)", callCount)
	}
}

func TestConsoleClient_QuerySubscriptionUsage_RoundTrip(t *testing.T) {
	body := loadFixture(t, "seroval_7abeebee372f.txt")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-server-id"); got != rpcSubscriptionID {
			t.Errorf("x-server-id = %q, want %s", got, rpcSubscriptionID)
		}
		args := r.URL.Query().Get("args")
		if !strings.Contains(args, "wrk_TEST123") {
			t.Errorf("args missing workspace id: %q", args)
		}
		if got := r.Header.Get("Referer"); !strings.Contains(got, "wrk_TEST123") {
			t.Errorf("Referer missing workspace id: %q", got)
		}
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	c := NewConsoleClient("test-cookie-value", "auth", "wrk_TEST123")
	c.baseURL = server.URL

	got, err := c.QuerySubscriptionUsage(context.Background(), "wrk_TEST123")
	if err != nil {
		t.Fatalf("QuerySubscriptionUsage error: %v", err)
	}
	if got.RollingUsagePct != 67.5 {
		t.Errorf("RollingUsagePct = %v, want 67.5", got.RollingUsagePct)
	}
	if got.RollingResetSec != 10800 {
		t.Errorf("RollingResetSec = %v, want 10800", got.RollingResetSec)
	}
	if got.WeeklyUsagePct != 42.3 {
		t.Errorf("WeeklyUsagePct = %v, want 42.3", got.WeeklyUsagePct)
	}
	if got.WeeklyResetSec != 259200 {
		t.Errorf("WeeklyResetSec = %v, want 259200", got.WeeklyResetSec)
	}
}

func TestConsoleClient_QuerySubscriptionUsage_RequiresWorkspaceID(t *testing.T) {
	c := NewConsoleClient("v", "auth", "")
	if _, err := c.QuerySubscriptionUsage(context.Background(), ""); err == nil {
		t.Error("expected error when workspace id missing")
	}
}

func TestParseWorkspaceIDsFromBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "single workspace",
			body: `{"workspaces":[{"id":"wrk_abc123","name":"Primary"}]}`,
			want: []string{"wrk_abc123"},
		},
		{
			name: "multiple workspaces",
			body: `id:"wrk_first" id:"wrk_second" id:"wrk_first"`,
			want: []string{"wrk_first", "wrk_second"},
		},
		{
			name: "no workspaces",
			body: `{"error":"unauthorized"}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkspaceIDsFromBody(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseSubscriptionUsageFallback(t *testing.T) {
	text := `rollingUsage:{usagePercent:75.2,resetInSec:5400},weeklyUsage:{usagePercent:30.1,resetInSec:180000}`
	got, err := parseSubscriptionUsageFallback(text)
	if err != nil {
		t.Fatalf("parseSubscriptionUsageFallback error: %v", err)
	}
	if got.RollingUsagePct != 75.2 {
		t.Errorf("RollingUsagePct = %v, want 75.2", got.RollingUsagePct)
	}
	if got.RollingResetSec != 5400 {
		t.Errorf("RollingResetSec = %v, want 5400", got.RollingResetSec)
	}
	if got.WeeklyUsagePct != 30.1 {
		t.Errorf("WeeklyUsagePct = %v, want 30.1", got.WeeklyUsagePct)
	}
	if got.WeeklyResetSec != 180000 {
		t.Errorf("WeeklyResetSec = %v, want 180000", got.WeeklyResetSec)
	}
}

func TestParseGoUsagePageHTML_ZeroUsagePercentIsNotDroppedAsMissing(t *testing.T) {
	html := `<html><script>self.$R=self.$R||[];` +
		`billing.get["wrk_x"]}=$R[1]=$R[2]($R[3]={balance:0,reloadAmount:2000000000,reloadTrigger:500000000});` +
		`rollingUsage:$R[4]={status:"ok",resetInSec:1800,usagePercent:0};` +
		`weeklyUsage:$R[5]={status:"ok",resetInSec:200000,usagePercent:12.5};` +
		`monthlyUsage:$R[6]={status:"ok",resetInSec:900000,usagePercent:0};` +
		`</script></html>`

	subscription, _ := parseGoUsagePageHTML(html)

	if !subscription.RollingUsageOK {
		t.Fatalf("RollingUsageOK = false, want true (usagePercent:0 is a legitimate reading, not a missing field)")
	}
	if subscription.RollingUsagePct != 0 {
		t.Errorf("RollingUsagePct = %v, want 0", subscription.RollingUsagePct)
	}
	if !subscription.WeeklyUsageOK || subscription.WeeklyUsagePct != 12.5 {
		t.Errorf("WeeklyUsageOK/Pct = %v/%v, want true/12.5", subscription.WeeklyUsageOK, subscription.WeeklyUsagePct)
	}
	if !subscription.MonthlyUsageOK {
		t.Fatalf("MonthlyUsageOK = false, want true (usagePercent:0 is a legitimate reading, not a missing field)")
	}
	if subscription.MonthlyUsagePct != 0 {
		t.Errorf("MonthlyUsagePct = %v, want 0", subscription.MonthlyUsagePct)
	}
}

func TestParseGoUsagePageHTML_MissingBlockLeavesUsageOKFalse(t *testing.T) {
	html := `<html><script>self.$R=self.$R||[];` +
		`billing.get["wrk_x"]}=$R[1]=$R[2]($R[3]={balance:0,reloadAmount:0,reloadTrigger:0});` +
		`</script></html>`

	subscription, _ := parseGoUsagePageHTML(html)

	if subscription.RollingUsageOK || subscription.WeeklyUsageOK || subscription.MonthlyUsageOK {
		t.Errorf("expected all *UsageOK flags false when no usage blocks are present, got rolling=%v weekly=%v monthly=%v",
			subscription.RollingUsageOK, subscription.WeeklyUsageOK, subscription.MonthlyUsageOK)
	}
}
