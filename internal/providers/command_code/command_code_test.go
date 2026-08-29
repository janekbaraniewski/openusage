package command_code

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCommandCode_Fetch_Success(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/alpha/billing/credits", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"credits": {
				"monthlyCredits": 34.15,
				"purchasedCredits": 0,
				"freeCredits": 0
			},
			"windowLimits": {
				"limited": false,
				"exceeded": "",
				"fiveHour": {
					"used": 2.0,
					"cap": 14.0,
					"exceeded": false,
					"resetAt": 1788053390413
				},
				"weekly": {
					"used": 10.0,
					"cap": 35.0,
					"exceeded": false,
					"resetAt": 1788053390413
				}
			}
		}`))
	})

	mux.HandleFunc("/alpha/billing/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"success": true,
			"data": {
				"id": "sub_123",
				"planId": "individual-goat",
				"status": "active",
				"currentPeriodStart": "2026-08-23T00:00:00Z",
				"currentPeriodEnd": "2026-09-23T00:00:00Z"
			}
		}`))
	})

	mux.HandleFunc("/alpha/whoami", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"success": true,
			"user": {
				"name": "Mohammed Nurul Islam",
				"userName": "nurulislamz",
				"email": "mohammed19.islam@gmail.com"
			}
		}`))
	})

	mux.HandleFunc("/alpha/usage/summary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"totalCost": 35.84,
			"totalTokens": 446918849,
			"totalCount": 2975
		}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "command_code",
		Provider: "command_code",
		APIKey:   "test-key",
		BaseURL:  srv.URL,
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Errorf("status = %v, want OK", snap.Status)
	}

	if snap.Attributes["plan_id"] != "individual-goat" {
		t.Errorf("plan = %q, want individual-goat", snap.Attributes["plan_id"])
	}
	if snap.Attributes["plan_name"] != "GOAT" {
		t.Errorf("plan_name = %q, want GOAT", snap.Attributes["plan_name"])
	}

	if bal, ok := snap.Metrics["balance"]; !ok || bal.Remaining == nil || *bal.Remaining != 34.15 {
		t.Errorf("balance = %v, want 34.15", bal)
	}

	if wu, ok := snap.Metrics["weekly_usage"]; !ok || wu.Used == nil || *wu.Used < 28.5 || *wu.Used > 28.6 {
		t.Errorf("weekly_usage used = %v, want ~28.57%%", wu.Used)
	}

	if sub, ok := snap.Metrics["monthly_subscription"]; !ok || sub.Remaining == nil || *sub.Remaining < 48.0 || *sub.Remaining > 49.0 {
		t.Errorf("monthly_subscription = %v, want ~48.79%%", sub)
	}

	if mc, ok := snap.Metrics["monthly_credits"]; !ok || mc.Limit == nil || *mc.Limit != 70.0 {
		t.Errorf("monthly_credits limit = %v, want 70.0", mc.Limit)
	}

	if snap.Attributes["monthly_cap"] != "$70.00" {
		t.Errorf("monthly_cap = %q, want $70.00", snap.Attributes["monthly_cap"])
	}

	if _, ok := snap.Resets["monthly_subscription"]; !ok {
		t.Errorf("expected monthly_subscription reset time")
	}
}

func TestCommandCode_LiveFetch(t *testing.T) {
	key := os.Getenv("COMMAND_CODE_API_KEY")
	if key == "" {
		t.Skip("skipping live test; COMMAND_CODE_API_KEY not set")
	}
	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:        "command_code",
		Provider:  "command_code",
		APIKeyEnv: "COMMAND_CODE_API_KEY",
		Token:     key,
	})
	if err != nil {
		t.Fatalf("Fetch() failed: %v", err)
	}
	if snap.Status == core.StatusAuth {
		t.Fatalf("Fetch() returned StatusAuth: %s", snap.Message)
	}
	t.Logf("Live status: %s", snap.Status)
	t.Logf("Plan: %s (%s)", snap.Attributes["plan_name"], snap.Attributes["plan_id"])
	t.Logf("Monthly Cap: %s, Used: %s, Remaining: %s", snap.Attributes["monthly_cap"], snap.Attributes["monthly_used"], snap.Attributes["monthly_remaining"])
	if sub, ok := snap.Metrics["monthly_subscription"]; ok {
		t.Logf("Monthly Subscription Metric: Used=%.1f%%, Remaining=%.1f%%", *sub.Used, *sub.Remaining)
	} else {
		t.Errorf("missing monthly_subscription metric")
	}
	if reset, ok := snap.Resets["monthly_subscription"]; ok {
		t.Logf("Monthly Reset: %v", reset)
	} else {
		t.Errorf("missing monthly_subscription reset time")
	}
}
