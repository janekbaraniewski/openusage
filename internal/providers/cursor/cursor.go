package cursor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/samber/lo"
)

var cursorAPIBase = "https://api2.cursor.sh"

type Provider struct {
	providerbase.Base
	mu                    sync.RWMutex
	modelAggregationCache map[string]cachedModelAggregation
}

type cachedModelAggregation struct {
	BillingCycleStart string
	BillingCycleEnd   string
	Aggregations      []modelAggregation
	EffectiveLimitUSD float64                // cached plan/included limit for gauge fallback
	BillingMetrics    map[string]core.Metric // cached billing metrics for local-only fallback
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "cursor",
			Info: core.ProviderInfo{
				Name:         "Cursor IDE",
				Capabilities: []string{"dashboard_api", "billing", "spend_tracking", "model_aggregation", "local_tracking", "composer_sessions", "ai_code_scoring"},
				DocURL:       "https://www.cursor.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeToken,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Authenticate in Cursor so usage/billing endpoints are available.",
					"Ensure Cursor local state is readable for fallback aggregation.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		modelAggregationCache: make(map[string]cachedModelAggregation),
	}
}

type planUsage struct {
	TotalSpend       float64 `json:"totalSpend"`
	IncludedSpend    float64 `json:"includedSpend"`
	BonusSpend       float64 `json:"bonusSpend"`
	Limit            float64 `json:"limit"`
	AutoPercentUsed  float64 `json:"autoPercentUsed"`
	APIPercentUsed   float64 `json:"apiPercentUsed"`
	TotalPercentUsed float64 `json:"totalPercentUsed"`
}

type spendLimitUsage struct {
	TotalSpend      float64 `json:"totalSpend"`
	PooledLimit     float64 `json:"pooledLimit"`
	PooledUsed      float64 `json:"pooledUsed"`
	PooledRemaining float64 `json:"pooledRemaining"`
	IndividualUsed  float64 `json:"individualUsed"`
	LimitType       string  `json:"limitType"`
}

type currentPeriodUsageResp struct {
	BillingCycleStart string          `json:"billingCycleStart"`
	BillingCycleEnd   string          `json:"billingCycleEnd"`
	PlanUsage         planUsage       `json:"planUsage"`
	SpendLimitUsage   spendLimitUsage `json:"spendLimitUsage"`
	DisplayThreshold  float64         `json:"displayThreshold"`
	DisplayMessage    string          `json:"displayMessage"`
}

type planInfoResp struct {
	PlanInfo struct {
		PlanName            string  `json:"planName"`
		IncludedAmountCents float64 `json:"includedAmountCents"`
		Price               string  `json:"price"`
		BillingCycleEnd     string  `json:"billingCycleEnd"`
	} `json:"planInfo"`
}

type hardLimitResp struct {
	NoUsageBasedAllowed bool `json:"noUsageBasedAllowed"`
}

type modelAggregation struct {
	ModelIntent      string  `json:"modelIntent"`
	InputTokens      string  `json:"inputTokens"`
	OutputTokens     string  `json:"outputTokens"`
	CacheWriteTokens string  `json:"cacheWriteTokens"`
	CacheReadTokens  string  `json:"cacheReadTokens"`
	TotalCents       float64 `json:"totalCents"`
	Tier             int     `json:"tier"`
}

type aggregatedUsageResp struct {
	Aggregations          []modelAggregation `json:"aggregations"`
	TotalInputTokens      string             `json:"totalInputTokens"`
	TotalOutputTokens     string             `json:"totalOutputTokens"`
	TotalCacheWriteTokens string             `json:"totalCacheWriteTokens"`
	TotalCacheReadTokens  string             `json:"totalCacheReadTokens"`
	TotalCostCents        float64            `json:"totalCostCents"`
}

type stripeProfileResp struct {
	MembershipType           string  `json:"membershipType"`
	PaymentID                string  `json:"paymentId"`
	IsTeamMember             bool    `json:"isTeamMember"`
	TeamID                   float64 `json:"teamId"`
	TeamMembershipType       string  `json:"teamMembershipType"`
	IndividualMembershipType string  `json:"individualMembershipType"`
}

type usageLimitPolicyResp struct {
	CanConfigureSpendLimit bool   `json:"canConfigureSpendLimit"`
	LimitType              string `json:"limitType"`
}

type teamMembersResp struct {
	TeamMembers []teamMember `json:"teamMembers"`
	UserID      float64      `json:"userId"`
}

type teamMember struct {
	Name      string  `json:"name"`
	ID        float64 `json:"id"`
	Role      string  `json:"role"`
	Email     string  `json:"email"`
	IsRemoved bool    `json:"isRemoved"`
}

type dailyStats struct {
	Date                   string `json:"date"`
	TabSuggestedLines      int    `json:"tabSuggestedLines"`
	TabAcceptedLines       int    `json:"tabAcceptedLines"`
	ComposerSuggestedLines int    `json:"composerSuggestedLines"`
	ComposerAcceptedLines  int    `json:"composerAcceptedLines"`
}

type composerModelUsage struct {
	CostInCents float64 `json:"costInCents"`
	Amount      int     `json:"amount"`
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Languages", Order: 3, Style: core.DetailSectionStyleLanguages},
			{Name: "Spending", Order: 4, Style: core.DetailSectionStyleSpending},
			{Name: "Trends", Order: 5, Style: core.DetailSectionStyleTrends},
			{Name: "Tokens", Order: 6, Style: core.DetailSectionStyleTokens},
			{Name: "Activity", Order: 7, Style: core.DetailSectionStyleActivity},
		},
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}
	if acct.ExtraData != nil {
		if email := strings.TrimSpace(acct.ExtraData["email"]); email != "" {
			snap.Raw["account_email"] = email
		}
		if membership := strings.TrimSpace(acct.ExtraData["membership"]); membership != "" {
			snap.Raw["membership_type"] = membership
		}
	}

	trackingDBPath := ""
	stateDBPath := ""
	if acct.ExtraData != nil {
		trackingDBPath = acct.ExtraData["tracking_db"]
		stateDBPath = acct.ExtraData["state_db"]
	}
	if trackingDBPath == "" {
		trackingDBPath = acct.Binary
	}
	if stateDBPath == "" {
		stateDBPath = acct.BaseURL
	}

	// If the token was not persisted (json:"-"), try to extract it fresh
	// from the Cursor state DB so daemon polls can access the API.
	token := acct.Token
	if token == "" && stateDBPath != "" {
		token = extractTokenFromStateDB(stateDBPath)
	}

	// Run API calls concurrently with local DB reads so heavy local queries
	// don't consume the context timeout needed by the API.
	type apiResult struct {
		snap *core.UsageSnapshot
		err  error
	}
	apiCh := make(chan apiResult, 1)
	if token != "" {
		go func() {
			apiSnap := core.UsageSnapshot{
				AccountID:   acct.ID,
				Metrics:     make(map[string]core.Metric),
				Resets:      make(map[string]time.Time),
				Raw:         make(map[string]string),
				DailySeries: make(map[string][]core.TimePoint),
			}
			err := p.fetchFromAPI(ctx, token, &apiSnap)
			apiCh <- apiResult{snap: &apiSnap, err: err}
		}()
	} else {
		apiCh <- apiResult{err: fmt.Errorf("no token")}
	}

	// Also resolve ExtraData from persisted fields if not present.
	if acct.ExtraData == nil {
		acct.ExtraData = make(map[string]string)
	}
	if acct.ExtraData["tracking_db"] == "" && trackingDBPath != "" {
		acct.ExtraData["tracking_db"] = trackingDBPath
	}
	if acct.ExtraData["state_db"] == "" && stateDBPath != "" {
		acct.ExtraData["state_db"] = stateDBPath
	}

	var hasLocalData bool
	if trackingDBPath != "" {
		if err := p.readTrackingDB(ctx, trackingDBPath, &snap); err != nil {
			log.Printf("[cursor] tracking DB error: %v", err)
			snap.Raw["tracking_db_error"] = err.Error()
		} else {
			hasLocalData = true
		}
	}
	if stateDBPath != "" {
		if err := p.readStateDB(ctx, stateDBPath, &snap); err != nil {
			log.Printf("[cursor] state DB error: %v", err)
			snap.Raw["state_db_error"] = err.Error()
		} else {
			hasLocalData = true
		}
	}

	// Collect API results.
	ar := <-apiCh
	hasAPIData := false
	if ar.err == nil && ar.snap != nil {
		mergeAPIIntoSnapshot(&snap, ar.snap)
		hasAPIData = true
	} else if ar.err != nil && token != "" {
		log.Printf("[cursor] API fetch failed, falling back to local data: %v", ar.err)
		snap.Raw["api_error"] = ar.err.Error()
	}

	if !hasAPIData && !hasLocalData {
		snap.Status = core.StatusError
		snap.Message = "No Cursor tracking data accessible (no API token and no local DBs)"
		return snap, nil
	}

	if !hasAPIData {
		p.applyCachedModelAggregations(acct.ID, "", "", &snap)
		p.applyCachedBillingMetrics(acct.ID, &snap)
		p.buildLocalOnlyMessage(&snap)
	}

	// Final safety net: ensure credit gauges exist from local data when
	// API didn't provide them (or API is completely unavailable).
	p.ensureCreditGauges(acct.ID, &snap)

	return snap, nil
}

func mergeAPIIntoSnapshot(dst, src *core.UsageSnapshot) {
	for k, v := range src.Metrics {
		dst.Metrics[k] = v
	}
	for k, v := range src.Resets {
		dst.Resets[k] = v
	}
	for k, v := range src.Raw {
		dst.Raw[k] = v
	}
	for k, v := range src.DailySeries {
		dst.DailySeries[k] = v
	}
	dst.ModelUsage = append(dst.ModelUsage, src.ModelUsage...)
	if src.Status != "" {
		dst.Status = src.Status
	}
	if src.Message != "" {
		dst.Message = src.Message
	}
}

func (p *Provider) buildLocalOnlyMessage(snap *core.UsageSnapshot) {
	var parts []string

	if m, ok := snap.Metrics["composer_cost"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f session cost", *m.Used))
	}
	if m, ok := snap.Metrics["total_ai_requests"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f requests", *m.Used))
	}
	if m, ok := snap.Metrics["composer_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f sessions", *m.Used))
	}

	if len(parts) > 0 {
		snap.Message = strings.Join(parts, " · ") + " (API unavailable)"
	} else {
		snap.Message = "Local Cursor IDE usage tracking (API unavailable)"
	}
}

func (p *Provider) fetchFromAPI(ctx context.Context, token string, snap *core.UsageSnapshot) error {
	// All API endpoints are called independently so a single endpoint failure
	// doesn't lose data from the others.
	var (
		hasPeriodUsage                  bool
		periodUsage                     currentPeriodUsageResp
		pu                              planUsage
		su                              spendLimitUsage
		totalSpendDollars, limitDollars float64
	)
	if err := p.callDashboardAPI(ctx, token, "GetCurrentPeriodUsage", &periodUsage); err != nil {
		log.Printf("[cursor] GetCurrentPeriodUsage failed (continuing with other endpoints): %v", err)
		snap.Raw["period_usage_error"] = err.Error()
	} else {
		hasPeriodUsage = true
		pu = periodUsage.PlanUsage
		su = periodUsage.SpendLimitUsage
		totalSpendDollars = pu.TotalSpend / 100.0
		includedDollars := pu.IncludedSpend / 100.0
		limitDollars = pu.Limit / 100.0
		bonusDollars := pu.BonusSpend / 100.0

		snap.Metrics["plan_spend"] = core.Metric{
			Used:   &totalSpendDollars,
			Limit:  &limitDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
		snap.Metrics["plan_included"] = core.Metric{
			Used:   &includedDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
		snap.Metrics["plan_bonus"] = core.Metric{
			Used:   &bonusDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}

		totalPctUsed := pu.TotalPercentUsed
		totalPctRemaining := 100.0 - totalPctUsed
		hundredPct := 100.0
		snap.Metrics["plan_percent_used"] = core.Metric{
			Used:      &totalPctUsed,
			Remaining: &totalPctRemaining,
			Limit:     &hundredPct,
			Unit:      "%",
			Window:    "billing-cycle",
		}
		autoPctUsed := pu.AutoPercentUsed
		autoPctRemaining := 100.0 - autoPctUsed
		snap.Metrics["plan_auto_percent_used"] = core.Metric{
			Used:      &autoPctUsed,
			Remaining: &autoPctRemaining,
			Limit:     &hundredPct,
			Unit:      "%",
			Window:    "billing-cycle",
		}
		apiPctUsed := pu.APIPercentUsed
		apiPctRemaining := 100.0 - apiPctUsed
		snap.Metrics["plan_api_percent_used"] = core.Metric{
			Used:      &apiPctUsed,
			Remaining: &apiPctRemaining,
			Limit:     &hundredPct,
			Unit:      "%",
			Window:    "billing-cycle",
		}

		if su.PooledLimit > 0 {
			pooledLimitDollars := su.PooledLimit / 100.0
			pooledUsedDollars := su.PooledUsed / 100.0
			pooledRemainingDollars := su.PooledRemaining / 100.0
			individualDollars := su.IndividualUsed / 100.0

			snap.Metrics["spend_limit"] = core.Metric{
				Limit:     &pooledLimitDollars,
				Used:      &pooledUsedDollars,
				Remaining: &pooledRemainingDollars,
				Unit:      "USD",
				Window:    "billing-cycle",
			}
			snap.Metrics["individual_spend"] = core.Metric{
				Used:   &individualDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}

			// Stacked gauge: team_budget shows self vs others within the pooled limit.
			teamTotalUsedDollars := pooledUsedDollars
			snap.Metrics["team_budget"] = core.Metric{
				Limit:  &pooledLimitDollars,
				Used:   &teamTotalUsedDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
			selfSpend := individualDollars
			snap.Metrics["team_budget_self"] = core.Metric{
				Used:   &selfSpend,
				Unit:   "USD",
				Window: "billing-cycle",
			}
			othersSpend := pooledUsedDollars - individualDollars
			if othersSpend < 0 {
				othersSpend = 0
			}
			snap.Metrics["team_budget_others"] = core.Metric{
				Used:   &othersSpend,
				Unit:   "USD",
				Window: "billing-cycle",
			}

			snap.Raw["spend_limit_type"] = su.LimitType
		}

		snap.Raw["display_message"] = periodUsage.DisplayMessage
		snap.Raw["display_threshold"] = strconv.FormatFloat(periodUsage.DisplayThreshold, 'f', -1, 64)
		snap.Raw["billing_cycle_start"] = formatTimestamp(periodUsage.BillingCycleStart)
		snap.Raw["billing_cycle_end"] = formatTimestamp(periodUsage.BillingCycleEnd)

		cycleStart := shared.FlexParseTime(periodUsage.BillingCycleStart)
		cycleEnd := shared.FlexParseTime(periodUsage.BillingCycleEnd)
		if !cycleEnd.IsZero() {
			snap.Resets["billing_cycle_end"] = cycleEnd
		}
		if !cycleStart.IsZero() && !cycleEnd.IsZero() && cycleEnd.After(cycleStart) {
			totalDuration := cycleEnd.Sub(cycleStart).Seconds()
			elapsed := time.Since(cycleStart).Seconds()
			if elapsed < 0 {
				elapsed = 0
			}
			if elapsed > totalDuration {
				elapsed = totalDuration
			}
			cyclePct := (elapsed / totalDuration) * 100
			remaining := 100.0 - cyclePct
			hundred := 100.0
			snap.Metrics["billing_cycle_progress"] = core.Metric{
				Used:      &cyclePct,
				Remaining: &remaining,
				Limit:     &hundred,
				Unit:      "%",
				Window:    "billing-cycle",
			}
			daysRemaining := cycleEnd.Sub(time.Now()).Hours() / 24
			if daysRemaining < 0 {
				daysRemaining = 0
			}
			snap.Raw["billing_cycle_days_remaining"] = fmt.Sprintf("%.0f", daysRemaining)
			totalDays := totalDuration / 86400
			snap.Raw["billing_cycle_total_days"] = fmt.Sprintf("%.0f", totalDays)
		}

		if su.PooledLimit > 0 && su.PooledRemaining > 0 {
			spendPctUsed := (su.PooledUsed / su.PooledLimit) * 100
			if spendPctUsed >= 100 {
				snap.Status = core.StatusLimited
			} else if spendPctUsed >= 80 {
				snap.Status = core.StatusNearLimit
			}
		} else if pu.TotalPercentUsed >= 100 {
			snap.Status = core.StatusLimited
		} else if pu.TotalPercentUsed >= 80 {
			snap.Status = core.StatusNearLimit
		}

		snap.Metrics["plan_total_spend_usd"] = core.Metric{
			Used:   &totalSpendDollars,
			Limit:  &limitDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
		if su.PooledLimit > 0 {
			pooledLimitDollars := su.PooledLimit / 100.0
			snap.Metrics["plan_limit_usd"] = core.Metric{
				Limit:  &pooledLimitDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
		} else {
			snap.Metrics["plan_limit_usd"] = core.Metric{
				Limit:  &limitDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
		}
	}

	var planInfo planInfoResp
	if err := p.callDashboardAPI(ctx, token, "GetPlanInfo", &planInfo); err == nil {
		snap.Raw["plan_name"] = planInfo.PlanInfo.PlanName
		snap.Raw["plan_price"] = planInfo.PlanInfo.Price
		snap.Raw["plan_billing_cycle_end"] = formatTimestamp(planInfo.PlanInfo.BillingCycleEnd)
		if planInfo.PlanInfo.IncludedAmountCents > 0 {
			snap.Raw["plan_included_amount_cents"] = strconv.FormatFloat(planInfo.PlanInfo.IncludedAmountCents, 'f', -1, 64)
			planIncludedAmountUSD := planInfo.PlanInfo.IncludedAmountCents / 100.0
			snap.Metrics["plan_included_amount"] = core.Metric{
				Used:   &planIncludedAmountUSD,
				Unit:   "USD",
				Window: "billing-cycle",
			}

			if hasPeriodUsage && limitDollars <= 0 && su.PooledLimit <= 0 {
				effectiveLimit := planIncludedAmountUSD
				snap.Metrics["plan_spend"] = core.Metric{
					Used:   &totalSpendDollars,
					Limit:  &effectiveLimit,
					Unit:   "USD",
					Window: "billing-cycle",
				}
			}
		}
	}

	effectivePlanLimitUSD := limitDollars
	if effectivePlanLimitUSD <= 0 && su.PooledLimit > 0 {
		effectivePlanLimitUSD = su.PooledLimit / 100.0
	}
	if effectivePlanLimitUSD <= 0 && planInfo.PlanInfo.IncludedAmountCents > 0 {
		effectivePlanLimitUSD = planInfo.PlanInfo.IncludedAmountCents / 100.0
	}

	var aggUsage aggregatedUsageResp
	aggErr := p.callDashboardAPI(ctx, token, "GetAggregatedUsageEvents", &aggUsage)
	aggApplied := false
	if aggErr == nil {
		aggApplied = applyModelAggregations(snap, aggUsage.Aggregations)
		if aggApplied {
			p.storeModelAggregationCache(snap.AccountID, snap.Raw["billing_cycle_start"], snap.Raw["billing_cycle_end"], aggUsage.Aggregations, effectivePlanLimitUSD)
		}
		applyAggregationTotals(snap, &aggUsage)
	}
	if !aggApplied && p.applyCachedModelAggregations(snap.AccountID, snap.Raw["billing_cycle_start"], snap.Raw["billing_cycle_end"], snap) {
		if aggErr != nil {
			log.Printf("[cursor] using cached model aggregation after API error: %v", aggErr)
		} else {
			log.Printf("[cursor] using cached model aggregation after empty API aggregation response")
		}
	}

	// If GetCurrentPeriodUsage failed but aggregation succeeded, build a
	// plan_spend gauge from billing_total_cost so credits are visible.
	if !hasPeriodUsage {
		p.applyCachedBillingMetrics(snap.AccountID, snap)
		if _, ok := snap.Metrics["plan_spend"]; !ok {
			if m, ok := snap.Metrics["billing_total_cost"]; ok && m.Used != nil && *m.Used > 0 {
				costUSD := *m.Used
				if effectivePlanLimitUSD > 0 {
					snap.Metrics["plan_spend"] = core.Metric{
						Used:   &costUSD,
						Limit:  core.Float64Ptr(effectivePlanLimitUSD),
						Unit:   "USD",
						Window: "billing-cycle",
					}
				}
			}
		}
	}

	var hardLimit hardLimitResp
	if err := p.callDashboardAPI(ctx, token, "GetHardLimit", &hardLimit); err == nil {
		if hardLimit.NoUsageBasedAllowed {
			snap.Raw["usage_based_billing"] = "disabled"
		} else {
			snap.Raw["usage_based_billing"] = "enabled"
		}
	}

	var profile stripeProfileResp
	if err := p.callRESTAPI(ctx, token, "/auth/full_stripe_profile", &profile); err == nil {
		snap.Raw["membership_type"] = profile.MembershipType
		snap.Raw["is_team_member"] = strconv.FormatBool(profile.IsTeamMember)
		snap.Raw["team_membership"] = profile.TeamMembershipType
		snap.Raw["individual_membership"] = profile.IndividualMembershipType
		if profile.IsTeamMember {
			snap.Raw["team_id"] = fmt.Sprintf("%.0f", profile.TeamID)
		}
	}

	var limitPolicy usageLimitPolicyResp
	if err := p.callDashboardAPI(ctx, token, "GetUsageLimitPolicyStatus", &limitPolicy); err == nil {
		snap.Raw["can_configure_spend_limit"] = strconv.FormatBool(limitPolicy.CanConfigureSpendLimit)
		snap.Raw["limit_policy_type"] = limitPolicy.LimitType
	}

	// Fetch team members if user is on a team.
	if profile.IsTeamMember && profile.TeamID > 0 {
		teamIDStr := fmt.Sprintf("%.0f", profile.TeamID)
		body := []byte(fmt.Sprintf(`{"teamId":"%s"}`, teamIDStr))
		var teamMembers teamMembersResp
		if err := p.callDashboardAPIWithBody(ctx, token, "GetTeamMembers", body, &teamMembers); err == nil {
			var activeCount int
			var memberNames []string
			var ownerCount int
			for _, m := range teamMembers.TeamMembers {
				if m.IsRemoved {
					continue
				}
				activeCount++
				memberNames = append(memberNames, m.Name)
				if strings.Contains(m.Role, "OWNER") {
					ownerCount++
				}
			}
			teamSize := float64(activeCount)
			snap.Metrics["team_size"] = core.Metric{Used: &teamSize, Unit: "members", Window: "current"}
			snap.Raw["team_members"] = strings.Join(memberNames, ", ")
			snap.Raw["team_size"] = strconv.Itoa(activeCount)
			if ownerCount > 0 {
				ownerV := float64(ownerCount)
				snap.Metrics["team_owners"] = core.Metric{Used: &ownerV, Unit: "owners", Window: "current"}
			}
		}
	}

	planName := snap.Raw["plan_name"]
	if su.PooledLimit > 0 {
		pooledLimitDollars := su.PooledLimit / 100.0
		pooledUsedDollars := su.PooledUsed / 100.0
		pooledRemainingDollars := su.PooledRemaining / 100.0
		snap.Message = fmt.Sprintf("%s — $%.0f / $%.0f team spend ($%.0f remaining)",
			planName, pooledUsedDollars, pooledLimitDollars, pooledRemainingDollars)
	} else if limitDollars > 0 {
		snap.Message = fmt.Sprintf("%s — $%.2f / $%.0f plan spend",
			planName, totalSpendDollars, limitDollars)
	} else if planName != "" {
		snap.Message = fmt.Sprintf("%s — %s", planName, periodUsage.DisplayMessage)
	}

	// Cache billing metrics so credit gauges survive temporary API failures.
	p.storeBillingMetricsCache(snap.AccountID, snap)

	// If none of the billing/aggregation endpoints yielded useful data,
	// report an error so the caller knows API data is effectively absent.
	_, hasPlanSpend := snap.Metrics["plan_spend"]
	_, hasSpendLimit := snap.Metrics["spend_limit"]
	_, hasBillingTotal := snap.Metrics["billing_total_cost"]
	if !hasPlanSpend && !hasSpendLimit && !hasBillingTotal && !hasPeriodUsage && !aggApplied {
		return fmt.Errorf("all billing API endpoints failed")
	}

	return nil
}

func (p *Provider) callDashboardAPI(ctx context.Context, token, method string, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", cursorAPIBase, method)
	return p.doPost(ctx, token, url, result)
}

func (p *Provider) callDashboardAPIWithBody(ctx context.Context, token, method string, body []byte, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", cursorAPIBase, method)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (p *Provider) callRESTAPI(ctx context.Context, token, path string, result interface{}) error {
	url := cursorAPIBase + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (p *Provider) doPost(ctx context.Context, token, url string, result interface{}) error {
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func applyModelAggregations(snap *core.UsageSnapshot, aggregations []modelAggregation) bool {
	if len(aggregations) == 0 {
		return false
	}
	if snap.Metrics == nil {
		snap.Metrics = make(map[string]core.Metric)
	}
	if snap.Raw == nil {
		snap.Raw = make(map[string]string)
	}

	var applied bool
	for _, agg := range aggregations {
		modelIntent := strings.TrimSpace(agg.ModelIntent)
		if modelIntent == "" {
			continue
		}
		rec := core.ModelUsageRecord{
			RawModelID: modelIntent,
			RawSource:  "api",
			Window:     "billing-cycle",
		}

		inputTokens := strings.TrimSpace(agg.InputTokens)
		outputTokens := strings.TrimSpace(agg.OutputTokens)
		cacheWriteTokens := strings.TrimSpace(agg.CacheWriteTokens)
		cacheReadTokens := strings.TrimSpace(agg.CacheReadTokens)

		if agg.TotalCents > 0 {
			costDollars := agg.TotalCents / 100.0
			snap.Metrics[fmt.Sprintf("model_%s_cost", modelIntent)] = core.Metric{
				Used:   &costDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
			rec.CostUSD = core.Float64Ptr(costDollars)
		}
		if inputTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_input_tokens", modelIntent)] = inputTokens
		}
		if outputTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_output_tokens", modelIntent)] = outputTokens
		}
		if cacheWriteTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_cache_write_tokens", modelIntent)] = cacheWriteTokens
		}
		if cacheReadTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_cache_read_tokens", modelIntent)] = cacheReadTokens
		}
		if agg.Tier > 0 {
			snap.Raw[fmt.Sprintf("model_%s_tier", modelIntent)] = strconv.Itoa(agg.Tier)
		}

		if parsed, ok := parseModelTokenCount(inputTokens); ok {
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_input_tokens", modelIntent)] = core.Metric{
				Used:   &v,
				Unit:   "tokens",
				Window: "billing-cycle",
			}
			rec.InputTokens = core.Float64Ptr(parsed)
		}
		if parsed, ok := parseModelTokenCount(outputTokens); ok {
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_output_tokens", modelIntent)] = core.Metric{
				Used:   &v,
				Unit:   "tokens",
				Window: "billing-cycle",
			}
			rec.OutputTokens = core.Float64Ptr(parsed)
		}
		cacheWrite := float64(0)
		cacheRead := float64(0)
		hasCacheWrite := false
		hasCacheRead := false
		if parsed, ok := parseModelTokenCount(cacheWriteTokens); ok {
			cacheWrite = parsed
			hasCacheWrite = true
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_cache_write_tokens", modelIntent)] = core.Metric{
				Used:   &v,
				Unit:   "tokens",
				Window: "billing-cycle",
			}
		}
		if parsed, ok := parseModelTokenCount(cacheReadTokens); ok {
			cacheRead = parsed
			hasCacheRead = true
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_cache_read_tokens", modelIntent)] = core.Metric{
				Used:   &v,
				Unit:   "tokens",
				Window: "billing-cycle",
			}
		}
		if hasCacheWrite || hasCacheRead {
			cached := cacheWrite + cacheRead
			snap.Metrics[fmt.Sprintf("model_%s_cached_tokens", modelIntent)] = core.Metric{
				Used:   &cached,
				Unit:   "tokens",
				Window: "billing-cycle",
			}
			rec.CachedTokens = core.Float64Ptr(cached)
		}

		if agg.TotalCents > 0 || inputTokens != "" || outputTokens != "" || cacheWriteTokens != "" || cacheReadTokens != "" {
			applied = true
			core.AppendModelUsageRecord(snap, rec)
		}
	}
	return applied
}

func applyAggregationTotals(snap *core.UsageSnapshot, agg *aggregatedUsageResp) {
	if agg.TotalCostCents > 0 {
		totalCostUSD := agg.TotalCostCents / 100.0
		snap.Metrics["billing_total_cost"] = core.Metric{
			Used:   &totalCostUSD,
			Unit:   "USD",
			Window: "billing-cycle",
		}
	}
	if v, ok := parseModelTokenCount(agg.TotalInputTokens); ok {
		snap.Metrics["billing_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
	}
	if v, ok := parseModelTokenCount(agg.TotalOutputTokens); ok {
		snap.Metrics["billing_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
	}
	if cwv, cwOK := parseModelTokenCount(agg.TotalCacheWriteTokens); cwOK {
		if crv, crOK := parseModelTokenCount(agg.TotalCacheReadTokens); crOK {
			total := cwv + crv
			snap.Metrics["billing_cached_tokens"] = core.Metric{Used: &total, Unit: "tokens", Window: "billing-cycle"}
		}
	}
}

func parseModelTokenCount(raw string) (float64, bool) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return 0, false
	}
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, "_", "")
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (p *Provider) storeModelAggregationCache(accountID, billingCycleStart, billingCycleEnd string, aggregations []modelAggregation, effectiveLimitUSD float64) {
	if accountID == "" || len(aggregations) == 0 {
		return
	}
	copied := make([]modelAggregation, len(aggregations))
	copy(copied, aggregations)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.modelAggregationCache == nil {
		p.modelAggregationCache = make(map[string]cachedModelAggregation)
	}
	entry := cachedModelAggregation{
		BillingCycleStart: billingCycleStart,
		BillingCycleEnd:   billingCycleEnd,
		Aggregations:      copied,
		EffectiveLimitUSD: effectiveLimitUSD,
	}
	// Preserve billing metrics from a previous storeBillingMetricsCache call.
	if prev, ok := p.modelAggregationCache[accountID]; ok && len(prev.BillingMetrics) > 0 {
		entry.BillingMetrics = prev.BillingMetrics
	}
	p.modelAggregationCache[accountID] = entry
}

func (p *Provider) applyCachedModelAggregations(accountID, billingCycleStart, billingCycleEnd string, snap *core.UsageSnapshot) bool {
	if accountID == "" {
		return false
	}

	p.mu.RLock()
	cached, ok := p.modelAggregationCache[accountID]
	p.mu.RUnlock()
	if !ok || len(cached.Aggregations) == 0 {
		return false
	}

	if billingCycleStart != "" && cached.BillingCycleStart != "" && billingCycleStart != cached.BillingCycleStart {
		return false
	}
	if billingCycleEnd != "" && cached.BillingCycleEnd != "" && billingCycleEnd != cached.BillingCycleEnd {
		return false
	}

	copied := make([]modelAggregation, len(cached.Aggregations))
	copy(copied, cached.Aggregations)
	return applyModelAggregations(snap, copied)
}

// billingMetricKeys lists the metric keys cached for local-only fallback.
var billingMetricKeys = []string{
	"plan_spend", "plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used",
	"spend_limit", "individual_spend", "team_budget", "team_budget_self", "team_budget_others",
	"plan_included", "plan_bonus", "plan_total_spend_usd", "plan_limit_usd",
}

func cloneMetric(m core.Metric) core.Metric {
	out := core.Metric{Unit: m.Unit, Window: m.Window}
	if m.Limit != nil {
		out.Limit = core.Float64Ptr(*m.Limit)
	}
	if m.Remaining != nil {
		out.Remaining = core.Float64Ptr(*m.Remaining)
	}
	if m.Used != nil {
		out.Used = core.Float64Ptr(*m.Used)
	}
	return out
}

// storeBillingMetricsCache snapshots the current billing metrics so they can
// be restored when the API is temporarily unavailable.
func (p *Provider) storeBillingMetricsCache(accountID string, snap *core.UsageSnapshot) {
	if accountID == "" {
		return
	}
	cached := make(map[string]core.Metric, len(billingMetricKeys))
	for _, key := range billingMetricKeys {
		if m, ok := snap.Metrics[key]; ok {
			cached[key] = cloneMetric(m)
		}
	}
	if len(cached) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.modelAggregationCache == nil {
		p.modelAggregationCache = make(map[string]cachedModelAggregation)
	}
	entry := p.modelAggregationCache[accountID]
	entry.BillingMetrics = cached
	p.modelAggregationCache[accountID] = entry
}

// applyCachedBillingMetrics restores billing metrics from cache into the
// snapshot so that credit gauges render when the API is temporarily down.
func (p *Provider) applyCachedBillingMetrics(accountID string, snap *core.UsageSnapshot) {
	if accountID == "" {
		return
	}
	p.mu.RLock()
	cached, ok := p.modelAggregationCache[accountID]
	p.mu.RUnlock()
	if !ok || len(cached.BillingMetrics) == 0 {
		return
	}
	for key, m := range cached.BillingMetrics {
		if _, exists := snap.Metrics[key]; !exists {
			snap.Metrics[key] = cloneMetric(m)
		}
	}
}

// ensureCreditGauges synthesizes credit metrics from local data when API
// didn't provide them. This runs as a final step in Fetch() so the Credits
// tag and gauge bars render regardless of API availability.
func (p *Provider) ensureCreditGauges(accountID string, snap *core.UsageSnapshot) {
	// Already have gauge-eligible credit metrics from API — nothing to do.
	if _, ok := snap.Metrics["plan_spend"]; ok {
		return
	}
	if _, ok := snap.Metrics["spend_limit"]; ok {
		return
	}

	// Determine total cost from best available source.
	var costUSD float64
	if m, ok := snap.Metrics["billing_total_cost"]; ok && m.Used != nil && *m.Used > 0 {
		costUSD = *m.Used
	} else if m, ok := snap.Metrics["composer_cost"]; ok && m.Used != nil && *m.Used > 0 {
		costUSD = *m.Used
	}
	if costUSD <= 0 {
		return
	}

	// Always expose plan_total_spend_usd so the Credits tag renders in the
	// TUI even without a limit (computeDisplayInfoRaw checks this key).
	if _, ok := snap.Metrics["plan_total_spend_usd"]; !ok {
		snap.Metrics["plan_total_spend_usd"] = core.Metric{
			Used:   core.Float64Ptr(costUSD),
			Unit:   "USD",
			Window: "billing-cycle",
		}
	}

	// Try to find a limit so we can create a gauge bar.
	var limitUSD float64

	// 1) From plan_included_amount (GetPlanInfo may have succeeded).
	if m, ok := snap.Metrics["plan_included_amount"]; ok && m.Used != nil && *m.Used > 0 {
		limitUSD = *m.Used
	}

	// 2) From cached effective limit.
	if limitUSD <= 0 {
		p.mu.RLock()
		if cached, ok := p.modelAggregationCache[accountID]; ok && cached.EffectiveLimitUSD > 0 {
			limitUSD = cached.EffectiveLimitUSD
		}
		p.mu.RUnlock()
	}

	// 3) From plan_included_amount_cents in Raw (may have been set by API).
	if limitUSD <= 0 {
		if raw, ok := snap.Raw["plan_included_amount_cents"]; ok {
			if cents, err := strconv.ParseFloat(raw, 64); err == nil && cents > 0 {
				limitUSD = cents / 100.0
			}
		}
	}

	if limitUSD > 0 {
		snap.Metrics["plan_spend"] = core.Metric{
			Used:   core.Float64Ptr(costUSD),
			Limit:  core.Float64Ptr(limitUSD),
			Unit:   "USD",
			Window: "billing-cycle",
		}
	}
}

func (p *Provider) readTrackingDB(ctx context.Context, dbPath string, snap *core.UsageSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return fmt.Errorf("opening tracking DB: %w", err)
	}
	defer db.Close()

	var totalRequests int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_code_hashes`).Scan(&totalRequests)
	if err != nil {
		return fmt.Errorf("querying total requests: %w", err)
	}

	if totalRequests > 0 {
		total := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{
			Used:   &total,
			Unit:   "requests",
			Window: "all-time",
		}
	}

	timeExpr := chooseTrackingTimeExpr(ctx, db)
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	var todayCount int
	err = db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM ai_code_hashes WHERE %s >= ?`, timeExpr), todayStart).Scan(&todayCount)
	if err == nil && todayCount > 0 {
		tc := float64(todayCount)
		snap.Metrics["requests_today"] = core.Metric{
			Used:   &tc,
			Unit:   "requests",
			Window: "1d",
		}
	}

	p.readTrackingSourceBreakdown(ctx, db, snap, todayStart, timeExpr)
	p.readTrackingDailyRequests(ctx, db, snap, timeExpr)
	p.readTrackingModelBreakdown(ctx, db, snap, todayStart, timeExpr)
	p.readTrackingLanguageBreakdown(ctx, db, snap)
	p.readScoredCommits(ctx, db, snap)
	p.readDeletedFiles(ctx, db, snap)
	p.readTrackedFileContent(ctx, db, snap)

	return nil
}

func (p *Provider) readScoredCommits(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var totalCommits int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scored_commits WHERE linesAdded IS NOT NULL AND linesAdded > 0`).Scan(&totalCommits) != nil || totalCommits == 0 {
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT v2AiPercentage, linesAdded, linesDeleted,
		       tabLinesAdded, tabLinesDeleted,
		       composerLinesAdded, composerLinesDeleted,
		       humanLinesAdded, humanLinesDeleted,
		       blankLinesAdded, blankLinesDeleted
		FROM scored_commits
		WHERE linesAdded IS NOT NULL AND linesAdded > 0
		ORDER BY scoredAt DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	var (
		sumAIPct       float64
		countWithPct   int
		totalTabAdd    int
		totalTabDel    int
		totalCompAdd   int
		totalCompDel   int
		totalHumanAdd  int
		totalHumanDel  int
		totalBlankAdd  int
		totalBlankDel  int
		totalLinesAdd  int
		totalLinesDel  int
	)

	for rows.Next() {
		var pctStr sql.NullString
		var linesAdded, linesDeleted sql.NullInt64
		var tabAdd, tabDel, compAdd, compDel, humanAdd, humanDel sql.NullInt64
		var blankAdd, blankDel sql.NullInt64
		if rows.Scan(&pctStr, &linesAdded, &linesDeleted, &tabAdd, &tabDel, &compAdd, &compDel, &humanAdd, &humanDel, &blankAdd, &blankDel) != nil {
			continue
		}
		if pctStr.Valid && pctStr.String != "" {
			if v, err := strconv.ParseFloat(pctStr.String, 64); err == nil {
				sumAIPct += v
				countWithPct++
			}
		}
		if linesAdded.Valid {
			totalLinesAdd += int(linesAdded.Int64)
		}
		if linesDeleted.Valid {
			totalLinesDel += int(linesDeleted.Int64)
		}
		if tabAdd.Valid {
			totalTabAdd += int(tabAdd.Int64)
		}
		if tabDel.Valid {
			totalTabDel += int(tabDel.Int64)
		}
		if compAdd.Valid {
			totalCompAdd += int(compAdd.Int64)
		}
		if compDel.Valid {
			totalCompDel += int(compDel.Int64)
		}
		if humanAdd.Valid {
			totalHumanAdd += int(humanAdd.Int64)
		}
		if humanDel.Valid {
			totalHumanDel += int(humanDel.Int64)
		}
		if blankAdd.Valid {
			totalBlankAdd += int(blankAdd.Int64)
		}
		if blankDel.Valid {
			totalBlankDel += int(blankDel.Int64)
		}
	}

	tc := float64(totalCommits)
	snap.Metrics["scored_commits"] = core.Metric{Used: &tc, Unit: "commits", Window: "all-time"}
	snap.Raw["scored_commits_total"] = strconv.Itoa(totalCommits)

	if countWithPct > 0 {
		avgPct := sumAIPct / float64(countWithPct)
		avgPct = math.Round(avgPct*10) / 10
		hundred := 100.0
		remaining := hundred - avgPct
		snap.Metrics["ai_code_percentage"] = core.Metric{
			Used:      &avgPct,
			Remaining: &remaining,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "all-commits",
		}
		snap.Raw["ai_code_pct_avg"] = fmt.Sprintf("%.1f%%", avgPct)
		snap.Raw["ai_code_pct_sample"] = strconv.Itoa(countWithPct)
	}

	if totalLinesAdd > 0 || totalLinesDel > 0 {
		snap.Raw["commit_total_lines_added"] = strconv.Itoa(totalLinesAdd)
		snap.Raw["commit_total_lines_deleted"] = strconv.Itoa(totalLinesDel)
	}
	if totalTabAdd > 0 || totalCompAdd > 0 || totalHumanAdd > 0 {
		snap.Raw["commit_tab_lines"] = strconv.Itoa(totalTabAdd)
		snap.Raw["commit_composer_lines"] = strconv.Itoa(totalCompAdd)
		snap.Raw["commit_human_lines"] = strconv.Itoa(totalHumanAdd)
	}
	if totalTabDel > 0 || totalCompDel > 0 || totalHumanDel > 0 {
		snap.Raw["commit_tab_lines_deleted"] = strconv.Itoa(totalTabDel)
		snap.Raw["commit_composer_lines_deleted"] = strconv.Itoa(totalCompDel)
		snap.Raw["commit_human_lines_deleted"] = strconv.Itoa(totalHumanDel)
	}
	if totalBlankAdd > 0 || totalBlankDel > 0 {
		snap.Raw["commit_blank_lines_added"] = strconv.Itoa(totalBlankAdd)
		snap.Raw["commit_blank_lines_deleted"] = strconv.Itoa(totalBlankDel)
	}
}

func (p *Provider) readDeletedFiles(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var count int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_deleted_files`).Scan(&count) == nil && count > 0 {
		v := float64(count)
		snap.Metrics["ai_deleted_files"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
}

func (p *Provider) readTrackedFileContent(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var count int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracked_file_content`).Scan(&count) == nil && count > 0 {
		v := float64(count)
		snap.Metrics["ai_tracked_files"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
}

func chooseTrackingTimeExpr(ctx context.Context, db *sql.DB) string {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(ai_code_hashes)`)
	if err != nil {
		return "createdAt"
	}
	defer rows.Close()

	hasCreatedAt := false
	hasTimestamp := false
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk) != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "createdat":
			hasCreatedAt = true
		case "timestamp":
			hasTimestamp = true
		}
	}

	switch {
	case hasCreatedAt && hasTimestamp:
		return "COALESCE(createdAt, timestamp)"
	case hasCreatedAt:
		return "createdAt"
	case hasTimestamp:
		return "timestamp"
	default:
		return "createdAt"
	}
}

func (p *Provider) readTrackingSourceBreakdown(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot, todayStart int64, timeExpr string) {
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(source, ''), COUNT(*)
		FROM ai_code_hashes
		GROUP BY COALESCE(source, '')
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	clientTotals := map[string]float64{
		"ide":        0,
		"cli_agents": 0,
		"other":      0,
	}
	var sourceSummary []string

	for rows.Next() {
		var source string
		var count int
		if rows.Scan(&source, &count) != nil || count <= 0 {
			continue
		}

		value := float64(count)
		sourceKey := sanitizeCursorMetricName(source)
		snap.Metrics["source_"+sourceKey+"_requests"] = core.Metric{
			Used:   &value,
			Unit:   "requests",
			Window: "all-time",
		}

		// Emit interface-level metrics for the Interface breakdown composition.
		ifaceValue := value
		snap.Metrics["interface_"+sourceKey] = core.Metric{
			Used:   &ifaceValue,
			Unit:   "calls",
			Window: "all-time",
		}

		clientKey := cursorClientBucket(source)
		clientTotals[clientKey] += value
		sourceSummary = append(sourceSummary, fmt.Sprintf("%s %d", sourceLabel(source), count))
	}

	if len(sourceSummary) > 0 {
		snap.Raw["source_usage"] = strings.Join(sourceSummary, " · ")
	}

	for bucket, value := range clientTotals {
		if value <= 0 {
			continue
		}
		v := value
		snap.Metrics["client_"+bucket+"_sessions"] = core.Metric{
			Used:   &v,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	todayRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(source, ''), COUNT(*)
		FROM ai_code_hashes
		WHERE %s >= ?
		GROUP BY COALESCE(source, '')
		ORDER BY COUNT(*) DESC`, timeExpr), todayStart)
	if err != nil {
		return
	}
	defer todayRows.Close()

	var todaySummary []string
	for todayRows.Next() {
		var source string
		var count int
		if todayRows.Scan(&source, &count) != nil || count <= 0 {
			continue
		}
		value := float64(count)
		sourceKey := sanitizeCursorMetricName(source)
		snap.Metrics["source_"+sourceKey+"_requests_today"] = core.Metric{
			Used:   &value,
			Unit:   "requests",
			Window: "1d",
		}
		todaySummary = append(todaySummary, fmt.Sprintf("%s %d", sourceLabel(source), count))
	}
	if len(todaySummary) > 0 {
		snap.Raw["source_usage_today"] = strings.Join(todaySummary, " · ")
	}
}

func (p *Provider) readTrackingDailyRequests(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot, timeExpr string) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(source, ''), strftime('%%Y-%%m-%%d', (%s)/1000, 'unixepoch', 'localtime') as day, COUNT(*)
		FROM ai_code_hashes
		GROUP BY COALESCE(source, ''), day
		ORDER BY day ASC`, timeExpr))
	if err != nil {
		return
	}
	defer rows.Close()

	totalByDay := make(map[string]float64)
	byClientDay := map[string]map[string]float64{
		"ide":        make(map[string]float64),
		"cli_agents": make(map[string]float64),
		"other":      make(map[string]float64),
	}
	bySourceDay := make(map[string]map[string]float64)

	for rows.Next() {
		var source string
		var day string
		var count int
		if rows.Scan(&source, &day, &count) != nil || count <= 0 || day == "" {
			continue
		}

		v := float64(count)
		totalByDay[day] += v
		clientKey := cursorClientBucket(source)
		byClientDay[clientKey][day] += v
		sourceKey := sanitizeCursorMetricName(source)
		if bySourceDay[sourceKey] == nil {
			bySourceDay[sourceKey] = make(map[string]float64)
		}
		bySourceDay[sourceKey][day] += v
	}

	if len(totalByDay) > 1 {
		snap.DailySeries["analytics_requests"] = mapToSortedDailyPoints(totalByDay)
	}
	for clientKey, pointsByDay := range byClientDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_client_"+clientKey] = mapToSortedDailyPoints(pointsByDay)
	}
	for sourceKey, pointsByDay := range bySourceDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_source_"+sourceKey] = mapToSortedDailyPoints(pointsByDay)
	}
}

func (p *Provider) readTrackingModelBreakdown(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot, todayStart int64, timeExpr string) {
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(model, ''), COUNT(*)
		FROM ai_code_hashes
		GROUP BY COALESCE(model, '')
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	var modelSummary []string
	for rows.Next() {
		var model string
		var count int
		if rows.Scan(&model, &count) != nil || count <= 0 {
			continue
		}

		value := float64(count)
		modelKey := sanitizeCursorMetricName(model)
		snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{
			Used:   &value,
			Unit:   "requests",
			Window: "all-time",
		}
		modelSummary = append(modelSummary, fmt.Sprintf("%s %d", sourceLabel(model), count))
	}
	if len(modelSummary) > 0 {
		snap.Raw["model_usage"] = strings.Join(modelSummary, " · ")
	}

	todayRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(model, ''), COUNT(*)
		FROM ai_code_hashes
		WHERE %s >= ?
		GROUP BY COALESCE(model, '')
		ORDER BY COUNT(*) DESC`, timeExpr), todayStart)
	if err == nil {
		defer todayRows.Close()
		for todayRows.Next() {
			var model string
			var count int
			if todayRows.Scan(&model, &count) != nil || count <= 0 {
				continue
			}
			value := float64(count)
			modelKey := sanitizeCursorMetricName(model)
			snap.Metrics["model_"+modelKey+"_requests_today"] = core.Metric{
				Used:   &value,
				Unit:   "requests",
				Window: "1d",
			}
		}
	}

	dailyRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT COALESCE(model, ''), strftime('%%Y-%%m-%%d', (%s)/1000, 'unixepoch', 'localtime') as day, COUNT(*)
		FROM ai_code_hashes
		GROUP BY COALESCE(model, ''), day
		ORDER BY day ASC`, timeExpr))
	if err != nil {
		return
	}
	defer dailyRows.Close()

	byModelDay := make(map[string]map[string]float64)
	for dailyRows.Next() {
		var model string
		var day string
		var count int
		if dailyRows.Scan(&model, &day, &count) != nil || count <= 0 || day == "" {
			continue
		}
		modelKey := sanitizeCursorMetricName(model)
		if byModelDay[modelKey] == nil {
			byModelDay[modelKey] = make(map[string]float64)
		}
		byModelDay[modelKey][day] += float64(count)
	}
	for modelKey, pointsByDay := range byModelDay {
		if len(pointsByDay) < 2 {
			continue
		}
		snap.DailySeries["usage_model_"+modelKey] = mapToSortedDailyPoints(pointsByDay)
	}
}

func (p *Provider) readTrackingLanguageBreakdown(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(fileExtension, ''), COUNT(*)
		FROM ai_code_hashes
		WHERE fileExtension IS NOT NULL AND fileExtension != ''
		GROUP BY COALESCE(fileExtension, '')
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return
	}
	defer rows.Close()

	var langSummary []string
	for rows.Next() {
		var ext string
		var count int
		if rows.Scan(&ext, &count) != nil || count <= 0 {
			continue
		}

		value := float64(count)
		langName := extensionToLanguage(ext)
		langKey := sanitizeCursorMetricName(langName)
		snap.Metrics["lang_"+langKey] = core.Metric{
			Used:   &value,
			Unit:   "requests",
			Window: "all-time",
		}
		langSummary = append(langSummary, fmt.Sprintf("%s %d", langName, count))
	}
	if len(langSummary) > 0 {
		snap.Raw["language_usage"] = strings.Join(langSummary, " · ")
	}
}

var extToLang = map[string]string{
	".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript", ".jsx": "JavaScript",
	".py": "Python", ".go": "Go", ".rs": "Rust", ".rb": "Ruby",
	".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin",
	".cs": "C#", ".fs": "F#",
	".cpp": "C++", ".cc": "C++", ".cxx": "C++", ".hpp": "C++",
	".c": "C", ".h": "C/C++",
	".swift": "Swift", ".m": "Obj-C",
	".php": "PHP", ".lua": "Lua", ".r": "R",
	".scala": "Scala", ".clj": "Clojure", ".ex": "Elixir", ".exs": "Elixir",
	".hs": "Haskell", ".erl": "Erlang",
	".html": "HTML", ".htm": "HTML", ".css": "CSS", ".scss": "SCSS", ".less": "LESS",
	".json": "JSON", ".yaml": "YAML", ".yml": "YAML", ".toml": "TOML", ".xml": "XML",
	".md": "Markdown", ".mdx": "Markdown",
	".sql": "SQL", ".graphql": "GraphQL", ".gql": "GraphQL",
	".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".fish": "Shell",
	".dockerfile": "Docker", ".tf": "Terraform", ".hcl": "HCL",
	".vue": "Vue", ".svelte": "Svelte", ".astro": "Astro",
	".dart": "Dart", ".zig": "Zig", ".nim": "Nim", ".v": "V",
	".proto": "Protobuf", ".wasm": "WASM",
}

func extensionToLanguage(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return strings.TrimPrefix(ext, ".")
}

func mapToSortedDailyPoints(byDay map[string]float64) []core.TimePoint {
	if len(byDay) == 0 {
		return nil
	}
	days := lo.Keys(byDay)
	sort.Strings(days)

	points := make([]core.TimePoint, 0, len(days))
	for _, day := range days {
		points = append(points, core.TimePoint{Date: day, Value: byDay[day]})
	}
	return points
}

func cursorClientBucket(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	switch {
	case s == "":
		return "other"
	case strings.Contains(s, "cloud"), strings.Contains(s, "web"), s == "background-agent", s == "background_agent":
		return "cloud_agents"
	case strings.Contains(s, "cli"), strings.Contains(s, "agent"), strings.Contains(s, "terminal"), strings.Contains(s, "cmd"):
		return "cli_agents"
	case s == "composer", s == "tab", s == "human", strings.Contains(s, "ide"), strings.Contains(s, "editor"):
		return "ide"
	default:
		return "other"
	}
}

func sanitizeCursorMetricName(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func sourceLabel(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func (p *Provider) readStateDB(ctx context.Context, dbPath string, snap *core.UsageSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return fmt.Errorf("opening state DB: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("state DB not accessible: %w", err)
	}

	p.readDailyStatsToday(ctx, db, snap)
	p.readDailyStatsSeries(ctx, db, snap)
	p.readComposerSessions(ctx, db, snap)
	p.readStateMetadata(ctx, db, snap)
	p.readToolUsage(ctx, db, snap)

	return nil
}

func (p *Provider) readDailyStatsToday(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	today := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("aiCodeTracking.dailyStats.v1.5.%s", today)

	var value string
	err := db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
			key = fmt.Sprintf("aiCodeTracking.dailyStats.v1.5.%s", yesterday)
			err = db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
			if err != nil {
				return
			}
		} else {
			return
		}
	}

	var stats dailyStats
	if json.Unmarshal([]byte(value), &stats) != nil {
		return
	}

	if stats.TabSuggestedLines > 0 {
		suggested := float64(stats.TabSuggestedLines)
		accepted := float64(stats.TabAcceptedLines)
		snap.Metrics["tab_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["tab_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}
	if stats.ComposerSuggestedLines > 0 {
		suggested := float64(stats.ComposerSuggestedLines)
		accepted := float64(stats.ComposerAcceptedLines)
		snap.Metrics["composer_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["composer_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}
}

func (p *Provider) readComposerSessions(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	rows, err := db.QueryContext(ctx, `
		SELECT json_extract(value, '$.usageData'),
		       json_extract(value, '$.unifiedMode'),
		       json_extract(value, '$.createdAt'),
		       json_extract(value, '$.totalLinesAdded'),
		       json_extract(value, '$.totalLinesRemoved'),
		       json_extract(value, '$.contextTokensUsed'),
		       json_extract(value, '$.contextTokenLimit'),
		       json_extract(value, '$.filesChangedCount'),
		       json_extract(value, '$.subagentInfo.subagentTypeName'),
		       json_extract(value, '$.isAgentic'),
		       json_extract(value, '$.forceMode'),
		       json_extract(value, '$.addedFiles'),
		       json_extract(value, '$.removedFiles'),
		       json_extract(value, '$.status')
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
		  AND json_extract(value, '$.usageData') IS NOT NULL
		  AND json_extract(value, '$.usageData') != '{}'`)
	if err != nil {
		log.Printf("[cursor] composerData query error: %v", err)
		return
	}
	defer rows.Close()

	var (
		totalCostCents     float64
		totalRequests      int
		totalSessions      int
		totalLinesAdded    int
		totalLinesRemoved  int
		totalFilesChanged  int
		totalFilesCreated  int
		totalFilesRemoved  int
		agenticSessions    int
		nonAgenticSessions int
		totalContextUsed   float64
		totalContextLimit  float64
		contextSampleCount int
		subagentTypes      = make(map[string]int)
		modelCosts         = make(map[string]float64)
		modelRequests      = make(map[string]int)
		modeSessions       = make(map[string]int)
		forceModes         = make(map[string]int)
		statusCounts       = make(map[string]int)
		dailyCost          = make(map[string]float64)
		dailyRequests      = make(map[string]float64)
		todayCostCents     float64
		todayRequests      int
	)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for rows.Next() {
		var usageJSON sql.NullString
		var mode sql.NullString
		var createdAt sql.NullInt64
		var linesAdded sql.NullInt64
		var linesRemoved sql.NullInt64
		var ctxUsed sql.NullFloat64
		var ctxLimit sql.NullFloat64
		var filesChanged sql.NullInt64
		var subagentType sql.NullString
		var isAgentic sql.NullBool
		var forceMode sql.NullString
		var addedFiles sql.NullInt64
		var removedFiles sql.NullInt64
		var status sql.NullString
		if rows.Scan(&usageJSON, &mode, &createdAt, &linesAdded, &linesRemoved,
			&ctxUsed, &ctxLimit, &filesChanged, &subagentType,
			&isAgentic, &forceMode, &addedFiles, &removedFiles, &status) != nil {
			continue
		}
		if !usageJSON.Valid || usageJSON.String == "" || usageJSON.String == "{}" {
			continue
		}

		var usage map[string]composerModelUsage
		if json.Unmarshal([]byte(usageJSON.String), &usage) != nil {
			continue
		}

		totalSessions++
		if mode.Valid && mode.String != "" {
			modeSessions[mode.String]++
		}
		if isAgentic.Valid {
			if isAgentic.Bool {
				agenticSessions++
			} else {
				nonAgenticSessions++
			}
		}
		if forceMode.Valid && forceMode.String != "" {
			forceModes[forceMode.String]++
		}
		if status.Valid && status.String != "" {
			statusCounts[status.String]++
		}
		if linesAdded.Valid {
			totalLinesAdded += int(linesAdded.Int64)
		}
		if linesRemoved.Valid {
			totalLinesRemoved += int(linesRemoved.Int64)
		}
		if filesChanged.Valid && filesChanged.Int64 > 0 {
			totalFilesChanged += int(filesChanged.Int64)
		}
		if addedFiles.Valid && addedFiles.Int64 > 0 {
			totalFilesCreated += int(addedFiles.Int64)
		}
		if removedFiles.Valid && removedFiles.Int64 > 0 {
			totalFilesRemoved += int(removedFiles.Int64)
		}
		if ctxUsed.Valid && ctxUsed.Float64 > 0 && ctxLimit.Valid && ctxLimit.Float64 > 0 {
			totalContextUsed += ctxUsed.Float64
			totalContextLimit += ctxLimit.Float64
			contextSampleCount++
		}
		if subagentType.Valid && subagentType.String != "" {
			subagentTypes[subagentType.String]++
		}

		var sessionDay string
		if createdAt.Valid && createdAt.Int64 > 0 {
			t := time.UnixMilli(createdAt.Int64)
			sessionDay = t.In(now.Location()).Format("2006-01-02")
		}

		for model, mu := range usage {
			totalCostCents += mu.CostInCents
			totalRequests += mu.Amount
			modelCosts[model] += mu.CostInCents
			modelRequests[model] += mu.Amount

			if sessionDay != "" {
				dailyCost[sessionDay] += mu.CostInCents
				dailyRequests[sessionDay] += float64(mu.Amount)
			}
			if createdAt.Valid && time.UnixMilli(createdAt.Int64).After(todayStart) {
				todayCostCents += mu.CostInCents
				todayRequests += mu.Amount
			}
		}
	}

	if totalSessions == 0 {
		return
	}

	totalCostUSD := totalCostCents / 100.0
	snap.Metrics["composer_cost"] = core.Metric{
		Used:   &totalCostUSD,
		Unit:   "USD",
		Window: "all-time",
	}

	if todayCostCents > 0 {
		todayCostUSD := todayCostCents / 100.0
		snap.Metrics["today_cost"] = core.Metric{
			Used:   &todayCostUSD,
			Unit:   "USD",
			Window: "1d",
		}
	}
	if todayRequests > 0 {
		tr := float64(todayRequests)
		snap.Metrics["today_composer_requests"] = core.Metric{
			Used:   &tr,
			Unit:   "requests",
			Window: "1d",
		}
	}

	sessions := float64(totalSessions)
	snap.Metrics["composer_sessions"] = core.Metric{
		Used:   &sessions,
		Unit:   "sessions",
		Window: "all-time",
	}
	reqs := float64(totalRequests)
	snap.Metrics["composer_requests"] = core.Metric{
		Used:   &reqs,
		Unit:   "requests",
		Window: "all-time",
	}

	if totalLinesAdded > 0 {
		la := float64(totalLinesAdded)
		snap.Metrics["composer_lines_added"] = core.Metric{Used: &la, Unit: "lines", Window: "all-time"}
	}
	if totalLinesRemoved > 0 {
		lr := float64(totalLinesRemoved)
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: &lr, Unit: "lines", Window: "all-time"}
	}

	for model, costCents := range modelCosts {
		costUSD := costCents / 100.0
		modelKey := sanitizeCursorMetricName(model)
		snap.Metrics["model_"+modelKey+"_cost"] = core.Metric{
			Used:   &costUSD,
			Unit:   "USD",
			Window: "all-time",
		}
		if reqs, ok := modelRequests[model]; ok {
			r := float64(reqs)
			if existing, exists := snap.Metrics["model_"+modelKey+"_requests"]; exists && existing.Used != nil {
				combined := *existing.Used + r
				snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{
					Used:   &combined,
					Unit:   "requests",
					Window: "all-time",
				}
			} else {
				snap.Metrics["model_"+modelKey+"_requests"] = core.Metric{
					Used:   &r,
					Unit:   "requests",
					Window: "all-time",
				}
			}
		}

		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "composer",
			Window:     "all-time",
			CostUSD:    core.Float64Ptr(costUSD),
		}
		if r, ok := modelRequests[model]; ok {
			rec.Requests = core.Float64Ptr(float64(r))
		}
		core.AppendModelUsageRecord(snap, rec)
	}

	for mode, count := range modeSessions {
		v := float64(count)
		modeKey := sanitizeCursorMetricName(mode)
		snap.Metrics["mode_"+modeKey+"_sessions"] = core.Metric{
			Used:   &v,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	if totalFilesChanged > 0 {
		fc := float64(totalFilesChanged)
		snap.Metrics["composer_files_changed"] = core.Metric{Used: &fc, Unit: "files", Window: "all-time"}
	}
	if totalFilesCreated > 0 {
		v := float64(totalFilesCreated)
		snap.Metrics["composer_files_created"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}
	if totalFilesRemoved > 0 {
		v := float64(totalFilesRemoved)
		snap.Metrics["composer_files_removed"] = core.Metric{Used: &v, Unit: "files", Window: "all-time"}
	}

	if agenticSessions > 0 {
		v := float64(agenticSessions)
		snap.Metrics["agentic_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}
	if nonAgenticSessions > 0 {
		v := float64(nonAgenticSessions)
		snap.Metrics["non_agentic_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: "all-time"}
	}

	for fm, count := range forceModes {
		v := float64(count)
		fmKey := sanitizeCursorMetricName(fm)
		snap.Metrics["mode_"+fmKey+"_sessions"] = core.Metric{
			Used:   &v,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	if contextSampleCount > 0 {
		avgPct := (totalContextUsed / totalContextLimit) * 100
		avgPct = math.Round(avgPct*10) / 10
		hundred := 100.0
		remaining := hundred - avgPct
		snap.Metrics["composer_context_pct"] = core.Metric{
			Used:      &avgPct,
			Remaining: &remaining,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "avg",
		}
	}

	for saType, count := range subagentTypes {
		v := float64(count)
		saKey := sanitizeCursorMetricName(saType)
		snap.Metrics["subagent_"+saKey+"_sessions"] = core.Metric{
			Used:   &v,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	snap.Raw["composer_total_cost"] = fmt.Sprintf("$%.2f", totalCostUSD)
	snap.Raw["composer_total_sessions"] = strconv.Itoa(totalSessions)
	snap.Raw["composer_total_requests"] = strconv.Itoa(totalRequests)
	if totalLinesAdded > 0 {
		snap.Raw["composer_lines_added"] = strconv.Itoa(totalLinesAdded)
		snap.Raw["composer_lines_removed"] = strconv.Itoa(totalLinesRemoved)
	}

	if len(dailyCost) > 1 {
		points := make([]core.TimePoint, 0, len(dailyCost))
		for day, cents := range dailyCost {
			points = append(points, core.TimePoint{Date: day, Value: cents / 100.0})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Date < points[j].Date })
		snap.DailySeries["analytics_cost"] = points
	}
	if len(dailyRequests) > 1 {
		points := mapToSortedDailyPoints(dailyRequests)
		if existing, ok := snap.DailySeries["analytics_requests"]; ok && len(existing) > 0 {
			snap.DailySeries["analytics_requests"] = mergeDailyPoints(existing, points)
		} else {
			snap.DailySeries["composer_requests_daily"] = points
		}
	}
}

func mergeDailyPoints(a, b []core.TimePoint) []core.TimePoint {
	byDay := make(map[string]float64)
	for _, p := range a {
		byDay[p.Date] += p.Value
	}
	for _, p := range b {
		if byDay[p.Date] < p.Value {
			byDay[p.Date] = p.Value
		}
	}
	return mapToSortedDailyPoints(byDay)
}

// extractTokenFromStateDB reads the Cursor access token directly from the
// state.vscdb SQLite database. This is needed because the Token field has
// json:"-" and is not persisted to the config file, so daemon polls that
// load accounts from config would otherwise have no API token.
func extractTokenFromStateDB(dbPath string) string {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return ""
	}
	defer db.Close()

	var token string
	if db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&token) != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func (p *Provider) readStateMetadata(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	var email string
	if db.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email) == nil && email != "" {
		snap.Raw["account_email"] = email
	}

	var promptCount string
	if db.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'freeBestOfN.promptCount'`).Scan(&promptCount) == nil && promptCount != "" {
		if v, err := strconv.ParseFloat(promptCount, 64); err == nil && v > 0 {
			snap.Metrics["total_prompts"] = core.Metric{Used: &v, Unit: "prompts", Window: "all-time"}
			snap.Raw["total_prompts"] = promptCount
		}
	}

	var membership string
	if db.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/stripeMembershipType'`).Scan(&membership) == nil && membership != "" {
		if snap.Raw["membership_type"] == "" {
			snap.Raw["membership_type"] = membership
		}
	}
}

// readToolUsage extracts tool call statistics from the bubbleId entries
// in cursorDiskKV. Each AI-response bubble (type=2) may contain a
// toolFormerData object with the tool name, status, and other metadata.
func (p *Provider) readToolUsage(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	rows, err := db.QueryContext(ctx, `
		SELECT json_extract(value, '$.toolFormerData.name') as tool_name,
		       json_extract(value, '$.toolFormerData.status') as tool_status
		FROM cursorDiskKV
		WHERE key LIKE 'bubbleId:%'
		  AND json_extract(value, '$.type') = 2
		  AND json_extract(value, '$.toolFormerData.name') IS NOT NULL
		  AND json_extract(value, '$.toolFormerData.name') != ''`)
	if err != nil {
		log.Printf("[cursor] tool usage query error: %v", err)
		return
	}
	defer rows.Close()

	toolCounts := make(map[string]int)
	statusCounts := make(map[string]int)
	var totalCalls int

	for rows.Next() {
		var toolName sql.NullString
		var toolStatus sql.NullString
		if rows.Scan(&toolName, &toolStatus) != nil {
			continue
		}
		if !toolName.Valid || toolName.String == "" {
			continue
		}

		name := normalizeToolName(toolName.String)
		toolCounts[name]++
		totalCalls++

		if toolStatus.Valid && toolStatus.String != "" {
			statusCounts[toolStatus.String]++
		}
	}

	if totalCalls == 0 {
		return
	}

	tc := float64(totalCalls)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &tc, Unit: "calls", Window: "all-time"}

	for name, count := range toolCounts {
		v := float64(count)
		toolKey := sanitizeCursorMetricName(name)
		snap.Metrics["tool_"+toolKey] = core.Metric{
			Used:   &v,
			Unit:   "calls",
			Window: "all-time",
		}
	}

	if completed, ok := statusCounts["completed"]; ok && completed > 0 {
		v := float64(completed)
		snap.Metrics["tool_completed"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}
	if errored, ok := statusCounts["error"]; ok && errored > 0 {
		v := float64(errored)
		snap.Metrics["tool_errored"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}
	if cancelled, ok := statusCounts["cancelled"]; ok && cancelled > 0 {
		v := float64(cancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &v, Unit: "calls", Window: "all-time"}
	}

	if totalCalls > 0 {
		completed := float64(statusCounts["completed"])
		successPct := (completed / float64(totalCalls)) * 100
		successPct = math.Round(successPct*10) / 10
		hundred := 100.0
		remaining := hundred - successPct
		snap.Metrics["tool_success_rate"] = core.Metric{
			Used:      &successPct,
			Remaining: &remaining,
			Limit:     &hundred,
			Unit:      "%",
			Window:    "all-time",
		}
	}

	snap.Raw["tool_calls_total"] = strconv.Itoa(totalCalls)
	snap.Raw["tool_completed"] = strconv.Itoa(statusCounts["completed"])
	snap.Raw["tool_errored"] = strconv.Itoa(statusCounts["error"])
	snap.Raw["tool_cancelled"] = strconv.Itoa(statusCounts["cancelled"])
}

// normalizeToolName cleans up raw tool names from Cursor bubble data.
// MCP tools have long prefixed names like "mcp-kubernetes-user-kubernetes-pods_list"
// which we normalize to shorter display names.
func normalizeToolName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "unknown"
	}

	// Normalize MCP tool names: "mcp-server-user-server-tool" → "tool (mcp)"
	if strings.HasPrefix(name, "mcp-") || strings.HasPrefix(name, "mcp_") {
		parts := strings.Split(name, "-")
		if len(parts) >= 4 {
			toolPart := strings.Join(parts[3:], "-")
			return toolPart + " (mcp)"
		}
		parts = strings.Split(name, "_")
		if len(parts) >= 3 {
			toolPart := strings.Join(parts[2:], "_")
			return toolPart + " (mcp)"
		}
	}

	// Strip version suffixes: "read_file_v2" → "read_file"
	name = strings.TrimSuffix(name, "_v2")
	name = strings.TrimSuffix(name, "_v3")

	return name
}

func (p *Provider) readDailyStatsSeries(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) {
	rows, err := db.QueryContext(ctx,
		`SELECT key, value FROM ItemTable WHERE key LIKE 'aiCodeTracking.dailyStats.v1.5.%' ORDER BY key ASC`)
	if err != nil {
		return
	}
	defer rows.Close()

	prefix := "aiCodeTracking.dailyStats.v1.5."
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		dateStr := strings.TrimPrefix(k, prefix)
		if len(dateStr) != 10 { // "2025-01-15"
			continue
		}

		var ds dailyStats
		if json.Unmarshal([]byte(v), &ds) != nil {
			continue
		}

		if ds.TabSuggestedLines > 0 || ds.TabAcceptedLines > 0 {
			snap.DailySeries["tab_suggested"] = append(snap.DailySeries["tab_suggested"],
				core.TimePoint{Date: dateStr, Value: float64(ds.TabSuggestedLines)})
			snap.DailySeries["tab_accepted"] = append(snap.DailySeries["tab_accepted"],
				core.TimePoint{Date: dateStr, Value: float64(ds.TabAcceptedLines)})
		}

		if ds.ComposerSuggestedLines > 0 || ds.ComposerAcceptedLines > 0 {
			snap.DailySeries["composer_suggested"] = append(snap.DailySeries["composer_suggested"],
				core.TimePoint{Date: dateStr, Value: float64(ds.ComposerSuggestedLines)})
			snap.DailySeries["composer_accepted"] = append(snap.DailySeries["composer_accepted"],
				core.TimePoint{Date: dateStr, Value: float64(ds.ComposerAcceptedLines)})
		}

		totalLines := float64(ds.TabSuggestedLines + ds.ComposerSuggestedLines)
		if totalLines > 0 {
			snap.DailySeries["total_lines"] = append(snap.DailySeries["total_lines"],
				core.TimePoint{Date: dateStr, Value: totalLines})
		}
	}
}

func formatTimestamp(s string) string {
	t := shared.FlexParseTime(s)
	if t.IsZero() {
		return s // return as-is if we can't parse
	}
	return t.Format("Jan 02, 2006 15:04 MST")
}
