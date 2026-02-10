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

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"

	maxGenerationsToFetch = 200
	generationPageSize    = 100
)

type creditsResponse struct {
	Data struct {
		Label      string   `json:"label"`
		Usage      float64  `json:"usage"`
		Limit      *float64 `json:"limit"`
		IsFreeTier bool     `json:"is_free_tier"`
		RateLimit  struct {
			Requests int    `json:"requests"`
			Interval string `json:"interval"`
		} `json:"rate_limit"`
	} `json:"data"`
}

type creditsDetailResponse struct {
	Data struct {
		TotalCredits     float64 `json:"total_credits"`
		TotalUsage       float64 `json:"total_usage"`
		RemainingBalance float64 `json:"remaining_balance"`
	} `json:"data"`
}

type generationEntry struct {
	ID                     string   `json:"id"`
	Model                  string   `json:"model"`
	TotalCost              float64  `json:"total_cost"`
	PromptTokens           int      `json:"tokens_prompt"`
	CompletionTokens       int      `json:"tokens_completion"`
	NativePromptTokens     *int     `json:"native_tokens_prompt"`
	NativeCompletionTokens *int     `json:"native_tokens_completion"`
	CreatedAt              string   `json:"created_at"`
	Streamed               bool     `json:"streamed"`
	GenerationTime         *int     `json:"generation_time"`
	Latency                *int     `json:"latency"`
	ProviderName           string   `json:"provider_name"`
	CacheDiscount          *float64 `json:"cache_discount"`
	Origin                 string   `json:"origin"`
	AppID                  *int     `json:"app_id"`
	NumMediaPrompt         *int     `json:"num_media_prompt"`
	NumMediaCompletion     *int     `json:"num_media_completion"`
	Finish                 string   `json:"finish_reason"`
	UpstreamID             string   `json:"upstream_id"`
	ModerationLatency      *int     `json:"moderation_latency"`
}

type generationStatsResponse struct {
	Data []generationEntry `json:"data"`
}

type modelStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	TotalLatencyMs   int
	LatencyCount     int
	CacheDiscountUSD float64
	Providers        map[string]int
}

type providerStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	Models           map[string]int
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "openrouter" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenRouter",
		Capabilities: []string{"credits_endpoint", "usage_endpoint", "generation_stats", "per_model_breakdown", "headers"},
		DocURL:       "https://openrouter.ai/docs/api-reference/limits",
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusAuth,
			Message:    fmt.Sprintf("env var %s not set", acct.APIKeyEnv),
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	if err := p.fetchAuthKey(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("auth/key error: %v", err)
		return snap, nil
	}

	if err := p.fetchCreditsDetail(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["credits_detail_error"] = err.Error()
	}

	if err := p.fetchGenerationStats(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["generation_error"] = err.Error()
	}

	return snap, nil
}

func (p *Provider) fetchAuthKey(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/auth/key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	snap.Raw = parsers.RedactHeaders(resp.Header)

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	var credits creditsResponse
	if err := json.Unmarshal(body, &credits); err != nil {
		snap.Status = core.StatusError
		snap.Message = "failed to parse credits response"
		return nil
	}

	usage := credits.Data.Usage
	if credits.Data.Limit != nil {
		remaining := *credits.Data.Limit - usage
		snap.Metrics["credits"] = core.Metric{
			Limit:     credits.Data.Limit,
			Used:      &usage,
			Remaining: &remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}
	} else {
		snap.Metrics["credits"] = core.Metric{
			Used:   &usage,
			Unit:   "USD",
			Window: "lifetime",
		}
	}

	if credits.Data.RateLimit.Requests > 0 {
		rl := float64(credits.Data.RateLimit.Requests)
		snap.Metrics["rpm"] = core.Metric{
			Limit:  &rl,
			Unit:   "requests",
			Window: credits.Data.RateLimit.Interval,
		}
	}

	if credits.Data.Label != "" {
		snap.Raw["key_label"] = credits.Data.Label
	}
	if credits.Data.IsFreeTier {
		snap.Raw["tier"] = "free"
	} else {
		snap.Raw["tier"] = "paid"
	}

	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm_headers", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm_headers", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("$%.4f used", usage)
	if credits.Data.Limit != nil {
		snap.Message += fmt.Sprintf(" / $%.2f limit", *credits.Data.Limit)
	}

	return nil
}

func (p *Provider) fetchCreditsDetail(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/credits"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var detail creditsDetailResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		return err
	}

	if detail.Data.TotalCredits > 0 {
		totalCredits := detail.Data.TotalCredits
		totalUsage := detail.Data.TotalUsage
		remaining := detail.Data.RemainingBalance

		snap.Metrics["credit_balance"] = core.Metric{
			Limit:     &totalCredits,
			Used:      &totalUsage,
			Remaining: &remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}
	}

	return nil
}

func (p *Provider) fetchGenerationStats(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	allGenerations, err := p.fetchAllGenerations(ctx, baseURL, apiKey)
	if err != nil {
		return err
	}

	if len(allGenerations) == 0 {
		snap.Raw["generations_fetched"] = "0"
		return nil
	}

	snap.Raw["generations_fetched"] = fmt.Sprintf("%d", len(allGenerations))

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	modelStatsMap := make(map[string]*modelStats)
	providerStatsMap := make(map[string]*providerStats)

	var todayPrompt, todayCompletion, todayRequests int
	var todayCost float64
	var todayLatencyMs, todayLatencyCount int
	var totalRequests int

	for _, g := range allGenerations {
		totalRequests++

		ts, err := time.Parse(time.RFC3339, g.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339Nano, g.CreatedAt)
			if err != nil {
				continue
			}
		}

		if !ts.After(todayStart) {
			continue
		}

		todayRequests++
		todayPrompt += g.PromptTokens
		todayCompletion += g.CompletionTokens
		todayCost += g.TotalCost

		if g.Latency != nil && *g.Latency > 0 {
			todayLatencyMs += *g.Latency
			todayLatencyCount++
		}

		modelKey := g.Model
		if modelKey == "" {
			modelKey = "unknown"
		}
		ms, ok := modelStatsMap[modelKey]
		if !ok {
			ms = &modelStats{Providers: make(map[string]int)}
			modelStatsMap[modelKey] = ms
		}
		ms.Requests++
		ms.PromptTokens += g.PromptTokens
		ms.CompletionTokens += g.CompletionTokens
		ms.TotalCost += g.TotalCost
		if g.Latency != nil && *g.Latency > 0 {
			ms.TotalLatencyMs += *g.Latency
			ms.LatencyCount++
		}
		if g.CacheDiscount != nil && *g.CacheDiscount > 0 {
			ms.CacheDiscountUSD += *g.CacheDiscount
		}
		if g.ProviderName != "" {
			ms.Providers[g.ProviderName]++
		}

		provKey := g.ProviderName
		if provKey == "" {
			provKey = "unknown"
		}
		ps, ok := providerStatsMap[provKey]
		if !ok {
			ps = &providerStats{Models: make(map[string]int)}
			providerStatsMap[provKey] = ps
		}
		ps.Requests++
		ps.PromptTokens += g.PromptTokens
		ps.CompletionTokens += g.CompletionTokens
		ps.TotalCost += g.TotalCost
		ps.Models[modelKey]++
	}

	if todayRequests > 0 {
		reqs := float64(todayRequests)
		snap.Metrics["today_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "today"}

		inp := float64(todayPrompt)
		snap.Metrics["today_input_tokens"] = core.Metric{Used: &inp, Unit: "tokens", Window: "today"}

		out := float64(todayCompletion)
		snap.Metrics["today_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "today"}

		snap.Metrics["today_cost"] = core.Metric{Used: &todayCost, Unit: "USD", Window: "today"}

		if todayLatencyCount > 0 {
			avgLatency := float64(todayLatencyMs) / float64(todayLatencyCount) / 1000.0
			snap.Metrics["today_avg_latency"] = core.Metric{Used: &avgLatency, Unit: "seconds", Window: "today"}
		}
	}

	reqs := float64(totalRequests)
	snap.Metrics["recent_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "recent"}

	emitPerModelMetrics(modelStatsMap, snap)
	emitPerProviderMetrics(providerStatsMap, snap)

	return nil
}

func (p *Provider) fetchAllGenerations(ctx context.Context, baseURL, apiKey string) ([]generationEntry, error) {
	var all []generationEntry
	offset := 0

	for offset < maxGenerationsToFetch {
		remaining := maxGenerationsToFetch - offset
		limit := generationPageSize
		if remaining < limit {
			limit = remaining
		}

		url := fmt.Sprintf("%s/generation?limit=%d&offset=%d", baseURL, limit, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return all, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return all, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return all, err
		}

		var gen generationStatsResponse
		if err := json.Unmarshal(body, &gen); err != nil {
			return all, err
		}

		all = append(all, gen.Data...)
		if len(gen.Data) < limit {
			break
		}
		offset += len(gen.Data)
	}

	return all, nil
}

func emitPerModelMetrics(modelStatsMap map[string]*modelStats, snap *core.QuotaSnapshot) {
	type entry struct {
		name  string
		stats *modelStats
	}
	sorted := make([]entry, 0, len(modelStatsMap))
	for name, stats := range modelStatsMap {
		sorted = append(sorted, entry{name, stats})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].stats.TotalCost > sorted[j].stats.TotalCost
	})

	for _, e := range sorted {
		safeName := sanitizeName(e.name)
		prefix := "model_" + safeName

		inputTokens := float64(e.stats.PromptTokens)
		snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &inputTokens, Unit: "tokens", Window: "today"}

		outputTokens := float64(e.stats.CompletionTokens)
		snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &outputTokens, Unit: "tokens", Window: "today"}

		costUSD := e.stats.TotalCost
		snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &costUSD, Unit: "USD", Window: "today"}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", e.stats.Requests)

		if e.stats.LatencyCount > 0 {
			avgMs := float64(e.stats.TotalLatencyMs) / float64(e.stats.LatencyCount)
			snap.Raw[prefix+"_avg_latency_ms"] = fmt.Sprintf("%.0f", avgMs)
		}

		if e.stats.CacheDiscountUSD > 0 {
			snap.Raw[prefix+"_cache_savings"] = fmt.Sprintf("$%.6f", e.stats.CacheDiscountUSD)
		}

		if len(e.stats.Providers) > 0 {
			var provList []string
			for prov := range e.stats.Providers {
				provList = append(provList, prov)
			}
			sort.Strings(provList)
			snap.Raw[prefix+"_providers"] = strings.Join(provList, ", ")
		}
	}
}

func emitPerProviderMetrics(providerStatsMap map[string]*providerStats, snap *core.QuotaSnapshot) {
	type entry struct {
		name  string
		stats *providerStats
	}
	sorted := make([]entry, 0, len(providerStatsMap))
	for name, stats := range providerStatsMap {
		sorted = append(sorted, entry{name, stats})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].stats.TotalCost > sorted[j].stats.TotalCost
	})

	for _, e := range sorted {
		prefix := "provider_" + sanitizeName(strings.ToLower(e.name))
		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", e.stats.Requests)
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", e.stats.TotalCost)
		snap.Raw[prefix+"_prompt_tokens"] = fmt.Sprintf("%d", e.stats.PromptTokens)
		snap.Raw[prefix+"_completion_tokens"] = fmt.Sprintf("%d", e.stats.CompletionTokens)
	}
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}
