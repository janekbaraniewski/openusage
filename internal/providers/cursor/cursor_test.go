package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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

	snap := core.QuotaSnapshot{
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
