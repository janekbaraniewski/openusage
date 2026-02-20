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
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"

	maxGenerationsToFetch = 500
	generationPageSize    = 100
	generationMaxAge      = 30 * 24 * time.Hour
)

// keyResponse represents the newer /key endpoint with detailed usage breakdowns
type keyResponse struct {
	Data struct {
		Label               string   `json:"label"`
		Limit               *float64 `json:"limit"`
		Usage               float64  `json:"usage"`
		UsageDaily          float64  `json:"usage_daily"`
		UsageWeekly         float64  `json:"usage_weekly"`
		UsageMonthly        float64  `json:"usage_monthly"`
		ByokUsage           float64  `json:"byok_usage"`
		ByokUsageDaily      float64  `json:"byok_usage_daily"`
		ByokUsageWeekly     float64  `json:"byok_usage_weekly"`
		ByokUsageMonthly    float64  `json:"byok_usage_monthly"`
		IsFreeTier          bool     `json:"is_free_tier"`
		IsManagementKey     bool     `json:"is_management_key"`
		IsProvisioningKey bool     `json:"is_provisioning_key"`
		LimitRemaining      *float64 `json:"limit_remaining"`
		LimitReset          *string  `json:"limit_reset"`
		IncludeByokInLimit  bool     `json:"include_byok_in_limit"`
		ExpiresAt           *string  `json:"expires_at"`
		RateLimit           struct {
			Requests int    `json:"requests"`
			Interval string `json:"interval"`
			Note     string `json:"note"`
		} `json:"rate_limit"`
	} `json:"data"`
}

// creditsResponse represents the /credits endpoint
type creditsResponse struct {
	Data struct {
		TotalCredits     float64 `json:"total_credits"`
		TotalUsage       float64 `json:"total_usage"`
		RemainingBalance float64 `json:"remaining_balance"`
	} `json:"data"`
}

// activityResponse represents the newer /activity endpoint
type activityResponse struct {
	Data []activityEntry `json:"data"`
}

type activityEntry struct {
	Date               string  `json:"date"`
	Model              string  `json:"model"`
	ModelPermaslug     string  `json:"model_permaslug"`
	EndpointID         string  `json:"endpoint_id"`
	ProviderName       string  `json:"provider_name"`
	Usage              float64 `json:"usage"`
	ByokUsageInference float64 `json:"byok_usage_inference"`
	Requests           float64 `json:"requests"`
	PromptTokens       float64 `json:"prompt_tokens"`
	CompletionTokens   float64 `json:"completion_tokens"`
	ReasoningTokens    float64 `json:"reasoning_tokens"`
}

// generationEntry represents a single generation with all available fields
type generationEntry struct {
	ID                           string             `json:"id"`
	UpstreamID                   *string            `json:"upstream_id"`
	TotalCost                    float64            `json:"total_cost"`
	CacheDiscount                *float64           `json:"cache_discount"`
	UpstreamInferenceCost        *float64           `json:"upstream_inference_cost"`
	CreatedAt                    string             `json:"created_at"`
	Model                        string             `json:"model"`
	AppID                        *int               `json:"app_id"`
	Streamed                     *bool              `json:"streamed"`
	Cancelled                    *bool              `json:"cancelled"`
	ProviderName                 *string            `json:"provider_name"`
	Latency                      *int               `json:"latency"`
	ModerationLatency            *int               `json:"moderation_latency"`
	GenerationTime               *int               `json:"generation_time"`
	FinishReason                 *string            `json:"finish_reason"`
	TokensPrompt                 *int               `json:"tokens_prompt"`
	TokensCompletion             *int               `json:"tokens_completion"`
	NativeTokensPrompt           *int               `json:"native_tokens_prompt"`
	NativeTokensCompletion       *int               `json:"native_tokens_completion"`
	NativeTokensCompletionImages *int               `json:"native_tokens_completion_images"`
	NativeTokensReasoning        *int               `json:"native_tokens_reasoning"`
	NativeTokensCached           *int               `json:"native_tokens_cached"`
	NumMediaPrompt               *int               `json:"num_media_prompt"`
	NumInputAudioPrompt          *int               `json:"num_input_audio_prompt"`
	NumMediaCompletion           *int               `json:"num_media_completion"`
	NumSearchResults             *int               `json:"num_search_results"`
	Origin                       string             `json:"origin"`
	Usage                        float64            `json:"usage"`
	IsByok                       bool               `json:"is_byok"`
	NativeFinishReason           *string            `json:"native_finish_reason"`
	ExternalUser                 *string            `json:"external_user"`
	APIType                      *string            `json:"api_type"`
	Router                       *string            `json:"router"`
	ProviderResponses            []providerResponse `json:"provider_responses"`
}

type providerResponse struct {
	ID             string   `json:"id"`
	EndpointID     string   `json:"endpoint_id"`
	ModelPermaslug string   `json:"model_permaslug"`
	ProviderName   string   `json:"provider_name"`
	Status         *float64 `json:"status"`
	Latency        float64  `json:"latency"`
	IsByok         bool     `json:"is_byok"`
}

type generationStatsResponse struct {
	Data []generationEntry `json:"data"`
}

// modelStats tracks per-model statistics
type modelStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
	ImageTokens      int
	TotalCost        float64
	ByokCost         float64
	TotalLatencyMs   int
	LatencyCount     int
	CacheDiscountUSD float64
	MediaPrompts     int
	AudioInputs      int
	MediaCompletions int
	SearchResults    int
	CancelledCount   int
	Providers        map[string]int
	Routers          map[string]int
	FinishReasons    map[string]int
}

// providerStats tracks per-provider statistics
type providerStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	Models           map[string]int
	FallbackAttempts int
}

// routerStats tracks per-router statistics
type routerStats struct {
	Requests int
	Cost     float64
}

// apiTypeStats tracks per-API type statistics
type apiTypeStats struct {
	Requests int
	Cost     float64
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "openrouter" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenRouter",
		Capabilities: []string{"credits_endpoint", "usage_endpoint", "generation_stats", "per_model_breakdown", "per_provider_breakdown", "byok_tracking", "reasoning_tokens", "cached_tokens", "media_tracking", "headers"},
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

	// Try the newer /key endpoint first (more detailed), fall back to /auth/key
	keyErr := p.fetchKey(ctx, baseURL, apiKey, &snap)
	if keyErr != nil {
		// Fall back to /auth/key
		if err := p.fetchAuthKey(ctx, baseURL, apiKey, &snap); err != nil {
			snap.Status = core.StatusError
			snap.Message = fmt.Sprintf("key error: %v", err)
			return snap, nil
		}
	}

	if err := p.fetchCredits(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["credits_error"] = err.Error()
	}

	snap.DailySeries = make(map[string][]core.TimePoint)

	// Try newer /activity endpoint first, fall back to /analytics/user-activity
	activityErr := p.fetchActivity(ctx, baseURL, apiKey, &snap)
	if activityErr != nil {
		if err := p.fetchAnalytics(ctx, baseURL, apiKey, &snap); err != nil {
			snap.Raw["activity_error"] = err.Error()
		}
	}

	if err := p.fetchGenerationStats(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["generation_error"] = err.Error()
	}

	return snap, nil
}

// fetchKey uses the newer /key endpoint with detailed usage breakdowns
func (p *Provider) fetchKey(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/key"
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

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("endpoint not available (HTTP %d)", resp.StatusCode)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		snap.Status = core.StatusAuth
		snap.Message = "HTTP 401 - check API key"
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	var keyData keyResponse
	if err := json.Unmarshal(body, &keyData); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	data := keyData.Data

	// Basic usage metric
	if data.Limit != nil {
		remaining := *data.Limit - data.Usage
		snap.Metrics["credits"] = core.Metric{
			Limit:     data.Limit,
			Used:      &data.Usage,
			Remaining: &remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}
	} else {
		snap.Metrics["credits"] = core.Metric{
			Used:   &data.Usage,
			Unit:   "USD",
			Window: "lifetime",
		}
	}

	// Time-bucketed usage metrics
	snap.Metrics["usage_daily"] = core.Metric{Used: &data.UsageDaily, Unit: "USD", Window: "today"}
	snap.Metrics["usage_weekly"] = core.Metric{Used: &data.UsageWeekly, Unit: "USD", Window: "week"}
	snap.Metrics["usage_monthly"] = core.Metric{Used: &data.UsageMonthly, Unit: "USD", Window: "month"}

	// BYOK usage metrics
	snap.Metrics["byok_usage"] = core.Metric{Used: &data.ByokUsage, Unit: "USD", Window: "lifetime"}
	snap.Metrics["byok_daily"] = core.Metric{Used: &data.ByokUsageDaily, Unit: "USD", Window: "today"}
	snap.Metrics["byok_weekly"] = core.Metric{Used: &data.ByokUsageWeekly, Unit: "USD", Window: "week"}
	snap.Metrics["byok_monthly"] = core.Metric{Used: &data.ByokUsageMonthly, Unit: "USD", Window: "month"}

	// Rate limit
	if data.RateLimit.Requests > 0 {
		rl := float64(data.RateLimit.Requests)
		snap.Metrics["rpm"] = core.Metric{
			Limit:  &rl,
			Unit:   "requests",
			Window: data.RateLimit.Interval,
		}
	}

	// Limit remaining
	if data.LimitRemaining != nil {
		snap.Metrics["limit_remaining"] = core.Metric{
			Remaining: data.LimitRemaining,
			Unit:      "USD",
			Window:    "current",
		}
	}

	// Raw metadata
	snap.Raw["key_label"] = data.Label
	if data.IsFreeTier {
		snap.Raw["tier"] = "free"
	} else {
		snap.Raw["tier"] = "paid"
	}
	if data.IsManagementKey {
		snap.Raw["key_type"] = "management"
	} else if data.IsProvisioningKey {
		snap.Raw["key_type"] = "provisioning"
	} else {
		snap.Raw["key_type"] = "standard"
	}
	if data.LimitReset != nil {
		snap.Raw["limit_reset"] = *data.LimitReset
	}
	if data.ExpiresAt != nil {
		snap.Raw["expires_at"] = *data.ExpiresAt
	}
	if data.RateLimit.Note != "" {
		snap.Raw["rate_limit_note"] = data.RateLimit.Note
	}
	snap.Raw["include_byok_in_limit"] = fmt.Sprintf("%v", data.IncludeByokInLimit)

	// Apply rate limit headers
	snap.Raw = parsers.RedactHeaders(resp.Header)
	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm_headers", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm_headers", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("$%.4f used (daily: $%.4f, weekly: $%.4f, monthly: $%.4f)",
		data.Usage, data.UsageDaily, data.UsageWeekly, data.UsageMonthly)

	return nil
}

// fetchAuthKey is the fallback to /auth/key endpoint
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

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d - check API key", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	var credits struct {
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

	snap.Raw["key_label"] = credits.Data.Label
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

func (p *Provider) fetchCredits(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
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

	var detail creditsResponse
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

// fetchActivity uses the newer /activity endpoint with reasoning tokens and BYOK tracking
func (p *Provider) fetchActivity(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/activity"
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

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("activity endpoint not available (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var activity activityResponse
	if err := json.Unmarshal(body, &activity); err != nil {
		return fmt.Errorf("parsing activity: %w", err)
	}

	costByDate := make(map[string]float64)
	tokensByDate := make(map[string]float64)
	reasoningByDate := make(map[string]float64)
	byokByDate := make(map[string]float64)

	for _, entry := range activity.Data {
		if entry.Date == "" {
			continue
		}
		costByDate[entry.Date] += entry.Usage
		tokensByDate[entry.Date] += entry.PromptTokens + entry.CompletionTokens
		reasoningByDate[entry.Date] += entry.ReasoningTokens
		byokByDate[entry.Date] += entry.ByokUsageInference
	}

	if len(costByDate) > 0 {
		snap.DailySeries["activity_cost"] = mapToSortedTimePoints(costByDate)
	}
	if len(tokensByDate) > 0 {
		snap.DailySeries["activity_tokens"] = mapToSortedTimePoints(tokensByDate)
	}
	if len(reasoningByDate) > 0 {
		snap.DailySeries["activity_reasoning_tokens"] = mapToSortedTimePoints(reasoningByDate)
	}
	if len(byokByDate) > 0 {
		snap.DailySeries["activity_byok_cost"] = mapToSortedTimePoints(byokByDate)
	}

	return nil
}

// fetchAnalytics is the fallback to the older /analytics/user-activity endpoint
func (p *Provider) fetchAnalytics(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/analytics/user-activity"
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

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("analytics endpoint not available (HTTP 404)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var analytics struct {
		Data []struct {
			Date        string  `json:"date"`
			Model       string  `json:"model"`
			TotalCost   float64 `json:"total_cost"`
			TotalTokens int     `json:"total_tokens"`
			Requests    int     `json:"requests"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &analytics); err != nil {
		return fmt.Errorf("parsing analytics: %w", err)
	}

	costByDate := make(map[string]float64)
	tokensByDate := make(map[string]float64)
	for _, entry := range analytics.Data {
		if entry.Date == "" {
			continue
		}
		costByDate[entry.Date] += entry.TotalCost
		tokensByDate[entry.Date] += float64(entry.TotalTokens)
	}

	if len(costByDate) > 0 {
		snap.DailySeries["analytics_cost"] = mapToSortedTimePoints(costByDate)
	}
	if len(tokensByDate) > 0 {
		snap.DailySeries["analytics_tokens"] = mapToSortedTimePoints(tokensByDate)
	}

	return nil
}

func mapToSortedTimePoints(m map[string]float64) []core.TimePoint {
	points := make([]core.TimePoint, 0, len(m))
	for date, val := range m {
		points = append(points, core.TimePoint{Date: date, Value: val})
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Date < points[j].Date
	})
	return points
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
	sevenDaysAgo := now.AddDate(0, 0, -7)
	burnCutoff := now.Add(-60 * time.Minute)

	modelStatsMap := make(map[string]*modelStats)
	providerStatsMap := make(map[string]*providerStats)
	routerStatsMap := make(map[string]*routerStats)
	apiTypeStatsMap := make(map[string]*apiTypeStats)

	var todayPrompt, todayCompletion, todayRequests int
	var todayCost, todayByokCost float64
	var todayLatencyMs, todayLatencyCount int
	var todayReasoningTokens, todayCachedTokens int
	var todayMediaPrompts, todayAudioInputs, todayMediaCompletions int
	var todayCancelled int
	var totalRequests int

	var cost7d, cost30d, burnCost float64
	var byokCost7d, byokCost30d float64

	dailyCost := make(map[string]float64)
	dailyByokCost := make(map[string]float64)
	dailyRequests := make(map[string]float64)
	dailyModelTokens := make(map[string]map[string]float64)
	dailyModelReasoning := make(map[string]map[string]float64)

	for _, g := range allGenerations {
		totalRequests++

		ts, err := time.Parse(time.RFC3339, g.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339Nano, g.CreatedAt)
			if err != nil {
				continue
			}
		}

		// Period cost aggregation
		cost30d += g.TotalCost
		if g.IsByok && g.UpstreamInferenceCost != nil {
			byokCost30d += *g.UpstreamInferenceCost
		}
		if ts.After(sevenDaysAgo) {
			cost7d += g.TotalCost
			if g.IsByok && g.UpstreamInferenceCost != nil {
				byokCost7d += *g.UpstreamInferenceCost
			}
		}

		// Burn rate: last 60 minutes
		if ts.After(burnCutoff) {
			burnCost += g.TotalCost
		}

		// Daily aggregation
		dateKey := ts.UTC().Format("2006-01-02")
		dailyCost[dateKey] += g.TotalCost
		if g.IsByok && g.UpstreamInferenceCost != nil {
			dailyByokCost[dateKey] += *g.UpstreamInferenceCost
		}
		dailyRequests[dateKey]++

		modelKey := g.Model
		if modelKey == "" {
			modelKey = "unknown"
		}
		if _, ok := dailyModelTokens[modelKey]; !ok {
			dailyModelTokens[modelKey] = make(map[string]float64)
			dailyModelReasoning[modelKey] = make(map[string]float64)
		}
		if g.TokensPrompt != nil && g.TokensCompletion != nil {
			dailyModelTokens[modelKey][dateKey] += float64(*g.TokensPrompt + *g.TokensCompletion)
		}
		if g.NativeTokensReasoning != nil {
			dailyModelReasoning[modelKey][dateKey] += float64(*g.NativeTokensReasoning)
		}

		if !ts.After(todayStart) {
			continue
		}

		// Today-only stats
		todayRequests++
		if g.TokensPrompt != nil {
			todayPrompt += *g.TokensPrompt
		}
		if g.TokensCompletion != nil {
			todayCompletion += *g.TokensCompletion
		}
		todayCost += g.TotalCost
		if g.IsByok && g.UpstreamInferenceCost != nil {
			todayByokCost += *g.UpstreamInferenceCost
		}

		if g.Latency != nil && *g.Latency > 0 {
			todayLatencyMs += *g.Latency
			todayLatencyCount++
		}
		if g.NativeTokensReasoning != nil {
			todayReasoningTokens += *g.NativeTokensReasoning
		}
		if g.NativeTokensCached != nil {
			todayCachedTokens += *g.NativeTokensCached
		}
		if g.NumMediaPrompt != nil {
			todayMediaPrompts += *g.NumMediaPrompt
		}
		if g.NumInputAudioPrompt != nil {
			todayAudioInputs += *g.NumInputAudioPrompt
		}
		if g.NumMediaCompletion != nil {
			todayMediaCompletions += *g.NumMediaCompletion
		}
		if g.Cancelled != nil && *g.Cancelled {
			todayCancelled++
		}

		// Per-model stats
		ms, ok := modelStatsMap[modelKey]
		if !ok {
			ms = &modelStats{Providers: make(map[string]int), Routers: make(map[string]int), FinishReasons: make(map[string]int)}
			modelStatsMap[modelKey] = ms
		}
		ms.Requests++
		if g.TokensPrompt != nil {
			ms.PromptTokens += *g.TokensPrompt
		}
		if g.TokensCompletion != nil {
			ms.CompletionTokens += *g.TokensCompletion
		}
		if g.NativeTokensReasoning != nil {
			ms.ReasoningTokens += *g.NativeTokensReasoning
		}
		if g.NativeTokensCached != nil {
			ms.CachedTokens += *g.NativeTokensCached
		}
		if g.NativeTokensCompletionImages != nil {
			ms.ImageTokens += *g.NativeTokensCompletionImages
		}
		ms.TotalCost += g.TotalCost
		if g.IsByok && g.UpstreamInferenceCost != nil {
			ms.ByokCost += *g.UpstreamInferenceCost
		}
		if g.Latency != nil && *g.Latency > 0 {
			ms.TotalLatencyMs += *g.Latency
			ms.LatencyCount++
		}
		if g.CacheDiscount != nil && *g.CacheDiscount > 0 {
			ms.CacheDiscountUSD += *g.CacheDiscount
		}
		if g.NumMediaPrompt != nil {
			ms.MediaPrompts += *g.NumMediaPrompt
		}
		if g.NumInputAudioPrompt != nil {
			ms.AudioInputs += *g.NumInputAudioPrompt
		}
		if g.NumMediaCompletion != nil {
			ms.MediaCompletions += *g.NumMediaCompletion
		}
		if g.Cancelled != nil && *g.Cancelled {
			ms.CancelledCount++
		}
		if g.ProviderName != nil && *g.ProviderName != "" {
			ms.Providers[*g.ProviderName]++
		}
		if g.Router != nil && *g.Router != "" {
			ms.Routers[*g.Router]++
		}
		if g.FinishReason != nil && *g.FinishReason != "" {
			ms.FinishReasons[*g.FinishReason]++
		}

		// Per-provider stats
		provKey := "unknown"
		if g.ProviderName != nil && *g.ProviderName != "" {
			provKey = *g.ProviderName
		}
		ps, ok := providerStatsMap[provKey]
		if !ok {
			ps = &providerStats{Models: make(map[string]int)}
			providerStatsMap[provKey] = ps
		}
		ps.Requests++
		if g.TokensPrompt != nil {
			ps.PromptTokens += *g.TokensPrompt
		}
		if g.TokensCompletion != nil {
			ps.CompletionTokens += *g.TokensCompletion
		}
		ps.TotalCost += g.TotalCost
		ps.Models[modelKey]++
		ps.FallbackAttempts += len(g.ProviderResponses) - 1

		// Per-router stats
		if g.Router != nil && *g.Router != "" {
			rs, ok := routerStatsMap[*g.Router]
			if !ok {
				rs = &routerStats{}
				routerStatsMap[*g.Router] = rs
			}
			rs.Requests++
			rs.Cost += g.TotalCost
		}

		// Per-API type stats
		apiType := "unknown"
		if g.APIType != nil && *g.APIType != "" {
			apiType = *g.APIType
		}
		ats, ok := apiTypeStatsMap[apiType]
		if !ok {
			ats = &apiTypeStats{}
			apiTypeStatsMap[apiType] = ats
		}
		ats.Requests++
		ats.Cost += g.TotalCost
	}

	// Emit today's metrics
	if todayRequests > 0 {
		reqs := float64(todayRequests)
		snap.Metrics["today_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "today"}

		inp := float64(todayPrompt)
		snap.Metrics["today_input_tokens"] = core.Metric{Used: &inp, Unit: "tokens", Window: "today"}

		out := float64(todayCompletion)
		snap.Metrics["today_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "today"}

		snap.Metrics["today_cost"] = core.Metric{Used: &todayCost, Unit: "USD", Window: "today"}

		if todayByokCost > 0 {
			snap.Metrics["today_byok_cost"] = core.Metric{Used: &todayByokCost, Unit: "USD", Window: "today"}
		}

		if todayLatencyCount > 0 {
			avgLatency := float64(todayLatencyMs) / float64(todayLatencyCount) / 1000.0
			snap.Metrics["today_avg_latency"] = core.Metric{Used: &avgLatency, Unit: "seconds", Window: "today"}
		}

		if todayReasoningTokens > 0 {
			rt := float64(todayReasoningTokens)
			snap.Metrics["today_reasoning_tokens"] = core.Metric{Used: &rt, Unit: "tokens", Window: "today"}
		}

		if todayCachedTokens > 0 {
			ct := float64(todayCachedTokens)
			snap.Metrics["today_cached_tokens"] = core.Metric{Used: &ct, Unit: "tokens", Window: "today"}
		}

		if todayMediaPrompts > 0 {
			mp := float64(todayMediaPrompts)
			snap.Metrics["today_media_prompts"] = core.Metric{Used: &mp, Unit: "items", Window: "today"}
		}

		if todayAudioInputs > 0 {
			ai := float64(todayAudioInputs)
			snap.Metrics["today_audio_inputs"] = core.Metric{Used: &ai, Unit: "items", Window: "today"}
		}

		if todayMediaCompletions > 0 {
			mc := float64(todayMediaCompletions)
			snap.Metrics["today_media_completions"] = core.Metric{Used: &mc, Unit: "items", Window: "today"}
		}

		if todayCancelled > 0 {
			canc := float64(todayCancelled)
			snap.Metrics["today_cancelled"] = core.Metric{Used: &canc, Unit: "requests", Window: "today"}
		}
	}

	reqs := float64(totalRequests)
	snap.Metrics["recent_requests"] = core.Metric{Used: &reqs, Unit: "requests", Window: "recent"}

	// Period cost metrics
	snap.Metrics["7d_api_cost"] = core.Metric{Used: &cost7d, Unit: "USD", Window: "7d"}
	snap.Metrics["30d_api_cost"] = core.Metric{Used: &cost30d, Unit: "USD", Window: "30d"}

	if byokCost7d > 0 {
		snap.Metrics["7d_byok_cost"] = core.Metric{Used: &byokCost7d, Unit: "USD", Window: "7d"}
	}
	if byokCost30d > 0 {
		snap.Metrics["30d_byok_cost"] = core.Metric{Used: &byokCost30d, Unit: "USD", Window: "30d"}
	}

	// Burn rate
	if burnCost > 0 {
		burnRate := burnCost
		dailyProjected := burnRate * 24
		snap.Metrics["burn_rate"] = core.Metric{Used: &burnRate, Unit: "USD/hour", Window: "1h"}
		snap.Metrics["daily_projected"] = core.Metric{Used: &dailyProjected, Unit: "USD", Window: "24h"}
	}

	// DailySeries
	snap.DailySeries["cost"] = mapToSortedTimePoints(dailyCost)
	if len(dailyByokCost) > 0 {
		snap.DailySeries["byok_cost"] = mapToSortedTimePoints(dailyByokCost)
	}
	snap.DailySeries["requests"] = mapToSortedTimePoints(dailyRequests)

	// Per-model token series (top 5 models by total tokens)
	type modelTokenTotal struct {
		model  string
		total  float64
		byDate map[string]float64
	}
	var modelTotals []modelTokenTotal
	for model, dateMap := range dailyModelTokens {
		var total float64
		for _, v := range dateMap {
			total += v
		}
		modelTotals = append(modelTotals, modelTokenTotal{model, total, dateMap})
	}
	sort.Slice(modelTotals, func(i, j int) bool {
		return modelTotals[i].total > modelTotals[j].total
	})
	topN := 5
	if len(modelTotals) < topN {
		topN = len(modelTotals)
	}
	for _, mt := range modelTotals[:topN] {
		key := "tokens_" + sanitizeName(mt.model)
		snap.DailySeries[key] = mapToSortedTimePoints(mt.byDate)
	}

	// Per-model reasoning token series
	var modelReasoningTotals []modelTokenTotal
	for model, dateMap := range dailyModelReasoning {
		var total float64
		for _, v := range dateMap {
			total += v
		}
		if total > 0 {
			modelReasoningTotals = append(modelReasoningTotals, modelTokenTotal{model, total, dateMap})
		}
	}
	sort.Slice(modelReasoningTotals, func(i, j int) bool {
		return modelReasoningTotals[i].total > modelReasoningTotals[j].total
	})
	topNR := 5
	if len(modelReasoningTotals) < topNR {
		topNR = len(modelReasoningTotals)
	}
	for _, mt := range modelReasoningTotals[:topNR] {
		key := "reasoning_tokens_" + sanitizeName(mt.model)
		snap.DailySeries[key] = mapToSortedTimePoints(mt.byDate)
	}

	emitPerModelMetrics(modelStatsMap, snap)
	emitPerProviderMetrics(providerStatsMap, snap)
	emitPerRouterMetrics(routerStatsMap, snap)
	emitPerAPITypeMetrics(apiTypeStatsMap, snap)

	return nil
}

func (p *Provider) fetchAllGenerations(ctx context.Context, baseURL, apiKey string) ([]generationEntry, error) {
	var all []generationEntry
	offset := 0
	cutoff := time.Now().Add(-generationMaxAge)

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

		hitCutoff := false
		for _, entry := range gen.Data {
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

		if hitCutoff || len(gen.Data) < limit {
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

		if e.stats.ReasoningTokens > 0 {
			rt := float64(e.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &rt, Unit: "tokens", Window: "today"}
		}

		if e.stats.CachedTokens > 0 {
			ct := float64(e.stats.CachedTokens)
			snap.Metrics[prefix+"_cached_tokens"] = core.Metric{Used: &ct, Unit: "tokens", Window: "today"}
		}

		if e.stats.ImageTokens > 0 {
			it := float64(e.stats.ImageTokens)
			snap.Metrics[prefix+"_image_tokens"] = core.Metric{Used: &it, Unit: "tokens", Window: "today"}
		}

		costUSD := e.stats.TotalCost
		snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &costUSD, Unit: "USD", Window: "today"}

		if e.stats.ByokCost > 0 {
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &e.stats.ByokCost, Unit: "USD", Window: "today"}
		}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", e.stats.Requests)

		if e.stats.LatencyCount > 0 {
			avgMs := float64(e.stats.TotalLatencyMs) / float64(e.stats.LatencyCount)
			snap.Raw[prefix+"_avg_latency_ms"] = fmt.Sprintf("%.0f", avgMs)
		}

		if e.stats.CacheDiscountUSD > 0 {
			snap.Raw[prefix+"_cache_savings"] = fmt.Sprintf("$%.6f", e.stats.CacheDiscountUSD)
		}

		if e.stats.MediaPrompts > 0 {
			snap.Raw[prefix+"_media_prompts"] = fmt.Sprintf("%d", e.stats.MediaPrompts)
		}

		if e.stats.AudioInputs > 0 {
			snap.Raw[prefix+"_audio_inputs"] = fmt.Sprintf("%d", e.stats.AudioInputs)
		}

		if e.stats.MediaCompletions > 0 {
			snap.Raw[prefix+"_media_completions"] = fmt.Sprintf("%d", e.stats.MediaCompletions)
		}

		if e.stats.CancelledCount > 0 {
			snap.Raw[prefix+"_cancelled"] = fmt.Sprintf("%d", e.stats.CancelledCount)
		}

		if len(e.stats.Providers) > 0 {
			var provList []string
			for prov := range e.stats.Providers {
				provList = append(provList, prov)
			}
			sort.Strings(provList)
			snap.Raw[prefix+"_providers"] = strings.Join(provList, ", ")
		}

		if len(e.stats.Routers) > 0 {
			var routerList []string
			for router := range e.stats.Routers {
				routerList = append(routerList, router)
			}
			sort.Strings(routerList)
			snap.Raw[prefix+"_routers"] = strings.Join(routerList, ", ")
		}

		if len(e.stats.FinishReasons) > 0 {
			var reasons []string
			for reason, count := range e.stats.FinishReasons {
				reasons = append(reasons, fmt.Sprintf("%s:%d", reason, count))
			}
			sort.Strings(reasons)
			snap.Raw[prefix+"_finish_reasons"] = strings.Join(reasons, ", ")
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
		if e.stats.FallbackAttempts > 0 {
			snap.Raw[prefix+"_fallback_attempts"] = fmt.Sprintf("%d", e.stats.FallbackAttempts)
		}
	}
}

func emitPerRouterMetrics(routerStatsMap map[string]*routerStats, snap *core.QuotaSnapshot) {
	for name, stats := range routerStatsMap {
		prefix := "router_" + sanitizeName(strings.ToLower(name))
		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", stats.Requests)
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", stats.Cost)
	}
}

func emitPerAPITypeMetrics(apiTypeStatsMap map[string]*apiTypeStats, snap *core.QuotaSnapshot) {
	for apiType, stats := range apiTypeStatsMap {
		prefix := "api_type_" + sanitizeName(strings.ToLower(apiType))
		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", stats.Requests)
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", stats.Cost)
	}
}

func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}