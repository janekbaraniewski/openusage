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

