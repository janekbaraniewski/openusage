package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type generationEntry struct {
	ID                     string                       `json:"id"`
	Model                  string                       `json:"model"`
	TotalCost              float64                      `json:"total_cost"`
	Usage                  float64                      `json:"usage"`
	IsByok                 bool                         `json:"is_byok"`
	UpstreamInferenceCost  *float64                     `json:"upstream_inference_cost"`
	Cancelled              bool                         `json:"cancelled"`
	PromptTokens           int                          `json:"tokens_prompt"`
	CompletionTokens       int                          `json:"tokens_completion"`
	NativePromptTokens     *int                         `json:"native_tokens_prompt"`
	NativeCompletionTokens *int                         `json:"native_tokens_completion"`
	NativeReasoningTokens  *int                         `json:"native_tokens_reasoning"`
	NativeCachedTokens     *int                         `json:"native_tokens_cached"`
	NativeImageTokens      *int                         `json:"native_tokens_completion_images"`
	CreatedAt              string                       `json:"created_at"`
	Streamed               bool                         `json:"streamed"`
	GenerationTime         *int                         `json:"generation_time"`
	Latency                *int                         `json:"latency"`
	ProviderName           string                       `json:"provider_name"`
	Provider               string                       `json:"provider"`
	ProviderID             string                       `json:"provider_id"`
	ProviderSlug           string                       `json:"provider_slug"`
	UpstreamProvider       string                       `json:"upstream_provider"`
	UpstreamProviderName   string                       `json:"upstream_provider_name"`
	CacheDiscount          *float64                     `json:"cache_discount"`
	Origin                 string                       `json:"origin"`
	AppID                  *int                         `json:"app_id"`
	NumMediaPrompt         *int                         `json:"num_media_prompt"`
	NumMediaCompletion     *int                         `json:"num_media_completion"`
	NumInputAudioPrompt    *int                         `json:"num_input_audio_prompt"`
	NumSearchResults       *int                         `json:"num_search_results"`
	Finish                 string                       `json:"finish_reason"`
	NativeFinish           string                       `json:"native_finish_reason"`
	UpstreamID             string                       `json:"upstream_id"`
	ModerationLatency      *int                         `json:"moderation_latency"`
	ExternalUser           string                       `json:"external_user"`
	APIType                string                       `json:"api_type"`
	Router                 string                       `json:"router"`
	ProviderResponses      []generationProviderResponse `json:"provider_responses"`
}

type generationProviderResponse struct {
	ProviderName string `json:"provider_name"`
	Provider     string `json:"provider"`
	ProviderID   string `json:"provider_id"`
	Status       *int   `json:"status"`
}

type generationStatsResponse struct {
	Data []generationEntry `json:"data"`
}

type generationDetailResponse struct {
	Data generationEntry `json:"data"`
}

func (p *Provider) fetchGenerationStats(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	allGenerations, err := p.fetchAllGenerations(ctx, baseURL, apiKey)
	if err != nil {
		if errors.Is(err, errGenerationListUnsupported) {
			snap.Raw["generation_note"] = "generation list endpoint unavailable without IDs"
			snap.Raw["generations_fetched"] = "0"
			return nil
		}
		return err
	}

	if len(allGenerations) == 0 {
		snap.Raw["generations_fetched"] = "0"
		return nil
	}

	detailLookups, detailHits := p.enrichGenerationProviderMetadata(ctx, baseURL, apiKey, allGenerations)
	if detailLookups > 0 {
		snap.Raw["generation_provider_detail_lookups"] = fmt.Sprintf("%d", detailLookups)
		snap.Raw["generation_provider_detail_hits"] = fmt.Sprintf("%d", detailHits)
	}

	snap.Raw["generations_fetched"] = fmt.Sprintf("%d", len(allGenerations))

	now := p.now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	burnCutoff := now.Add(-60 * time.Minute)

	modelStatsMap := make(map[string]*modelStats)
	providerStatsMap := make(map[string]*providerStats)

	var todayPrompt, todayCompletion, todayRequests int
	var todayNativePrompt, todayNativeCompletion int
	var todayReasoning, todayCached, todayImageTokens int
	var todayMediaPrompt, todayMediaCompletion, todayAudioInputs, todaySearchResults, todayCancelled int
	var todayStreamed int
	var todayCost float64
	var todayLatencyMs, todayLatencyCount int
	var todayGenerationMs, todayGenerationCount int
	var todayModerationMs, todayModerationCount int
	var totalRequests int
	totalCancelled := 0
	apiTypeCountsToday := make(map[string]int)
	finishReasonCounts := make(map[string]int)
	originCounts := make(map[string]int)
	routerCounts := make(map[string]int)

	var cost7d, cost30d, burnCost float64
	var todayByokCost, cost7dByok, cost30dByok float64

	dailyCost := make(map[string]float64)
	dailyRequests := make(map[string]float64)
	dailyProviderTokens := make(map[string]map[string]float64)
	dailyProviderRequests := make(map[string]map[string]float64)
	dailyModelTokens := make(map[string]map[string]float64)
	providerResolutionCounts := make(map[providerResolutionSource]int)

	for _, generation := range allGenerations {
		totalRequests++
		generationCost := generation.TotalCost
		if generationCost == 0 && generation.Usage > 0 {
			generationCost = generation.Usage
		}

		if generation.Cancelled {
			totalCancelled++
		}

		ts, err := time.Parse(time.RFC3339, generation.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339Nano, generation.CreatedAt)
			if err != nil {
				continue
			}
		}

		cost30d += generationCost
		if ts.After(sevenDaysAgo) {
			cost7d += generationCost
		}
		byokCost := generationByokCost(generation)
		cost30dByok += byokCost
		if ts.After(sevenDaysAgo) {
			cost7dByok += byokCost
		}
		if ts.After(burnCutoff) {
			burnCost += generationCost
		}

		dateKey := ts.UTC().Format("2006-01-02")
		dailyCost[dateKey] += generationCost
		dailyRequests[dateKey]++

		modelKey := normalizeModelName(generation.Model)
		if modelKey == "" {
			modelKey = "unknown"
		}
		if _, ok := dailyModelTokens[modelKey]; !ok {
			dailyModelTokens[modelKey] = make(map[string]float64)
		}
		dailyModelTokens[modelKey][dateKey] += float64(generation.PromptTokens + generation.CompletionTokens)

		ms, ok := modelStatsMap[modelKey]
		if !ok {
			ms = &modelStats{Providers: make(map[string]int)}
			modelStatsMap[modelKey] = ms
		}
		ms.Requests++
		ms.PromptTokens += generation.PromptTokens
		ms.CompletionTokens += generation.CompletionTokens
		if generation.NativePromptTokens != nil {
			ms.NativePrompt += *generation.NativePromptTokens
		}
		if generation.NativeCompletionTokens != nil {
			ms.NativeCompletion += *generation.NativeCompletionTokens
		}
		if generation.NativeReasoningTokens != nil {
			ms.ReasoningTokens += *generation.NativeReasoningTokens
		}
		if generation.NativeCachedTokens != nil {
			ms.CachedTokens += *generation.NativeCachedTokens
		}
		if generation.NativeImageTokens != nil {
			ms.ImageTokens += *generation.NativeImageTokens
		}
		ms.TotalCost += generationCost
		if generation.Latency != nil && *generation.Latency > 0 {
			ms.TotalLatencyMs += *generation.Latency
			ms.LatencyCount++
		}
		if generation.GenerationTime != nil && *generation.GenerationTime > 0 {
			ms.TotalGenMs += *generation.GenerationTime
			ms.GenerationCount++
		}
		if generation.ModerationLatency != nil && *generation.ModerationLatency > 0 {
			ms.TotalModeration += *generation.ModerationLatency
			ms.ModerationCount++
		}
		if generation.CacheDiscount != nil && *generation.CacheDiscount > 0 {
			ms.CacheDiscountUSD += *generation.CacheDiscount
		}
		hostingProvider, source := resolveGenerationHostingProviderWithSource(generation)
		providerResolutionCounts[source]++
		if hostingProvider != "" {
			ms.Providers[hostingProvider]++
		}

		providerKey := hostingProvider
		if providerKey == "" {
			providerKey = "unknown"
		}
		providerClientKey := sanitizeName(strings.ToLower(providerKey))
		if dailyProviderTokens[providerClientKey] == nil {
			dailyProviderTokens[providerClientKey] = make(map[string]float64)
		}
		requestTokens := float64(generation.PromptTokens + generation.CompletionTokens)
		if generation.NativeReasoningTokens != nil {
			requestTokens += float64(*generation.NativeReasoningTokens)
		}
		dailyProviderTokens[providerClientKey][dateKey] += requestTokens
		if dailyProviderRequests[providerClientKey] == nil {
			dailyProviderRequests[providerClientKey] = make(map[string]float64)
		}
		dailyProviderRequests[providerClientKey][dateKey]++

		ps, ok := providerStatsMap[providerKey]
		if !ok {
			ps = &providerStats{Models: make(map[string]int)}
			providerStatsMap[providerKey] = ps
		}
		ps.Requests++
		ps.PromptTokens += generation.PromptTokens
		ps.CompletionTokens += generation.CompletionTokens
		if generation.NativeReasoningTokens != nil {
			ps.ReasoningTokens += *generation.NativeReasoningTokens
		}
		ps.ByokCost += byokCost
		ps.TotalCost += generationCost
		ps.Models[modelKey]++

		if !ts.After(todayStart) {
			continue
		}

		todayRequests++
		todayPrompt += generation.PromptTokens
		todayCompletion += generation.CompletionTokens
		if generation.NativePromptTokens != nil {
			todayNativePrompt += *generation.NativePromptTokens
		}
		if generation.NativeCompletionTokens != nil {
			todayNativeCompletion += *generation.NativeCompletionTokens
		}
		todayCost += generationCost
		todayByokCost += byokCost
		if generation.Cancelled {
			todayCancelled++
		}
		if generation.Streamed {
			todayStreamed++
		}
		if generation.NativeReasoningTokens != nil {
			todayReasoning += *generation.NativeReasoningTokens
		}
		if generation.NativeCachedTokens != nil {
			todayCached += *generation.NativeCachedTokens
		}
		if generation.NativeImageTokens != nil {
			todayImageTokens += *generation.NativeImageTokens
		}
		if generation.NumMediaPrompt != nil {
			todayMediaPrompt += *generation.NumMediaPrompt
		}
		if generation.NumMediaCompletion != nil {
			todayMediaCompletion += *generation.NumMediaCompletion
		}
		if generation.NumInputAudioPrompt != nil {
			todayAudioInputs += *generation.NumInputAudioPrompt
		}
		if generation.NumSearchResults != nil {
			todaySearchResults += *generation.NumSearchResults
		}
		if generation.Latency != nil && *generation.Latency > 0 {
			todayLatencyMs += *generation.Latency
			todayLatencyCount++
		}
		if generation.GenerationTime != nil && *generation.GenerationTime > 0 {
			todayGenerationMs += *generation.GenerationTime
			todayGenerationCount++
		}
		if generation.ModerationLatency != nil && *generation.ModerationLatency > 0 {
			todayModerationMs += *generation.ModerationLatency
			todayModerationCount++
		}
		if generation.APIType != "" {
			apiTypeCountsToday[generation.APIType]++
		}
		if generation.Finish != "" {
			finishReasonCounts[generation.Finish]++
		}
		if generation.Origin != "" {
			originCounts[generation.Origin]++
		}
		if generation.Router != "" {
			routerCounts[generation.Router]++
		}
	}

	if todayRequests > 0 {
		reqs := float64(todayRequests)
		snap.Metrics["today_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "today"}
		inp := float64(todayPrompt)
		snap.Metrics["today_input_tokens"] = core.Metric{Used: &inp, Unit: "tokens", Window: "today"}
		out := float64(todayCompletion)
		snap.Metrics["today_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "today"}
		if todayNativePrompt > 0 {
			v := float64(todayNativePrompt)
			snap.Metrics["today_native_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "today"}
		}
		if todayNativeCompletion > 0 {
			v := float64(todayNativeCompletion)
			snap.Metrics["today_native_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "today"}
		}
		snap.Metrics["today_cost"] = core.Metric{Used: &todayCost, Unit: "USD", Window: "today"}
		if todayByokCost > 0 {
			snap.Metrics["today_byok_cost"] = core.Metric{Used: &todayByokCost, Unit: "USD", Window: "today"}
			snap.Raw["byok_in_use"] = "true"
		}
		if todayReasoning > 0 {
			v := float64(todayReasoning)
			snap.Metrics["today_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "today"}
		}
		if todayCached > 0 {
			v := float64(todayCached)
			snap.Metrics["today_cached_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "today"}
		}
		if todayImageTokens > 0 {
			v := float64(todayImageTokens)
			snap.Metrics["today_image_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "today"}
		}
		if todayMediaPrompt > 0 {
			v := float64(todayMediaPrompt)
			snap.Metrics["today_media_prompts"] = core.Metric{Used: &v, Unit: "count", Window: "today"}
		}
		if todayMediaCompletion > 0 {
			v := float64(todayMediaCompletion)
			snap.Metrics["today_media_completions"] = core.Metric{Used: &v, Unit: "count", Window: "today"}
		}
		if todayAudioInputs > 0 {
			v := float64(todayAudioInputs)
			snap.Metrics["today_audio_inputs"] = core.Metric{Used: &v, Unit: "count", Window: "today"}
		}
		if todaySearchResults > 0 {
			v := float64(todaySearchResults)
			snap.Metrics["today_search_results"] = core.Metric{Used: &v, Unit: "count", Window: "today"}
		}
		if todayCancelled > 0 {
			v := float64(todayCancelled)
			snap.Metrics["today_cancelled"] = core.Metric{Used: &v, Unit: "count", Window: "today"}
		}
		if todayStreamed > 0 {
			v := float64(todayStreamed)
			snap.Metrics["today_streamed_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
			pct := v / reqs * 100
			snap.Metrics["today_streamed_percent"] = core.Metric{Used: &pct, Unit: "%", Window: "today"}
		}
		if todayLatencyCount > 0 {
			avgLatency := float64(todayLatencyMs) / float64(todayLatencyCount) / 1000.0
			snap.Metrics["today_avg_latency"] = core.Metric{Used: &avgLatency, Unit: "seconds", Window: "today"}
		}
		if todayGenerationCount > 0 {
			avgGeneration := float64(todayGenerationMs) / float64(todayGenerationCount) / 1000.0
			snap.Metrics["today_avg_generation_time"] = core.Metric{Used: &avgGeneration, Unit: "seconds", Window: "today"}
		}
		if todayModerationCount > 0 {
			avgModeration := float64(todayModerationMs) / float64(todayModerationCount) / 1000.0
			snap.Metrics["today_avg_moderation_latency"] = core.Metric{Used: &avgModeration, Unit: "seconds", Window: "today"}
		}
	}

	for apiType, count := range apiTypeCountsToday {
		if count <= 0 {
			continue
		}
		v := float64(count)
		snap.Metrics["today_"+sanitizeName(apiType)+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
	}
	if len(finishReasonCounts) > 0 {
		snap.Raw["today_finish_reasons"] = summarizeTopCounts(finishReasonCounts, 4)
	}
	if len(originCounts) > 0 {
		snap.Raw["today_origins"] = summarizeTopCounts(originCounts, 3)
	}
	if len(routerCounts) > 0 {
		snap.Raw["today_routers"] = summarizeTopCounts(routerCounts, 3)
	}

	reqs := float64(totalRequests)
	snap.Metrics["recent_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "recent"}
	snap.Metrics["7d_api_cost"] = core.Metric{Used: &cost7d, Unit: "USD", Window: "7d"}
	snap.Metrics["30d_api_cost"] = core.Metric{Used: &cost30d, Unit: "USD", Window: "30d"}
	if cost7dByok > 0 {
		snap.Metrics["7d_byok_cost"] = core.Metric{Used: &cost7dByok, Unit: "USD", Window: "7d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if cost30dByok > 0 {
		snap.Metrics["30d_byok_cost"] = core.Metric{Used: &cost30dByok, Unit: "USD", Window: "30d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if burnCost > 0 {
		burnRate := burnCost
		dailyProjected := burnRate * 24
		snap.Metrics["burn_rate"] = core.Metric{Used: &burnRate, Unit: "USD/hour", Window: "1h"}
		snap.Metrics["daily_projected"] = core.Metric{Used: &dailyProjected, Unit: "USD", Window: "24h"}
	}

	snap.DailySeries["cost"] = core.SortedTimePoints(dailyCost)
	snap.DailySeries["requests"] = core.SortedTimePoints(dailyRequests)
	emitClientDailySeries(snap, dailyProviderTokens, dailyProviderRequests)

	type modelTokenTotal struct {
		model  string
		total  float64
		byDate map[string]float64
	}
	var modelTotals []modelTokenTotal
	for model, dateMap := range dailyModelTokens {
		var total float64
		for _, value := range dateMap {
			total += value
		}
		modelTotals = append(modelTotals, modelTokenTotal{model: model, total: total, byDate: dateMap})
	}
	sort.Slice(modelTotals, func(i, j int) bool {
		return modelTotals[i].total > modelTotals[j].total
	})
	topN := 5
	if len(modelTotals) < topN {
		topN = len(modelTotals)
	}
	for _, modelTotal := range modelTotals[:topN] {
		snap.DailySeries["tokens_"+sanitizeName(modelTotal.model)] = core.SortedTimePoints(modelTotal.byDate)
	}

	hasAnalyticsModelRows := strings.TrimSpace(snap.Raw["activity_rows"]) != "" && strings.TrimSpace(snap.Raw["activity_rows"]) != "0"
	if hasAnalyticsModelRows {
		if analyticsRowsStale(snap, p.now().UTC()) {
			snap.Raw["activity_rows_stale"] = "true"
		} else {
			snap.Raw["activity_rows_stale"] = "false"
		}
	}
	emitPerModelMetrics(modelStatsMap, snap)
	emitPerProviderMetrics(providerStatsMap, snap)
	snap.Raw["model_mix_source"] = "generation_live"
	if len(providerResolutionCounts) > 0 {
		summary := make(map[string]int, len(providerResolutionCounts))
		for key, value := range providerResolutionCounts {
			if value <= 0 {
				continue
			}
			summary[string(key)] = value
		}
		if txt := summarizeTopCounts(summary, 8); txt != "" {
			snap.Raw["provider_resolution"] = txt
		}
	}
	modelRequests := make(map[string]float64, len(modelStatsMap))
	for model, stats := range modelStatsMap {
		if stats == nil || stats.Requests <= 0 {
			continue
		}
		modelRequests[model] = float64(stats.Requests)
	}
	emitModelDerivedToolUsageMetrics(snap, modelRequests, "30d inferred", "inferred_from_model_requests")
	emitToolOutcomeMetrics(snap, totalRequests, totalCancelled, "30d")

	return nil
}

func analyticsRowsStale(snap *core.UsageSnapshot, now time.Time) bool {
	cachedAtRaw := strings.TrimSpace(snap.Raw["activity_cached_at"])
	if cachedAtRaw != "" {
		if t, err := time.Parse(time.RFC3339, cachedAtRaw); err == nil {
			return now.UTC().Sub(t.UTC()) > 10*time.Minute
		}
	}
	maxDateRaw := strings.TrimSpace(snap.Raw["activity_max_date"])
	if maxDateRaw == "" {
		if dateRange := strings.TrimSpace(snap.Raw["activity_date_range"]); dateRange != "" {
			if idx := strings.LastIndex(dateRange, ".."); idx >= 0 {
				maxDateRaw = strings.TrimSpace(dateRange[idx+2:])
			}
		}
	}
	if maxDateRaw == "" {
		return false
	}
	day, err := time.Parse("2006-01-02", maxDateRaw)
	if err != nil {
		return false
	}
	todayUTC := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return day.UTC().Before(todayUTC)
}

func (p *Provider) fetchAllGenerations(ctx context.Context, baseURL, apiKey string) ([]generationEntry, error) {
	var all []generationEntry
	offset := 0
	cutoff := p.now().UTC().Add(-generationMaxAge)

	for offset < maxGenerationsToFetch {
		remaining := maxGenerationsToFetch - offset
		limit := generationPageSize
		if remaining < limit {
			limit = remaining
		}

		endpoint := fmt.Sprintf("%s/generation?limit=%d&offset=%d", baseURL, limit, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return all, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.Client().Do(req)
		if err != nil {
			return all, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return all, err
		}
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusBadRequest {
				lowerBody := strings.ToLower(string(body))
				lowerMsg := strings.ToLower(parseAPIErrorMessage(body))
				if strings.Contains(lowerMsg, "expected string") && strings.Contains(lowerMsg, "id") {
					return all, errGenerationListUnsupported
				}
				hasID := strings.Contains(lowerBody, "\"id\"") || strings.Contains(lowerBody, "\\\"id\\\"") || strings.Contains(lowerBody, "for id")
				if strings.Contains(lowerBody, "expected string") && hasID {
					return all, errGenerationListUnsupported
				}
			}
			return all, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var generationStats generationStatsResponse
		if err := json.Unmarshal(body, &generationStats); err != nil {
			return all, err
		}

		hitCutoff := false
		for _, entry := range generationStats.Data {
			ts, err := time.Parse(time.RFC3339, entry.CreatedAt)
			if err != nil {
				ts, _ = time.Parse(time.RFC3339Nano, entry.CreatedAt)
			}
			if !ts.IsZero() && ts.Before(cutoff) {
				hitCutoff = true
				break
			}
			all = append(all, entry)
		}

		if hitCutoff || len(generationStats.Data) < limit {
			break
		}
		offset += len(generationStats.Data)
	}
	return all, nil
}

func (p *Provider) enrichGenerationProviderMetadata(ctx context.Context, baseURL, apiKey string, rows []generationEntry) (int, int) {
	attempts := 0
	hits := 0
	for i := range rows {
		if attempts >= maxGenerationProviderDetailLookups {
			break
		}
		if rows[i].ID == "" {
			continue
		}
		if providerNameFromResponses(rows[i].ProviderResponses) != "" {
			continue
		}
		if !isLikelyRouterClientProviderName(rows[i].ProviderName) && strings.TrimSpace(rows[i].ProviderName) != "" {
			continue
		}

		attempts++
		detail, err := p.fetchGenerationDetail(ctx, baseURL, apiKey, rows[i].ID)
		if err != nil {
			continue
		}
		resolvedBefore := resolveGenerationHostingProvider(rows[i])
		if len(detail.ProviderResponses) > 0 {
			rows[i].ProviderResponses = detail.ProviderResponses
		}
		if providerName := strings.TrimSpace(detail.ProviderName); providerName != "" {
			rows[i].ProviderName = providerName
		}
		if upstream := strings.TrimSpace(detail.UpstreamID); upstream != "" {
			rows[i].UpstreamID = upstream
		}
		if resolvedAfter := resolveGenerationHostingProvider(rows[i]); resolvedAfter != "" && resolvedAfter != resolvedBefore {
			hits++
		}
	}
	return attempts, hits
}

func (p *Provider) fetchGenerationDetail(ctx context.Context, baseURL, apiKey, generationID string) (generationEntry, error) {
	if strings.TrimSpace(generationID) == "" {
		return generationEntry{}, fmt.Errorf("missing generation id")
	}
	endpoint := fmt.Sprintf("%s/generation?id=%s", baseURL, url.QueryEscape(generationID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return generationEntry{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.Client().Do(req)
	if err != nil {
		return generationEntry{}, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return generationEntry{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return generationEntry{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var detail generationDetailResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		return generationEntry{}, err
	}
	return detail.Data, nil
}
