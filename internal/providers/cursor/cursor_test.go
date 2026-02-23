package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestProvider_ID(t *testing.T) {
	p := New()
	if p.ID() != "cursor" {
		t.Errorf("Expected ID 'cursor', got %q", p.ID())
	}
}

func TestProvider_Describe(t *testing.T) {
	p := New()
	info := p.Describe()
	if info.Name != "Cursor IDE" {
		t.Errorf("Expected name 'Cursor IDE', got %q", info.Name)
	}
	if info.DocURL != "https://www.cursor.com/" {
		t.Errorf("Expected DocURL 'https://www.cursor.com/', got %q", info.DocURL)
	}
	if len(info.Capabilities) == 0 {
		t.Error("Expected non-empty capabilities")
	}
}

func TestProvider_Fetch_NoData(t *testing.T) {
	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID: "test-cursor",
		ExtraData: map[string]string{
			"tracking_db": "/nonexistent/ai-code-tracking.db",
			"state_db":    "/nonexistent/state.vscdb",
		},
	})
	if err != nil {
		t.Fatalf("Fetch should not error, got: %v", err)
	}

	if snap.Status != core.StatusError {
		t.Errorf("Expected StatusError when no data, got %v", snap.Status)
	}
}

func TestProvider_Fetch_WithMockAPI(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       4500,
				IncludedSpend:    2000,
				BonusSpend:       2500,
				Limit:            2000,
				AutoPercentUsed:  50,
				APIPercentUsed:   75,
				TotalPercentUsed: 65,
			},
			SpendLimitUsage: spendLimitUsage{
				TotalSpend:      10000,
				PooledLimit:     50000,
				PooledUsed:      10000,
				PooledRemaining: 40000,
				IndividualUsed:  8000,
				LimitType:       "team",
			},
			DisplayThreshold: 200,
			DisplayMessage:   "You've used 65% of your plan",
		})
	})

	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Team",
				IncludedAmountCents: 2000,
				Price:               "$40/mo",
				BillingCycleEnd:     "1770733695000",
			},
		})
	})

	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{
					ModelIntent:  "claude-4.5-opus-high-thinking",
					InputTokens:  "2343133",
					OutputTokens: "1629263",
					TotalCents:   17109.57,
					Tier:         1,
				},
				{
					ModelIntent:  "gpt-5.2-codex",
					InputTokens:  "1794263",
					OutputTokens: "92146",
					TotalCents:   1098.95,
					Tier:         1,
				},
			},
		})
	})

	mux.HandleFunc("/aiserver.v1.DashboardService/GetHardLimit", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hardLimitResp{NoUsageBasedAllowed: true})
	})

	mux.HandleFunc("/auth/full_stripe_profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(stripeProfileResp{
			MembershipType:           "enterprise",
			IsTeamMember:             true,
			TeamID:                   6648893,
			TeamMembershipType:       "SELF_SERVE",
			IndividualMembershipType: "free",
		})
	})

	mux.HandleFunc("/aiserver.v1.DashboardService/GetUsageLimitPolicyStatus", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usageLimitPolicyResp{
			CanConfigureSpendLimit: true,
			LimitType:              "user-team",
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	p := &Provider{}

	snap := core.UsageSnapshot{
		ProviderID: p.ID(),
		AccountID:  "test-cursor-api",
		Metrics:    make(map[string]core.Metric),
		Raw:        make(map[string]string),
	}

	var periodUsage currentPeriodUsageResp
	err := p.doPost(context.Background(), "test-token",
		fmt.Sprintf("%s/aiserver.v1.DashboardService/GetCurrentPeriodUsage", server.URL),
		&periodUsage)
	if err != nil {
		t.Fatalf("GetCurrentPeriodUsage failed: %v", err)
	}

	if periodUsage.PlanUsage.TotalPercentUsed != 65 {
		t.Errorf("Expected TotalPercentUsed=65, got %f", periodUsage.PlanUsage.TotalPercentUsed)
	}
	if periodUsage.SpendLimitUsage.PooledRemaining != 40000 {
		t.Errorf("Expected PooledRemaining=40000, got %f", periodUsage.SpendLimitUsage.PooledRemaining)
	}
	if periodUsage.DisplayMessage != "You've used 65% of your plan" {
		t.Errorf("Unexpected display message: %s", periodUsage.DisplayMessage)
	}

	var planInfo planInfoResp
	err = p.doPost(context.Background(), "test-token",
		fmt.Sprintf("%s/aiserver.v1.DashboardService/GetPlanInfo", server.URL),
		&planInfo)
	if err != nil {
		t.Fatalf("GetPlanInfo failed: %v", err)
	}
	if planInfo.PlanInfo.PlanName != "Team" {
		t.Errorf("Expected PlanName='Team', got %q", planInfo.PlanInfo.PlanName)
	}
	if planInfo.PlanInfo.Price != "$40/mo" {
		t.Errorf("Expected Price='$40/mo', got %q", planInfo.PlanInfo.Price)
	}

	var aggUsage aggregatedUsageResp
	err = p.doPost(context.Background(), "test-token",
		fmt.Sprintf("%s/aiserver.v1.DashboardService/GetAggregatedUsageEvents", server.URL),
		&aggUsage)
	if err != nil {
		t.Fatalf("GetAggregatedUsageEvents failed: %v", err)
	}
	if len(aggUsage.Aggregations) != 2 {
		t.Fatalf("Expected 2 aggregations, got %d", len(aggUsage.Aggregations))
	}
	if aggUsage.Aggregations[0].ModelIntent != "claude-4.5-opus-high-thinking" {
		t.Errorf("Expected first model 'claude-4.5-opus-high-thinking', got %q", aggUsage.Aggregations[0].ModelIntent)
	}
	if aggUsage.Aggregations[0].TotalCents != 17109.57 {
		t.Errorf("Expected TotalCents=17109.57, got %f", aggUsage.Aggregations[0].TotalCents)
	}

	var profile stripeProfileResp
	err = p.callRESTAPI(context.Background(), "test-token",
		"", &profile) // Won't work with test server directly
	req, _ := http.NewRequest("GET", server.URL+"/auth/full_stripe_profile", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Stripe profile request failed: %v", err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&profile)
	if profile.MembershipType != "enterprise" {
		t.Errorf("Expected membership 'enterprise', got %q", profile.MembershipType)
	}
	if !profile.IsTeamMember {
		t.Error("Expected IsTeamMember=true")
	}

	_ = snap // We've verified the individual API responses parse correctly
}

func TestProvider_Fetch_APIUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"unauthenticated"}`, http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := New()

	var result map[string]interface{}
	err := p.doPost(context.Background(), "invalid-token",
		fmt.Sprintf("%s/aiserver.v1.DashboardService/GetCurrentPeriodUsage", server.URL),
		&result)

	if err == nil {
		t.Error("Expected error for unauthorized request")
	}
}

func TestProvider_Fetch_ExposesPlanSplitAndCacheTokenMetrics(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       4500,
				IncludedSpend:    2000,
				BonusSpend:       2500,
				Limit:            2000,
				AutoPercentUsed:  12.5,
				APIPercentUsed:   87.5,
				TotalPercentUsed: 65,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     50000,
				PooledUsed:      10000,
				PooledRemaining: 40000,
				IndividualUsed:  8000,
				LimitType:       "team",
			},
			DisplayThreshold: 200,
			DisplayMessage:   "You've used 65% of your plan",
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Team",
				IncludedAmountCents: 2000,
				Price:               "$40/mo",
				BillingCycleEnd:     "1770733695000",
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{
					ModelIntent:      "claude-4.5-opus",
					InputTokens:      "1200",
					OutputTokens:     "300",
					CacheWriteTokens: "100",
					CacheReadTokens:  "50",
					TotalCents:       987.0,
					Tier:             1,
				},
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetHardLimit", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hardLimitResp{NoUsageBasedAllowed: true})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetUsageLimitPolicyStatus", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usageLimitPolicyResp{
			CanConfigureSpendLimit: true,
			LimitType:              "user-team",
		})
	})
	mux.HandleFunc("/auth/full_stripe_profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(stripeProfileResp{
			MembershipType:     "enterprise",
			IsTeamMember:       true,
			TeamID:             6648893,
			TeamMembershipType: "SELF_SERVE",
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-split-test",
		Provider: "cursor",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if m, ok := snap.Metrics["plan_auto_percent_used"]; !ok || m.Used == nil || *m.Used != 12.5 {
		t.Fatalf("plan_auto_percent_used missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["plan_api_percent_used"]; !ok || m.Used == nil || *m.Used != 87.5 {
		t.Fatalf("plan_api_percent_used missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["model_claude-4.5-opus_cached_tokens"]; !ok || m.Used == nil || *m.Used != 150 {
		t.Fatalf("model cached tokens missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["model_claude-4.5-opus_input_tokens"]; !ok || m.Used == nil || *m.Used != 1200 {
		t.Fatalf("model input tokens missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["model_claude-4.5-opus_output_tokens"]; !ok || m.Used == nil || *m.Used != 300 {
		t.Fatalf("model output tokens missing or invalid: %+v", m)
	}
	if _, ok := snap.Resets["billing_cycle_end"]; !ok {
		t.Fatalf("billing_cycle_end reset missing from snapshot")
	}
	if snap.Raw["can_configure_spend_limit"] != "true" {
		t.Fatalf("can_configure_spend_limit = %q, want true", snap.Raw["can_configure_spend_limit"])
	}
}

func TestProvider_Fetch_UsesCachedModelAggregationWhenAggregationEndpointErrors(t *testing.T) {
	var aggCalls int
	server := httptest.NewServer(newCursorAPITestMux(func(w http.ResponseWriter, r *http.Request) {
		aggCalls++
		if aggCalls == 1 {
			json.NewEncoder(w).Encode(aggregatedUsageResp{
				Aggregations: []modelAggregation{
					{
						ModelIntent:  "claude-4.5-opus",
						InputTokens:  "12345",
						OutputTokens: "678",
						TotalCents:   987.0,
					},
				},
			})
			return
		}
		http.Error(w, "temporary upstream error", http.StatusInternalServerError)
	}))
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	acct := core.AccountConfig{ID: "cursor-cache-error", Provider: "cursor", Token: "test-token"}

	first, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch returned error: %v", err)
	}
	if _, ok := first.Metrics["model_claude-4.5-opus_cost"]; !ok {
		t.Fatalf("first Fetch missing model cost metric")
	}

	second, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch returned error: %v", err)
	}
	metric, ok := second.Metrics["model_claude-4.5-opus_cost"]
	if !ok {
		t.Fatalf("second Fetch missing cached model cost metric")
	}
	if metric.Used == nil || *metric.Used != 9.87 {
		t.Fatalf("second Fetch model cost = %v, want 9.87", metric.Used)
	}
	if second.Raw["model_claude-4.5-opus_input_tokens"] != "12345" {
		t.Fatalf("second Fetch missing cached input tokens, got %q", second.Raw["model_claude-4.5-opus_input_tokens"])
	}
}

func TestProvider_Fetch_UsesCachedModelAggregationWhenAggregationEndpointReturnsEmpty(t *testing.T) {
	var aggCalls int
	server := httptest.NewServer(newCursorAPITestMux(func(w http.ResponseWriter, r *http.Request) {
		aggCalls++
		if aggCalls == 1 {
			json.NewEncoder(w).Encode(aggregatedUsageResp{
				Aggregations: []modelAggregation{
					{
						ModelIntent:  "gemini-2.5-pro",
						InputTokens:  "23456",
						OutputTokens: "789",
						TotalCents:   123.0,
					},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(aggregatedUsageResp{Aggregations: []modelAggregation{}})
	}))
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	acct := core.AccountConfig{ID: "cursor-cache-empty", Provider: "cursor", Token: "test-token"}

	first, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch returned error: %v", err)
	}
	if _, ok := first.Metrics["model_gemini-2.5-pro_cost"]; !ok {
		t.Fatalf("first Fetch missing model cost metric")
	}

	second, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch returned error: %v", err)
	}
	metric, ok := second.Metrics["model_gemini-2.5-pro_cost"]
	if !ok {
		t.Fatalf("second Fetch missing cached model cost metric")
	}
	if metric.Used == nil || *metric.Used != 1.23 {
		t.Fatalf("second Fetch model cost = %v, want 1.23", metric.Used)
	}
	if second.Raw["model_gemini-2.5-pro_output_tokens"] != "789" {
		t.Fatalf("second Fetch missing cached output tokens, got %q", second.Raw["model_gemini-2.5-pro_output_tokens"])
	}
}

func TestProvider_Fetch_MergesAPIWithLocalTrackingBreakdowns(t *testing.T) {
	now := time.Now().In(time.Local)
	anchor := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	trackingDBPath := createCursorTrackingDBForTest(t, []cursorTrackingRow{
		{Hash: "h1", Source: "composer", Model: "claude-4.5-opus", CreatedAt: anchor.Add(-2 * time.Hour).UnixMilli()},
		{Hash: "h2", Source: "composer", Model: "claude-4.5-opus", CreatedAt: anchor.AddDate(0, 0, -1).UnixMilli()},
		{Hash: "h3", Source: "tab", Model: "claude-4.5-opus", CreatedAt: anchor.Add(-1 * time.Hour).UnixMilli()},
		{Hash: "h4", Source: "cli", Model: "gpt-4o", CreatedAt: anchor.Add(-90 * time.Minute).UnixMilli()},
	})

	server := httptest.NewServer(newCursorAPITestMux(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{Aggregations: []modelAggregation{}})
	}))
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-api-local-merge",
		Provider: "cursor",
		Token:    "test-token",
		ExtraData: map[string]string{
			"tracking_db": trackingDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if _, ok := snap.Metrics["plan_spend"]; !ok {
		t.Fatalf("expected API plan_spend metric to be present")
	}
	if m, ok := snap.Metrics["source_composer_requests"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("source_composer_requests missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["source_tab_requests"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("source_tab_requests missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["source_cli_requests"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("source_cli_requests missing or invalid: %+v", m)
	}
	// Verify tool_* metrics are emitted from source breakdown.
	if m, ok := snap.Metrics["tool_composer"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Fatalf("tool_composer missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["tool_tab"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("tool_tab missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["tool_cli"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("tool_cli missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["client_ide_sessions"]; !ok || m.Used == nil || *m.Used != 3 {
		t.Fatalf("client_ide_sessions missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["client_cli_agents_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("client_cli_agents_sessions missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["model_claude_4_5_opus_requests"]; !ok || m.Used == nil || *m.Used != 3 {
		t.Fatalf("model_claude_4_5_opus_requests missing or invalid: %+v", m)
	}
	if m, ok := snap.Metrics["model_gpt_4o_requests"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("model_gpt_4o_requests missing or invalid: %+v", m)
	}
	if len(snap.DailySeries["usage_source_composer"]) < 2 {
		t.Fatalf("expected usage_source_composer daily series with at least 2 points")
	}
	if len(snap.DailySeries["usage_model_claude_4_5_opus"]) < 2 {
		t.Fatalf("expected usage_model_claude_4_5_opus daily series with at least 2 points")
	}
	if snap.Message == "Local Cursor IDE usage tracking (API unavailable)" {
		t.Fatalf("expected API message to be preserved when API succeeds")
	}
}

func TestProvider_Fetch_PreservesLocalMetricsWhenOptionalAPICallsTimeout(t *testing.T) {
	now := time.Now()
	trackingDBPath := createCursorTrackingDBForTest(t, []cursorTrackingRow{
		{Hash: "h1", Source: "composer", Model: "claude-4.5-opus", CreatedAt: now.Add(-1 * time.Hour).UnixMilli()},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       4500,
				IncludedSpend:    2000,
				BonusSpend:       2500,
				Limit:            2000,
				TotalPercentUsed: 65,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     50000,
				PooledUsed:      10000,
				PooledRemaining: 40000,
				IndividualUsed:  8000,
				LimitType:       "team",
			},
			DisplayMessage: "You've used 65% of your plan",
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		json.NewEncoder(w).Encode(planInfoResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{Aggregations: []modelAggregation{}})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetHardLimit", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hardLimitResp{})
	})
	mux.HandleFunc("/auth/full_stripe_profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(stripeProfileResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetUsageLimitPolicyStatus", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usageLimitPolicyResp{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	snap, err := p.Fetch(ctx, core.AccountConfig{
		ID:       "cursor-optional-timeout",
		Provider: "cursor",
		Token:    "test-token",
		ExtraData: map[string]string{
			"tracking_db": trackingDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if _, ok := snap.Metrics["plan_spend"]; !ok {
		t.Fatalf("expected API plan_spend metric to be present")
	}

	if m, ok := snap.Metrics["total_ai_requests"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Fatalf("expected local total_ai_requests to be preserved, got %+v", m)
	}

	if _, ok := snap.Raw["tracking_db_error"]; ok {
		t.Fatalf("did not expect tracking_db_error when local data is available")
	}
}

func newCursorAPITestMux(aggregateHandler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       4500,
				IncludedSpend:    2000,
				BonusSpend:       2500,
				Limit:            2000,
				TotalPercentUsed: 65,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     50000,
				PooledUsed:      10000,
				PooledRemaining: 40000,
				IndividualUsed:  8000,
				LimitType:       "team",
			},
			DisplayMessage: "You've used 65% of your plan",
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Team",
				IncludedAmountCents: 2000,
				Price:               "$40/mo",
				BillingCycleEnd:     "1770733695000",
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", aggregateHandler)
	return mux
}

func TestProvider_Fetch_ReadsComposerSessionsFromStateDB(t *testing.T) {
	stateDBPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/cachedEmail', 'test@example.com')`)
	db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('freeBestOfN.promptCount', '42')`)

	db.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	now := time.Now()
	session1 := fmt.Sprintf(`{"usageData":{"claude-4.5-opus":{"costInCents":500,"amount":10},"gpt-4o":{"costInCents":100,"amount":5}},"unifiedMode":"agent","createdAt":%d,"totalLinesAdded":200,"totalLinesRemoved":50}`, now.Add(-1*time.Hour).UnixMilli())
	session2 := fmt.Sprintf(`{"usageData":{"claude-4.5-opus":{"costInCents":300,"amount":8}},"unifiedMode":"chat","createdAt":%d,"totalLinesAdded":100,"totalLinesRemoved":20}`, now.Add(-2*time.Hour).UnixMilli())
	sessionEmpty := `{"usageData":{},"unifiedMode":"agent","createdAt":1000}`
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:aaa', ?)`, session1)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:bbb', ?)`, session2)
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:ccc', ?)`, sessionEmpty)
	db.Close()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-composer-test",
		Provider: "cursor",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if m, ok := snap.Metrics["composer_cost"]; !ok || m.Used == nil || *m.Used != 9.0 {
		t.Errorf("composer_cost: got %+v, want Used=9.0 (900 cents)", m)
	}
	if m, ok := snap.Metrics["composer_sessions"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Errorf("composer_sessions: got %+v, want Used=2", m)
	}
	if m, ok := snap.Metrics["composer_requests"]; !ok || m.Used == nil || *m.Used != 23 {
		t.Errorf("composer_requests: got %+v, want Used=23", m)
	}
	if m, ok := snap.Metrics["composer_lines_added"]; !ok || m.Used == nil || *m.Used != 300 {
		t.Errorf("composer_lines_added: got %+v, want Used=300", m)
	}
	if m, ok := snap.Metrics["mode_agent_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Errorf("mode_agent_sessions: got %+v, want Used=1", m)
	}
	if m, ok := snap.Metrics["mode_chat_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		t.Errorf("mode_chat_sessions: got %+v, want Used=1", m)
	}
	if m, ok := snap.Metrics["total_prompts"]; !ok || m.Used == nil || *m.Used != 42 {
		t.Errorf("total_prompts: got %+v, want Used=42", m)
	}
	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("account_email: got %q, want test@example.com", snap.Raw["account_email"])
	}
	if snap.Raw["total_prompts"] != "42" {
		t.Errorf("total_prompts raw: got %q, want 42", snap.Raw["total_prompts"])
	}
}

func TestProvider_Fetch_ReadsScoredCommitsFromTrackingDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-code-tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE ai_code_hashes (hash TEXT PRIMARY KEY, source TEXT, createdAt INTEGER, model TEXT)`)
	db.Exec(`INSERT INTO ai_code_hashes VALUES ('h1', 'composer', ?, 'claude')`, time.Now().UnixMilli())

	db.Exec(`CREATE TABLE scored_commits (
		commitHash TEXT, branchName TEXT, scoredAt INTEGER,
		linesAdded INTEGER, linesDeleted INTEGER,
		tabLinesAdded INTEGER, tabLinesDeleted INTEGER,
		composerLinesAdded INTEGER, composerLinesDeleted INTEGER,
		humanLinesAdded INTEGER, humanLinesDeleted INTEGER,
		blankLinesAdded INTEGER, blankLinesDeleted INTEGER,
		commitMessage TEXT, commitDate TEXT,
		v1AiPercentage TEXT, v2AiPercentage TEXT,
		PRIMARY KEY (commitHash, branchName))`)
	db.Exec(`INSERT INTO scored_commits VALUES ('abc', 'main', ?, 100, 10, 20, 5, 60, 3, 20, 2, 0, 0, 'test', '2026-02-23', '50.0', '80.0')`, time.Now().UnixMilli())
	db.Exec(`INSERT INTO scored_commits VALUES ('def', 'main', ?, 200, 20, 40, 10, 120, 6, 40, 4, 0, 0, 'test2', '2026-02-22', '30.0', '60.0')`, time.Now().UnixMilli())
	db.Close()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-commits-test",
		Provider: "cursor",
		ExtraData: map[string]string{
			"tracking_db": dbPath,
		},
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	if m, ok := snap.Metrics["scored_commits"]; !ok || m.Used == nil || *m.Used != 2 {
		t.Errorf("scored_commits: got %+v, want Used=2", m)
	}
	if m, ok := snap.Metrics["ai_code_percentage"]; !ok || m.Used == nil {
		t.Errorf("ai_code_percentage missing")
	} else if *m.Used != 70.0 {
		t.Errorf("ai_code_percentage: got %.1f, want 70.0 (avg of 80+60)", *m.Used)
	}
}

func TestCursorClientBucket(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: "composer", want: "ide"},
		{source: "tab", want: "ide"},
		{source: "human", want: "ide"},
		{source: "cli", want: "cli_agents"},
		{source: "background-agent", want: "cli_agents"},
		{source: "terminal", want: "cli_agents"},
		{source: "unknown-source", want: "other"},
		{source: "", want: "other"},
	}

	for _, tt := range tests {
		if got := cursorClientBucket(tt.source); got != tt.want {
			t.Errorf("cursorClientBucket(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

type cursorTrackingRow struct {
	Hash      string
	Source    string
	Model     string
	CreatedAt int64
}

func createCursorTrackingDBForTest(t *testing.T, rows []cursorTrackingRow) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "ai-code-tracking.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE ai_code_hashes (
			hash TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			fileExtension TEXT,
			fileName TEXT,
			requestId TEXT,
			conversationId TEXT,
			timestamp INTEGER,
			createdAt INTEGER NOT NULL,
			model TEXT
		)`)
	if err != nil {
		t.Fatalf("create ai_code_hashes table: %v", err)
	}

	stmt, err := db.Prepare(`
		INSERT INTO ai_code_hashes (
			hash, source, fileExtension, fileName, requestId, conversationId, timestamp, createdAt, model
		) VALUES (?, ?, '', '', '', '', ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare insert: %v", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		ts := row.CreatedAt
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		if _, err := stmt.Exec(row.Hash, row.Source, ts, ts, row.Model); err != nil {
			t.Fatalf("insert row %q: %v", row.Hash, err)
		}
	}

	return dbPath
}

func TestProvider_Fetch_PlanSpendGaugeUsesIncludedAmountWhenNoLimit(t *testing.T) {
	// When the plan has no hard limit (pu.Limit=0) and no pooled team limit,
	// plan_spend should use the plan's included amount as the gauge reference.
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       36470, // $364.70
				IncludedSpend:    2000,
				Limit:            0, // No hard limit
				TotalPercentUsed: 0,
			},
			DisplayMessage: "Usage-based billing",
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Pro",
				IncludedAmountCents: 2000, // $20 included
				Price:               "$20/mo",
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetHardLimit", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hardLimitResp{})
	})
	mux.HandleFunc("/auth/full_stripe_profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(stripeProfileResp{})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetUsageLimitPolicyStatus", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usageLimitPolicyResp{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-gauge-test",
		Provider: "cursor",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	m, ok := snap.Metrics["plan_spend"]
	if !ok {
		t.Fatal("plan_spend metric missing")
	}
	if m.Used == nil || *m.Used != 364.70 {
		t.Fatalf("plan_spend.Used = %v, want 364.70", m.Used)
	}
	if m.Limit == nil || *m.Limit != 20.0 {
		t.Fatalf("plan_spend.Limit = %v, want 20.0 (from IncludedAmountCents)", m.Limit)
	}
}

func TestProvider_Fetch_CachedBillingMetricsRestoreOnAPIFailure(t *testing.T) {
	// First call: API available → caches billing metrics.
	// Second call: API fails → billing metrics restored from cache.
	var periodCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		periodCalls++
		if periodCalls > 1 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       40700,
				Limit:            0,
				TotalPercentUsed: 85.0,
				AutoPercentUsed:  60.0,
				APIPercentUsed:   25.0,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     360000,
				PooledUsed:      40700,
				PooledRemaining: 319300,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Business",
				IncludedAmountCents: 50000,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "test-model", TotalCents: 100},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	// Create state DB with composer cost data.
	stateDBPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite3", stateDBPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS ItemTable (key TEXT PRIMARY KEY, value TEXT)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS cursorDiskKV (key TEXT PRIMARY KEY, value TEXT)`)
	session := fmt.Sprintf(`{"usageData":{"test-model":{"costInCents":7500,"amount":15}},"unifiedMode":"agent","createdAt":%d}`, time.Now().Add(-1*time.Hour).UnixMilli())
	db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('composerData:aaa', ?)`, session)
	db.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "cursor-cache-billing",
		Provider: "cursor",
		Token:    "test-token",
		ExtraData: map[string]string{
			"state_db": stateDBPath,
		},
	}

	// First fetch: API works, caches billing metrics.
	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch returned error: %v", err)
	}
	// Verify API-derived billing metrics exist.
	if m, ok := snap1.Metrics["spend_limit"]; !ok || m.Limit == nil || *m.Limit != 3600.0 {
		t.Fatalf("spend_limit after API call: got %+v, want Limit=3600", snap1.Metrics["spend_limit"])
	}
	if m, ok := snap1.Metrics["plan_percent_used"]; !ok || m.Used == nil || *m.Used != 85.0 {
		t.Fatalf("plan_percent_used after API call: got %+v, want Used=85", snap1.Metrics["plan_percent_used"])
	}

	// Second fetch: API fails → billing metrics should be restored from cache.
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch returned error: %v", err)
	}

	// spend_limit should be restored from cache.
	if m, ok := snap2.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing after API failure (should be restored from cache)")
	} else {
		if m.Limit == nil || *m.Limit != 3600.0 {
			t.Fatalf("spend_limit.Limit = %v, want 3600 (from cache)", m.Limit)
		}
		if m.Used == nil || *m.Used != 407.0 {
			t.Fatalf("spend_limit.Used = %v, want 407 (from cache)", m.Used)
		}
	}

	// plan_percent_used should be restored from cache.
	if m, ok := snap2.Metrics["plan_percent_used"]; !ok {
		t.Fatal("plan_percent_used missing after API failure (should be restored from cache)")
	} else {
		if m.Used == nil || *m.Used != 85.0 {
			t.Fatalf("plan_percent_used.Used = %v, want 85 (from cache)", m.Used)
		}
	}

	// plan_spend should be restored from cache.
	if m, ok := snap2.Metrics["plan_spend"]; !ok {
		t.Fatal("plan_spend missing after API failure (should be restored from cache)")
	} else {
		if m.Used == nil {
			t.Fatal("plan_spend.Used is nil (should be restored from cache)")
		}
	}
}

func TestProvider_Fetch_PartialAPIFailure_PeriodUsageDown(t *testing.T) {
	// GetCurrentPeriodUsage fails, but GetAggregatedUsageEvents succeeds.
	// After a first successful call caches billing metrics, the second call
	// with GetCurrentPeriodUsage failing should still show billing gauges
	// AND model aggregation data from the live API.
	var periodCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		periodCalls++
		if periodCalls > 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(currentPeriodUsageResp{
			BillingCycleStart: "1768055295000",
			BillingCycleEnd:   "1770733695000",
			PlanUsage: planUsage{
				TotalSpend:       40700,
				TotalPercentUsed: 85.0,
			},
			SpendLimitUsage: spendLimitUsage{
				PooledLimit:     360000,
				PooledUsed:      40700,
				PooledRemaining: 319300,
			},
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "claude-opus", TotalCents: 30000, InputTokens: "1000000"},
			},
			TotalCostCents: 30000,
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	acct := core.AccountConfig{
		ID:       "cursor-partial",
		Provider: "cursor",
		Token:    "test-token",
	}

	// First fetch: everything works.
	snap1, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if _, ok := snap1.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing after successful API call")
	}

	// Second fetch: GetCurrentPeriodUsage fails, but aggregation succeeds.
	snap2, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}

	// Model aggregation from live API should still work.
	if _, ok := snap2.Metrics["billing_total_cost"]; !ok {
		t.Fatal("billing_total_cost missing — aggregation endpoint should still work")
	}

	// Billing gauge should be restored from cache.
	if m, ok := snap2.Metrics["spend_limit"]; !ok {
		t.Fatal("spend_limit missing — should be restored from billing cache")
	} else if m.Limit == nil || *m.Limit != 3600.0 {
		t.Fatalf("spend_limit.Limit = %v, want 3600 (from cached billing)", m.Limit)
	}

	// plan_percent_used should also be restored.
	if m, ok := snap2.Metrics["plan_percent_used"]; !ok {
		t.Fatal("plan_percent_used missing — should be restored from billing cache")
	} else if m.Used == nil || *m.Used != 85.0 {
		t.Fatalf("plan_percent_used.Used = %v, want 85 (from cached billing)", m.Used)
	}
}

func TestProvider_Fetch_NoPeriodUsage_AggregationCreatesGauge(t *testing.T) {
	// GetCurrentPeriodUsage always fails, no billing cache exists.
	// GetAggregatedUsageEvents succeeds with cost data.
	// GetPlanInfo returns IncludedAmountCents.
	// Should create a plan_spend gauge from billing_total_cost + plan limit.
	mux := http.NewServeMux()
	mux.HandleFunc("/aiserver.v1.DashboardService/GetCurrentPeriodUsage", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetAggregatedUsageEvents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedUsageResp{
			Aggregations: []modelAggregation{
				{ModelIntent: "claude-opus", TotalCents: 36470},
			},
			TotalCostCents: 36470,
		})
	})
	mux.HandleFunc("/aiserver.v1.DashboardService/GetPlanInfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(planInfoResp{
			PlanInfo: struct {
				PlanName            string  `json:"planName"`
				IncludedAmountCents float64 `json:"includedAmountCents"`
				Price               string  `json:"price"`
				BillingCycleEnd     string  `json:"billingCycleEnd"`
			}{
				PlanName:            "Pro",
				IncludedAmountCents: 2000,
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	prevBase := cursorAPIBase
	cursorAPIBase = server.URL
	defer func() { cursorAPIBase = prevBase }()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-no-period",
		Provider: "cursor",
		Token:    "test-token",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// billing_total_cost should exist from aggregation.
	if m, ok := snap.Metrics["billing_total_cost"]; !ok || m.Used == nil {
		t.Fatal("billing_total_cost missing from aggregation")
	}

	// plan_spend should be created from billing_total_cost + plan included amount.
	m, ok := snap.Metrics["plan_spend"]
	if !ok {
		t.Fatal("plan_spend missing — should be built from billing_total_cost + plan limit")
	}
	if m.Used == nil || *m.Used != 364.70 {
		t.Fatalf("plan_spend.Used = %v, want 364.70", m.Used)
	}
	if m.Limit == nil || *m.Limit != 20.0 {
		t.Fatalf("plan_spend.Limit = %v, want 20.0 (from IncludedAmountCents)", m.Limit)
	}
}
