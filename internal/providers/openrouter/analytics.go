package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) fetchAnalytics(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	var analytics analyticsResponse
	var activityEndpoint string
	var activityCachedAt string
	forbiddenMsg := ""
	yesterdayUTC := p.now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	for _, endpoint := range []string{
		"/activity",
		"/activity?date=" + yesterdayUTC,
		"/analytics/user-activity",
		"/api/internal/v1/transaction-analytics?window=1mo",
	} {
		url := analyticsEndpointURL(baseURL, endpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Cache-Control", "no-cache, no-store, max-age=0")
		req.Header.Set("Pragma", "no-cache")

		resp, err := p.Client().Do(req)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			if endpoint == "/activity" && resp.StatusCode == http.StatusForbidden {
				msg := parseAPIErrorMessage(body)
				if msg == "" {
					msg = "activity endpoint requires management key"
				}
				forbiddenMsg = msg
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}

		parsed, cachedAt, ok, err := parseAnalyticsBody(body)
		if err != nil || !ok {
			continue
		}
		analytics = parsed
		activityEndpoint = endpoint
		activityCachedAt = cachedAt
		break
	}

	if activityEndpoint == "" {
		if forbiddenMsg != "" {
			return fmt.Errorf("%s (HTTP 403)", forbiddenMsg)
		}
		return fmt.Errorf("analytics endpoint not available (HTTP 404)")
	}

	snap.Raw["activity_endpoint"] = activityEndpoint
	if activityCachedAt != "" {
		snap.Raw["activity_cached_at"] = activityCachedAt
	}

	costByDate := make(map[string]float64)
	tokensByDate := make(map[string]float64)
	requestsByDate := make(map[string]float64)
	byokCostByDate := make(map[string]float64)
	reasoningTokensByDate := make(map[string]float64)
	cachedTokensByDate := make(map[string]float64)
	providerTokensByDate := make(map[string]map[string]float64)
	providerRequestsByDate := make(map[string]map[string]float64)
	modelCost := make(map[string]float64)
	modelByokCost := make(map[string]float64)
	modelInputTokens := make(map[string]float64)
	modelOutputTokens := make(map[string]float64)
	modelReasoningTokens := make(map[string]float64)
	modelCachedTokens := make(map[string]float64)
	modelTotalTokens := make(map[string]float64)
	modelRequests := make(map[string]float64)
	modelByokRequests := make(map[string]float64)
	providerCost := make(map[string]float64)
	providerByokCost := make(map[string]float64)
	providerInputTokens := make(map[string]float64)
	providerOutputTokens := make(map[string]float64)
	providerReasoningTokens := make(map[string]float64)
	providerRequests := make(map[string]float64)
	endpointStatsMap := make(map[string]*endpointStats)
	models := make(map[string]struct{})
	providers := make(map[string]struct{})
	endpoints := make(map[string]struct{})
	activeDays := make(map[string]struct{})

	now := p.now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	thirtyDaysAgo := now.AddDate(0, 0, -30)

	var totalCost, totalByok, totalRequests float64
	var totalInput, totalOutput, totalReasoning, totalCached, totalTokens float64
	var cost7d, byok7d, requests7d float64
	var input7d, output7d, reasoning7d, cached7d, tokens7d float64
	var todayByok, cost7dByok, cost30dByok float64
	var minDate, maxDate string

	for _, entry := range analytics.Data {
		if entry.Date == "" {
			continue
		}
		date, entryDate, hasParsedDate := normalizeActivityDate(entry.Date)

		cost := entry.Usage
		if cost == 0 {
			cost = entry.TotalCost
		}
		tokens := float64(entry.TotalTokens)
		if tokens == 0 {
			tokens = float64(entry.PromptTokens + entry.CompletionTokens + entry.ReasoningTokens)
		}
		inputTokens := float64(entry.PromptTokens)
		outputTokens := float64(entry.CompletionTokens)
		requests := float64(entry.Requests)
		byokCost := entry.ByokUsageInference
		byokRequests := float64(entry.ByokRequests)
		reasoningTokens := float64(entry.ReasoningTokens)
		cachedTokens := float64(entry.CachedTokens)
		modelName := normalizeModelName(entry.Model)
		if modelName == "" {
			modelName = normalizeModelName(entry.ModelPermaslug)
		}
		if modelName == "" {
			modelName = "unknown"
		}
		providerName := entry.ProviderName
		if providerName == "" {
			providerName = "unknown"
		}
		endpointID := strings.TrimSpace(entry.EndpointID)
		if endpointID == "" {
			endpointID = "unknown"
		}

		costByDate[date] += cost
		tokensByDate[date] += tokens
		requestsByDate[date] += requests
		byokCostByDate[date] += byokCost
		reasoningTokensByDate[date] += reasoningTokens
		cachedTokensByDate[date] += cachedTokens
		modelCost[modelName] += cost
		modelByokCost[modelName] += byokCost
		modelInputTokens[modelName] += inputTokens
		modelOutputTokens[modelName] += outputTokens
		modelReasoningTokens[modelName] += reasoningTokens
		modelCachedTokens[modelName] += cachedTokens
		modelTotalTokens[modelName] += tokens
		modelRequests[modelName] += requests
		modelByokRequests[modelName] += byokRequests
		providerCost[providerName] += cost
		providerByokCost[providerName] += byokCost
		providerInputTokens[providerName] += inputTokens
		providerOutputTokens[providerName] += outputTokens
		providerReasoningTokens[providerName] += reasoningTokens
		providerRequests[providerName] += requests
		providerClientKey := sanitizeName(strings.ToLower(providerName))
		if providerTokensByDate[providerClientKey] == nil {
			providerTokensByDate[providerClientKey] = make(map[string]float64)
		}
		providerTokensByDate[providerClientKey][date] += inputTokens + outputTokens + reasoningTokens
		if providerRequestsByDate[providerClientKey] == nil {
			providerRequestsByDate[providerClientKey] = make(map[string]float64)
		}
		providerRequestsByDate[providerClientKey][date] += requests

		stats := endpointStatsMap[endpointID]
		if stats == nil {
			stats = &endpointStats{Model: modelName, Provider: providerName}
			endpointStatsMap[endpointID] = stats
		}
		stats.Requests += entry.Requests
		stats.TotalCost += cost
		stats.ByokCost += byokCost
		stats.PromptTokens += entry.PromptTokens
		stats.CompletionTokens += entry.CompletionTokens
		stats.ReasoningTokens += entry.ReasoningTokens

		models[modelName] = struct{}{}
		providers[providerName] = struct{}{}
		if endpointID != "unknown" {
			endpoints[endpointID] = struct{}{}
		}
		activeDays[date] = struct{}{}

		if minDate == "" || date < minDate {
			minDate = date
		}
		if maxDate == "" || date > maxDate {
			maxDate = date
		}

		totalCost += cost
		totalByok += byokCost
		totalRequests += requests
		totalInput += inputTokens
		totalOutput += outputTokens
		totalReasoning += reasoningTokens
		totalCached += cachedTokens
		totalTokens += tokens

		if !hasParsedDate {
			continue
		}
		if !entryDate.Before(todayStart) {
			todayByok += byokCost
		}
		if entryDate.After(sevenDaysAgo) {
			cost7dByok += byokCost
		}
		if entryDate.After(thirtyDaysAgo) {
			cost30dByok += byokCost
		}
		if entryDate.After(sevenDaysAgo) {
			cost7d += cost
			byok7d += byokCost
			requests7d += requests
			input7d += inputTokens
			output7d += outputTokens
			reasoning7d += reasoningTokens
			cached7d += cachedTokens
			tokens7d += tokens
		}
	}

	snap.Raw["activity_rows"] = fmt.Sprintf("%d", len(analytics.Data))
	if minDate != "" && maxDate != "" {
		snap.Raw["activity_date_range"] = minDate + " .. " + maxDate
	}
	if minDate != "" {
		snap.Raw["activity_min_date"] = minDate
	}
	if maxDate != "" {
		snap.Raw["activity_max_date"] = maxDate
	}
	if len(models) > 0 {
		snap.Raw["activity_models"] = fmt.Sprintf("%d", len(models))
	}
	if len(providers) > 0 {
		snap.Raw["activity_providers"] = fmt.Sprintf("%d", len(providers))
	}
	if len(endpoints) > 0 {
		snap.Raw["activity_endpoints"] = fmt.Sprintf("%d", len(endpoints))
	}
	if len(activeDays) > 0 {
		snap.Raw["activity_days"] = fmt.Sprintf("%d", len(activeDays))
	}

	if len(costByDate) > 0 {
		snap.DailySeries["analytics_cost"] = core.SortedTimePoints(costByDate)
	}
	if len(tokensByDate) > 0 {
		snap.DailySeries["analytics_tokens"] = core.SortedTimePoints(tokensByDate)
	}
	if len(requestsByDate) > 0 {
		snap.DailySeries["analytics_requests"] = core.SortedTimePoints(requestsByDate)
	}
	if len(byokCostByDate) > 0 {
		snap.DailySeries["analytics_byok_cost"] = core.SortedTimePoints(byokCostByDate)
	}
	if len(reasoningTokensByDate) > 0 {
		snap.DailySeries["analytics_reasoning_tokens"] = core.SortedTimePoints(reasoningTokensByDate)
	}
	if len(cachedTokensByDate) > 0 {
		snap.DailySeries["analytics_cached_tokens"] = core.SortedTimePoints(cachedTokensByDate)
	}

	if totalCost > 0 {
		snap.Metrics["analytics_30d_cost"] = core.Metric{Used: &totalCost, Unit: "USD", Window: "30d"}
	}
	if totalByok > 0 {
		snap.Metrics["analytics_30d_byok_cost"] = core.Metric{Used: &totalByok, Unit: "USD", Window: "30d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if totalRequests > 0 {
		snap.Metrics["analytics_30d_requests"] = core.Metric{Used: &totalRequests, Unit: "requests", Window: "30d"}
	}
	if totalInput > 0 {
		snap.Metrics["analytics_30d_input_tokens"] = core.Metric{Used: &totalInput, Unit: "tokens", Window: "30d"}
	}
	if totalOutput > 0 {
		snap.Metrics["analytics_30d_output_tokens"] = core.Metric{Used: &totalOutput, Unit: "tokens", Window: "30d"}
	}
	if totalReasoning > 0 {
		snap.Metrics["analytics_30d_reasoning_tokens"] = core.Metric{Used: &totalReasoning, Unit: "tokens", Window: "30d"}
	}
	if totalCached > 0 {
		snap.Metrics["analytics_30d_cached_tokens"] = core.Metric{Used: &totalCached, Unit: "tokens", Window: "30d"}
	}
	if totalTokens > 0 {
		snap.Metrics["analytics_30d_tokens"] = core.Metric{Used: &totalTokens, Unit: "tokens", Window: "30d"}
	}

	if cost7d > 0 {
		snap.Metrics["analytics_7d_cost"] = core.Metric{Used: &cost7d, Unit: "USD", Window: "7d"}
	}
	if byok7d > 0 {
		snap.Metrics["analytics_7d_byok_cost"] = core.Metric{Used: &byok7d, Unit: "USD", Window: "7d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if requests7d > 0 {
		snap.Metrics["analytics_7d_requests"] = core.Metric{Used: &requests7d, Unit: "requests", Window: "7d"}
	}
	if input7d > 0 {
		snap.Metrics["analytics_7d_input_tokens"] = core.Metric{Used: &input7d, Unit: "tokens", Window: "7d"}
	}
	if output7d > 0 {
		snap.Metrics["analytics_7d_output_tokens"] = core.Metric{Used: &output7d, Unit: "tokens", Window: "7d"}
	}
	if reasoning7d > 0 {
		snap.Metrics["analytics_7d_reasoning_tokens"] = core.Metric{Used: &reasoning7d, Unit: "tokens", Window: "7d"}
	}
	if cached7d > 0 {
		snap.Metrics["analytics_7d_cached_tokens"] = core.Metric{Used: &cached7d, Unit: "tokens", Window: "7d"}
	}
	if tokens7d > 0 {
		snap.Metrics["analytics_7d_tokens"] = core.Metric{Used: &tokens7d, Unit: "tokens", Window: "7d"}
	}

	if days := len(activeDays); days > 0 {
		v := float64(days)
		snap.Metrics["analytics_active_days"] = core.Metric{Used: &v, Unit: "days", Window: "30d"}
	}
	if count := len(models); count > 0 {
		v := float64(count)
		snap.Metrics["analytics_models"] = core.Metric{Used: &v, Unit: "models", Window: "30d"}
	}
	if count := len(providers); count > 0 {
		v := float64(count)
		snap.Metrics["analytics_providers"] = core.Metric{Used: &v, Unit: "providers", Window: "30d"}
	}
	if count := len(endpoints); count > 0 {
		v := float64(count)
		snap.Metrics["analytics_endpoints"] = core.Metric{Used: &v, Unit: "endpoints", Window: "30d"}
	}

	emitAnalyticsPerModelMetrics(snap, modelCost, modelByokCost, modelInputTokens, modelOutputTokens, modelReasoningTokens, modelCachedTokens, modelTotalTokens, modelRequests, modelByokRequests)
	filterRouterClientProviders(providerCost, providerByokCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests)
	emitAnalyticsPerProviderMetrics(snap, providerCost, providerByokCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests)
	emitUpstreamProviderMetrics(snap, providerCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests)
	emitAnalyticsEndpointMetrics(snap, endpointStatsMap)
	for name := range providerTokensByDate {
		if isLikelyRouterClientProviderName(name) {
			delete(providerTokensByDate, name)
		}
	}
	for name := range providerRequestsByDate {
		if isLikelyRouterClientProviderName(name) {
			delete(providerRequestsByDate, name)
		}
	}
	emitClientDailySeries(snap, providerTokensByDate, providerRequestsByDate)
	emitModelDerivedToolUsageMetrics(snap, modelRequests, "30d inferred", "inferred_from_model_requests")

	if todayByok > 0 {
		snap.Metrics["today_byok_cost"] = core.Metric{Used: &todayByok, Unit: "USD", Window: "1d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if cost7dByok > 0 {
		snap.Metrics["7d_byok_cost"] = core.Metric{Used: &cost7dByok, Unit: "USD", Window: "7d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if cost30dByok > 0 {
		snap.Metrics["30d_byok_cost"] = core.Metric{Used: &cost30dByok, Unit: "USD", Window: "30d"}
		snap.Raw["byok_in_use"] = "true"
	}

	return nil
}

func analyticsEndpointURL(baseURL, endpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasPrefix(endpoint, "/api/internal/") && strings.HasSuffix(base, "/api/v1") {
		base = strings.TrimSuffix(base, "/api/v1")
	}
	return base + endpoint
}

func parseAnalyticsBody(body []byte) (analyticsResponse, string, bool, error) {
	var direct analyticsResponse
	if err := json.Unmarshal(body, &direct); err == nil && direct.Data != nil {
		return direct, "", true, nil
	}

	var wrapped analyticsEnvelopeResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data.Data != nil {
		return analyticsResponse{Data: wrapped.Data.Data}, parseAnalyticsCachedAt(wrapped.Data.CachedAt), true, nil
	}

	return analyticsResponse{}, "", false, fmt.Errorf("unrecognized analytics payload")
}

func parseAnalyticsCachedAt(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}

	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return s
	}

	sec := int64(n)
	if sec > 1_000_000_000_000 {
		sec /= 1000
	}
	if sec <= 0 {
		return fmt.Sprintf("%.0f", n)
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func normalizeActivityDate(raw string) (string, time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			date := t.UTC().Format("2006-01-02")
			return date, t.UTC(), true
		}
	}
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		date := raw[:10]
		if t, err := time.Parse("2006-01-02", date); err == nil {
			return date, t.UTC(), true
		}
		return date, time.Time{}, false
	}
	return raw, time.Time{}, false
}

func emitAnalyticsPerModelMetrics(
	snap *core.UsageSnapshot,
	modelCost, modelByokCost, modelInputTokens, modelOutputTokens, modelReasoningTokens, modelCachedTokens, modelTotalTokens, modelRequests, modelByokRequests map[string]float64,
) {
	modelSet := make(map[string]struct{})
	for model := range modelCost {
		modelSet[model] = struct{}{}
	}
	for model := range modelByokCost {
		modelSet[model] = struct{}{}
	}
	for model := range modelInputTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelOutputTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelReasoningTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelCachedTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelTotalTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelRequests {
		modelSet[model] = struct{}{}
	}
	for model := range modelByokRequests {
		modelSet[model] = struct{}{}
	}

	for model := range modelSet {
		safe := sanitizeName(model)
		prefix := "model_" + safe
		rec := core.ModelUsageRecord{RawModelID: model, RawSource: "api", Window: "activity"}

		if v := modelCost[model]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
			rec.CostUSD = core.Float64Ptr(v)
		}
		if v := modelByokCost[model]; v > 0 {
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := modelInputTokens[model]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.InputTokens = core.Float64Ptr(v)
		}
		if v := modelOutputTokens[model]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.OutputTokens = core.Float64Ptr(v)
		}
		if v := modelReasoningTokens[model]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.ReasoningTokens = core.Float64Ptr(v)
		}
		if v := modelCachedTokens[model]; v > 0 {
			snap.Metrics[prefix+"_cached_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.CachedTokens = core.Float64Ptr(v)
		}
		if v := modelTotalTokens[model]; v > 0 {
			snap.Metrics[prefix+"_total_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.TotalTokens = core.Float64Ptr(v)
		}
		if v := modelRequests[model]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
			snap.Raw[prefix+"_requests"] = fmt.Sprintf("%.0f", v)
			rec.Requests = core.Float64Ptr(v)
		}
		if v := modelByokRequests[model]; v > 0 {
			snap.Metrics[prefix+"_byok_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}
		if rec.InputTokens != nil || rec.OutputTokens != nil || rec.CostUSD != nil || rec.Requests != nil || rec.ReasoningTokens != nil || rec.CachedTokens != nil || rec.TotalTokens != nil {
			snap.AppendModelUsage(rec)
		}
	}
}

func filterRouterClientProviders(maps ...map[string]float64) {
	for _, metrics := range maps {
		for name := range metrics {
			if isLikelyRouterClientProviderName(name) {
				delete(metrics, name)
			}
		}
	}
}

func emitAnalyticsPerProviderMetrics(
	snap *core.UsageSnapshot,
	providerCost, providerByokCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests map[string]float64,
) {
	providerSet := make(map[string]struct{})
	for provider := range providerCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerByokCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerInputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerOutputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerReasoningTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerRequests {
		providerSet[provider] = struct{}{}
	}

	for provider := range providerSet {
		prefix := "provider_" + sanitizeName(strings.ToLower(provider))
		if v := providerCost[provider]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerByokCost[provider]; v > 0 {
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerInputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerOutputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerReasoningTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerRequests[provider]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%.0f", providerRequests[provider])
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", providerCost[provider])
		if providerByokCost[provider] > 0 {
			snap.Raw[prefix+"_byok_cost"] = fmt.Sprintf("$%.6f", providerByokCost[provider])
		}
		snap.Raw[prefix+"_prompt_tokens"] = fmt.Sprintf("%.0f", providerInputTokens[provider])
		snap.Raw[prefix+"_completion_tokens"] = fmt.Sprintf("%.0f", providerOutputTokens[provider])
		if providerReasoningTokens[provider] > 0 {
			snap.Raw[prefix+"_reasoning_tokens"] = fmt.Sprintf("%.0f", providerReasoningTokens[provider])
		}
	}
}

func emitUpstreamProviderMetrics(
	snap *core.UsageSnapshot,
	providerCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests map[string]float64,
) {
	providerSet := make(map[string]struct{})
	for provider := range providerCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerInputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerOutputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerReasoningTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerRequests {
		providerSet[provider] = struct{}{}
	}

	for provider := range providerSet {
		prefix := "upstream_" + sanitizeName(strings.ToLower(provider))
		if v := providerCost[provider]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerInputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerOutputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerReasoningTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerRequests[provider]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}
	}
}

func emitAnalyticsEndpointMetrics(snap *core.UsageSnapshot, endpointStatsMap map[string]*endpointStats) {
	type endpointEntry struct {
		id    string
		stats *endpointStats
	}

	var entries []endpointEntry
	for id, stats := range endpointStatsMap {
		if id == "unknown" {
			continue
		}
		entries = append(entries, endpointEntry{id: id, stats: stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].stats.TotalCost != entries[j].stats.TotalCost {
			return entries[i].stats.TotalCost > entries[j].stats.TotalCost
		}
		return entries[i].stats.Requests > entries[j].stats.Requests
	})

	const maxEndpointMetrics = 8
	if len(entries) > maxEndpointMetrics {
		entries = entries[:maxEndpointMetrics]
	}
	for _, entry := range entries {
		prefix := "endpoint_" + sanitizeName(entry.id)

		if req := float64(entry.stats.Requests); req > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &req, Unit: "requests", Window: "activity"}
		}
		if entry.stats.TotalCost > 0 {
			v := entry.stats.TotalCost
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if entry.stats.ByokCost > 0 {
			v := entry.stats.ByokCost
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if entry.stats.PromptTokens > 0 {
			v := float64(entry.stats.PromptTokens)
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.CompletionTokens > 0 {
			v := float64(entry.stats.CompletionTokens)
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.ReasoningTokens > 0 {
			v := float64(entry.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.Provider != "" {
			snap.Raw[prefix+"_provider"] = entry.stats.Provider
		}
		if entry.stats.Model != "" {
			snap.Raw[prefix+"_model"] = entry.stats.Model
		}
	}
}

func parseAPIErrorMessage(body []byte) string {
	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return ""
	}
	return strings.TrimSpace(apiErr.Error.Message)
}
