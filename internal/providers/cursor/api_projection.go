package cursor

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) fetchFromAPI(ctx context.Context, token string, snap *core.UsageSnapshot) error {
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

		snap.Metrics["plan_spend"] = core.Metric{Used: &totalSpendDollars, Limit: &limitDollars, Unit: "USD", Window: "billing-cycle"}
		snap.Metrics["plan_included"] = core.Metric{Used: &includedDollars, Unit: "USD", Window: "billing-cycle"}
		snap.Metrics["plan_bonus"] = core.Metric{Used: &bonusDollars, Unit: "USD", Window: "billing-cycle"}

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
			snap.Metrics["individual_spend"] = core.Metric{Used: &individualDollars, Unit: "USD", Window: "billing-cycle"}

			teamTotalUsedDollars := pooledUsedDollars
			snap.Metrics["team_budget"] = core.Metric{Limit: &pooledLimitDollars, Used: &teamTotalUsedDollars, Unit: "USD", Window: "billing-cycle"}
			selfSpend := individualDollars
			snap.Metrics["team_budget_self"] = core.Metric{Used: &selfSpend, Unit: "USD", Window: "billing-cycle"}
			othersSpend := pooledUsedDollars - individualDollars
			if othersSpend < 0 {
				othersSpend = 0
			}
			snap.Metrics["team_budget_others"] = core.Metric{Used: &othersSpend, Unit: "USD", Window: "billing-cycle"}

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
			daysRemaining := cycleEnd.Sub(p.now()).Hours() / 24
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

		snap.Metrics["plan_total_spend_usd"] = core.Metric{Used: &totalSpendDollars, Limit: &limitDollars, Unit: "USD", Window: "billing-cycle"}
		if su.PooledLimit > 0 {
			pooledLimitDollars := su.PooledLimit / 100.0
			snap.Metrics["plan_limit_usd"] = core.Metric{Limit: &pooledLimitDollars, Unit: "USD", Window: "billing-cycle"}
		} else {
			snap.Metrics["plan_limit_usd"] = core.Metric{Limit: &limitDollars, Unit: "USD", Window: "billing-cycle"}
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
			snap.Metrics["plan_included_amount"] = core.Metric{Used: &planIncludedAmountUSD, Unit: "USD", Window: "billing-cycle"}

			if hasPeriodUsage && limitDollars <= 0 && su.PooledLimit <= 0 {
				effectiveLimit := planIncludedAmountUSD
				snap.Metrics["plan_spend"] = core.Metric{Used: &totalSpendDollars, Limit: &effectiveLimit, Unit: "USD", Window: "billing-cycle"}
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

	if profile.IsTeamMember && profile.TeamID > 0 {
		teamIDStr := fmt.Sprintf("%.0f", profile.TeamID)
		body := []byte(fmt.Sprintf(`{"teamId":"%s"}`, teamIDStr))
		var teamMembers teamMembersResp
		if err := p.callDashboardAPIWithBody(ctx, token, "GetTeamMembers", body, &teamMembers); err == nil {
			var activeCount int
			var memberNames []string
			var ownerCount int
			for _, member := range teamMembers.TeamMembers {
				if member.IsRemoved {
					continue
				}
				activeCount++
				memberNames = append(memberNames, member.Name)
				if strings.Contains(member.Role, "OWNER") {
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
		snap.Message = fmt.Sprintf("%s — $%.0f / $%.0f team spend ($%.0f remaining)", planName, pooledUsedDollars, pooledLimitDollars, pooledRemainingDollars)
	} else if limitDollars > 0 {
		snap.Message = fmt.Sprintf("%s — $%.2f / $%.0f plan spend", planName, totalSpendDollars, limitDollars)
	} else if planName != "" {
		snap.Message = fmt.Sprintf("%s — %s", planName, periodUsage.DisplayMessage)
	}

	p.storeBillingMetricsCache(snap.AccountID, snap)

	_, hasPlanSpend := snap.Metrics["plan_spend"]
	_, hasSpendLimit := snap.Metrics["spend_limit"]
	_, hasBillingTotal := snap.Metrics["billing_total_cost"]
	if !hasPlanSpend && !hasSpendLimit && !hasBillingTotal && !hasPeriodUsage && !aggApplied {
		return fmt.Errorf("all billing API endpoints failed")
	}

	return nil
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
			snap.Metrics[fmt.Sprintf("model_%s_cost", modelIntent)] = core.Metric{Used: &costDollars, Unit: "USD", Window: "billing-cycle"}
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
			snap.Metrics[fmt.Sprintf("model_%s_input_tokens", modelIntent)] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
			rec.InputTokens = core.Float64Ptr(parsed)
		}
		if parsed, ok := parseModelTokenCount(outputTokens); ok {
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_output_tokens", modelIntent)] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
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
			snap.Metrics[fmt.Sprintf("model_%s_cache_write_tokens", modelIntent)] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
		}
		if parsed, ok := parseModelTokenCount(cacheReadTokens); ok {
			cacheRead = parsed
			hasCacheRead = true
			v := parsed
			snap.Metrics[fmt.Sprintf("model_%s_cache_read_tokens", modelIntent)] = core.Metric{Used: &v, Unit: "tokens", Window: "billing-cycle"}
		}
		if hasCacheWrite || hasCacheRead {
			cached := cacheWrite + cacheRead
			snap.Metrics[fmt.Sprintf("model_%s_cached_tokens", modelIntent)] = core.Metric{Used: &cached, Unit: "tokens", Window: "billing-cycle"}
			rec.CachedTokens = core.Float64Ptr(cached)
		}

		if agg.TotalCents > 0 || inputTokens != "" || outputTokens != "" || cacheWriteTokens != "" || cacheReadTokens != "" {
			applied = true
			snap.AppendModelUsage(rec)
		}
	}
	return applied
}

func applyAggregationTotals(snap *core.UsageSnapshot, agg *aggregatedUsageResp) {
	if agg.TotalCostCents > 0 {
		totalCostUSD := agg.TotalCostCents / 100.0
		snap.Metrics["billing_total_cost"] = core.Metric{Used: &totalCostUSD, Unit: "USD", Window: "billing-cycle"}
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
