package cursor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func startNestedPlanUsageServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-jwt" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Connect-Protocol-Version") != "1" {
			http.Error(w, "missing connect version", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
			_, _ = w.Write([]byte(`{
				"billingCycleEnd": "2026-09-27T00:00:00Z",
				"planUsage": {
					"autoPercentUsed": 6,
					"apiPercentUsed": 29,
					"totalPercentUsed": 7
				}
			}`))
		case "/aiserver.v1.DashboardService/GetPlanInfo":
			_, _ = w.Write([]byte(`{"planName":"Pro","billingCycleEnd":"2026-09-27T00:00:00Z"}`))
		case "/aiserver.v1.DashboardService/GetHardLimit":
			_, _ = w.Write([]byte(`{"noUsageBasedAllowed":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestClient_FetchLivePlanUsage_NestedPlanUsage(t *testing.T) {
	ts := startNestedPlanUsageServer(t)
	defer ts.Close()

	live, err := NewClient(ts.URL, ts.Client()).fetchLivePlanUsage(context.Background(), "test-jwt")
	if err != nil {
		t.Fatalf("fetchLivePlanUsage: %v", err)
	}
	if live.Included == nil || *live.Included != 7 {
		t.Fatalf("Included = %v, want 7", live.Included)
	}
	if live.Auto == nil || *live.Auto != 6 {
		t.Fatalf("Auto = %v, want 6", live.Auto)
	}
	if live.API == nil || *live.API != 29 {
		t.Fatalf("API = %v, want 29", live.API)
	}
	if live.PlanName != "Pro" {
		t.Fatalf("PlanName = %q, want Pro", live.PlanName)
	}
	if live.Ondemand != "disabled" {
		t.Fatalf("Ondemand = %q, want disabled", live.Ondemand)
	}
	wantReset := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	if !live.ResetAt.Equal(wantReset) {
		t.Fatalf("ResetAt = %v, want %v", live.ResetAt, wantReset)
	}
}

func TestParseFlexibleTime(t *testing.T) {
	rfc := parseFlexibleTime([]byte(`"2026-09-27T00:00:00Z"`))
	if !rfc.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rfc = %v", rfc)
	}
	ms := parseFlexibleTime([]byte(`1758931200000`))
	if ms.IsZero() {
		t.Fatal("unix ms parsed to zero")
	}
	strMs := parseFlexibleTime([]byte(`"1790685824000"`))
	want := time.UnixMilli(1790685824000).UTC()
	if !strMs.Equal(want) {
		t.Fatalf("string millis = %v, want %v", strMs, want)
	}
}

func TestPlanBucketHitMonthlyLimit(t *testing.T) {
	pct := func(v float64) *float64 { return &v }
	if planBucketHitMonthlyLimit(livePlanUsage{Auto: pct(99.9), API: pct(12)}) {
		t.Fatal("99.9% auto must not count as monthly hit")
	}
	if !planBucketHitMonthlyLimit(livePlanUsage{Auto: pct(100), API: pct(12)}) {
		t.Fatal("100% auto must count as monthly hit")
	}
	if !planBucketHitMonthlyLimit(livePlanUsage{Auto: pct(10), API: pct(100)}) {
		t.Fatal("100% api must count as monthly hit")
	}
}
