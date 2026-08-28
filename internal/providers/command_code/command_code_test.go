package command_code

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	if bal, ok := snap.Metrics["balance"]; !ok || bal.Remaining == nil || *bal.Remaining != 34.15 {
		t.Errorf("balance = %v, want 34.15", bal)
	}

	if wu, ok := snap.Metrics["weekly_usage"]; !ok || wu.Used == nil || *wu.Used < 28.5 || *wu.Used > 28.6 {
		t.Errorf("weekly_usage used = %v, want ~28.57%%", wu.Used)
	}
}
