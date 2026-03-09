package cursor

import (
	"strconv"

	"github.com/janekbaraniewski/openusage/internal/core"
)

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

func (p *Provider) storeBillingMetricsCache(accountID string, snap *core.UsageSnapshot) {
	if accountID == "" {
		return
	}
	cached := make(map[string]core.Metric, len(billingMetricKeys))
	for _, key := range billingMetricKeys {
		if metric, ok := snap.Metrics[key]; ok {
			cached[key] = cloneMetric(metric)
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
	for key, metric := range cached.BillingMetrics {
		if _, exists := snap.Metrics[key]; !exists {
			snap.Metrics[key] = cloneMetric(metric)
		}
	}
}

func (p *Provider) ensureCreditGauges(accountID string, snap *core.UsageSnapshot) {
	if _, ok := snap.Metrics["plan_spend"]; ok {
		return
	}
	if _, ok := snap.Metrics["spend_limit"]; ok {
		return
	}

	var costUSD float64
	if metric, ok := snap.Metrics["billing_total_cost"]; ok && metric.Used != nil && *metric.Used > 0 {
		costUSD = *metric.Used
	} else if metric, ok := snap.Metrics["composer_cost"]; ok && metric.Used != nil && *metric.Used > 0 {
		costUSD = *metric.Used
	}
	if costUSD <= 0 {
		return
	}

	if _, ok := snap.Metrics["plan_total_spend_usd"]; !ok {
		snap.Metrics["plan_total_spend_usd"] = core.Metric{
			Used:   core.Float64Ptr(costUSD),
			Unit:   "USD",
			Window: "billing-cycle",
		}
	}

	var limitUSD float64
	if metric, ok := snap.Metrics["plan_included_amount"]; ok && metric.Used != nil && *metric.Used > 0 {
		limitUSD = *metric.Used
	}
	if limitUSD <= 0 {
		p.mu.RLock()
		if cached, ok := p.modelAggregationCache[accountID]; ok && cached.EffectiveLimitUSD > 0 {
			limitUSD = cached.EffectiveLimitUSD
		}
		p.mu.RUnlock()
	}
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
