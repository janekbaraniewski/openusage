// Package openrouter implements a QuotaProvider for OpenRouter.
//
// OpenRouter provides several quota/usage endpoints:
//
//   - GET /api/v1/auth/key — API key info: credits, usage, limits, free_tier
//   - GET /api/v1/credits — detailed credit balance and history
//   - GET /api/v1/generation — generation history with per-request token counts,
//     costs, models, providers, latencies, and cache discounts
//
// All responses also include rate-limit headers:
//
//	Request limits:
//	  x-ratelimit-limit-requests / x-ratelimit-remaining-requests / x-ratelimit-reset-requests
//	Token limits:
//	  x-ratelimit-limit-tokens / x-ratelimit-remaining-tokens / x-ratelimit-reset-tokens
package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"

	// Maximum generations to fetch for per-model breakdown.
	// OpenRouter paginates via limit+offset; we fetch in pages.
	maxGenerationsToFetch = 200
	generationPageSize    = 100
)

// ---------------------------------------------------------------------------
// API response types
// ---------------------------------------------------------------------------

// creditsResponse is the JSON returned by /api/v1/auth/key.
type creditsResponse struct {
	Data struct {
		Label      string   `json:"label"`
		Usage      float64  `json:"usage"` // USD spent
		Limit      *float64 `json:"limit"` // USD limit (null = unlimited)
		IsFreeTier bool     `json:"is_free_tier"`
		RateLimit  struct {
			Requests int    `json:"requests"`
			Interval string `json:"interval"`
		} `json:"rate_limit"`
	} `json:"data"`
}

// creditsDetailResponse is the JSON returned by /api/v1/credits.
type creditsDetailResponse struct {
	Data struct {
		TotalCredits     float64 `json:"total_credits"`
		TotalUsage       float64 `json:"total_usage"`
		RemainingBalance float64 `json:"remaining_balance"`
	} `json:"data"`
}

// generationEntry represents a single generation from /api/v1/generation.
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
	GenerationTime         *int     `json:"generation_time"` // ms
	Latency                *int     `json:"latency"`         // ms
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

// generationStatsResponse wraps the array from /api/v1/generation.
type generationStatsResponse struct {
	Data []generationEntry `json:"data"`
}

// ---------------------------------------------------------------------------
// Aggregation types
// ---------------------------------------------------------------------------

// modelStats accumulates per-model usage data from generation history.
type modelStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	TotalLatencyMs   int
	LatencyCount     int // number of generations with latency data
	CacheDiscountUSD float64
	Providers        map[string]int // provider_name → request count
}

// providerStats accumulates per-provider (upstream) usage data.
type providerStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	Models           map[string]int // model → request count
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// ── Pricing reference ───────────────────────────────────────────────────────
// OpenRouter acts as a unified gateway to many LLM providers.
// Pricing is passthrough from each provider plus a small OpenRouter fee.
// Prices vary widely by model — see https://openrouter.ai/models for current rates.
//
// Popular model pricing (USD per 1M tokens, approximate):
//   - Claude Sonnet 4:     $3/$15
//   - Claude Opus 4:       $15/$75
//   - GPT-4.1:             $2/$8
//   - GPT-4o:              $2.50/$10
//   - Gemini 2.5 Pro:      $1.25/$10
//   - Gemini 2.5 Flash:    $0.15/$0.60
//   - Llama 3.3 70B:       $0.10/$0.10 (via Fireworks/Together)
//   - DeepSeek V3:         $0.07/$0.30
//   - Mistral Large:       $2/$6
//
// Free models are available with rate limits.
const pricingSummary = "Passthrough pricing per provider · " +
	"Claude Sonnet 4: $3/$15 · GPT-4.1: $2/$8 · Gemini 2.5 Pro: $1.25/$10 · " +
	"Llama 3.3 70B: ~$0.10/$0.10 · DeepSeek V3: ~$0.07/$0.30 " +
	"(input/output per 1M tokens; free models available)"

// Provider implements core.QuotaProvider for OpenRouter.
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

// Fetch queries the OpenRouter /auth/key endpoint for credits info,
// /credits for detailed balance, /generation for per-model usage,
// and parses rate-limit headers (both request and token limits).
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	apiKey := acct.Token
	if apiKey == "" {
		apiKey = os.Getenv(acct.APIKeyEnv)
	}
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

	// 1. Query the main auth/key endpoint for credits + key metadata
	if err := p.fetchAuthKey(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("auth/key error: %v", err)
		return snap, nil
	}

	// 2. Try /credits for detailed balance
	if err := p.fetchCreditsDetail(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["credits_detail_error"] = err.Error()
	}

	// 3. Fetch generation history with pagination for comprehensive per-model stats
	if err := p.fetchGenerationStats(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["generation_error"] = err.Error()
	}

	snap.Raw["pricing_summary"] = pricingSummary

	return snap, nil
}

// ---------------------------------------------------------------------------
// /auth/key
// ---------------------------------------------------------------------------

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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
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

	// Credits/spend metric
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

	// Rate limit from key info
	if credits.Data.RateLimit.Requests > 0 {
		rl := float64(credits.Data.RateLimit.Requests)
		snap.Metrics["rpm"] = core.Metric{
			Limit:  &rl,
			Unit:   "requests",
			Window: credits.Data.RateLimit.Interval,
		}
	}

	// Key metadata
	if credits.Data.Label != "" {
		snap.Raw["key_label"] = credits.Data.Label
	}
	if credits.Data.IsFreeTier {
		snap.Raw["tier"] = "free"
	} else {
		snap.Raw["tier"] = "paid"
	}

	// Parse rate-limit headers (both request and token limits)
	parseRateLimitHeaders(resp.Header, snap)

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("$%.4f used", usage)
	if credits.Data.Limit != nil {
		snap.Message += fmt.Sprintf(" / $%.2f limit", *credits.Data.Limit)
	}

	return nil
}

// ---------------------------------------------------------------------------
// /credits
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// /generation — with pagination and per-model/provider aggregation
// ---------------------------------------------------------------------------

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

	// Time boundaries
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Aggregate per-model and per-provider stats for today
	modelStatsMap := make(map[string]*modelStats)
	providerStatsMap := make(map[string]*providerStats)

	// Overall counters
	var todayPrompt, todayCompletion, todayRequests int
	var todayCost float64
	var todayLatencyMs, todayLatencyCount int
	var totalRequests int

	for _, g := range allGenerations {
		totalRequests++

		ts, err := time.Parse(time.RFC3339, g.CreatedAt)
		if err != nil {
			// Try alternative time format
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

		// Per-model aggregation
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

		// Per-provider aggregation
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

	// --- Emit today's aggregate metrics ---
	if todayRequests > 0 {
		reqs := float64(todayRequests)
		snap.Metrics["today_requests"] = core.Metric{
			Used:   &reqs,
			Unit:   "requests",
			Window: "today",
		}

		inp := float64(todayPrompt)
		snap.Metrics["today_input_tokens"] = core.Metric{
			Used:   &inp,
			Unit:   "tokens",
			Window: "today",
		}

		out := float64(todayCompletion)
		snap.Metrics["today_output_tokens"] = core.Metric{
			Used:   &out,
			Unit:   "tokens",
			Window: "today",
		}

		snap.Metrics["today_cost"] = core.Metric{
			Used:   &todayCost,
			Unit:   "USD",
			Window: "today",
		}

		if todayLatencyCount > 0 {
			avgLatency := float64(todayLatencyMs) / float64(todayLatencyCount) / 1000.0 // seconds
			snap.Metrics["today_avg_latency"] = core.Metric{
				Used:   &avgLatency,
				Unit:   "seconds",
				Window: "today",
			}
		}
	}

	reqs := float64(totalRequests)
	snap.Metrics["recent_requests"] = core.Metric{
		Used:   &reqs,
		Unit:   "requests",
		Window: "recent",
	}

	// --- Emit per-model metrics (today) ---
	// Sort by cost (descending) for consistent ordering
	type modelEntry struct {
		name  string
		stats *modelStats
	}
	var sortedModels []modelEntry
	for name, stats := range modelStatsMap {
		sortedModels = append(sortedModels, modelEntry{name, stats})
	}
	sort.Slice(sortedModels, func(i, j int) bool {
		return sortedModels[i].stats.TotalCost > sortedModels[j].stats.TotalCost
	})

	for _, me := range sortedModels {
		safeName := sanitizeModelName(me.name)

		inputTokens := float64(me.stats.PromptTokens)
		snap.Metrics[fmt.Sprintf("model_%s_input_tokens", safeName)] = core.Metric{
			Used:   &inputTokens,
			Unit:   "tokens",
			Window: "today",
		}

		outputTokens := float64(me.stats.CompletionTokens)
		snap.Metrics[fmt.Sprintf("model_%s_output_tokens", safeName)] = core.Metric{
			Used:   &outputTokens,
			Unit:   "tokens",
			Window: "today",
		}

		costUSD := me.stats.TotalCost
		snap.Metrics[fmt.Sprintf("model_%s_cost_usd", safeName)] = core.Metric{
			Used:   &costUSD,
			Unit:   "USD",
			Window: "today",
		}

		// Store request count and avg latency in Raw for detail view
		snap.Raw[fmt.Sprintf("model_%s_requests", safeName)] = fmt.Sprintf("%d", me.stats.Requests)

		if me.stats.LatencyCount > 0 {
			avgMs := float64(me.stats.TotalLatencyMs) / float64(me.stats.LatencyCount)
			snap.Raw[fmt.Sprintf("model_%s_avg_latency_ms", safeName)] = fmt.Sprintf("%.0f", avgMs)
		}

		if me.stats.CacheDiscountUSD > 0 {
			snap.Raw[fmt.Sprintf("model_%s_cache_savings", safeName)] = fmt.Sprintf("$%.6f", me.stats.CacheDiscountUSD)
		}

		// Store list of providers used for this model
		if len(me.stats.Providers) > 0 {
			var provList []string
			for prov := range me.stats.Providers {
				provList = append(provList, prov)
			}
			sort.Strings(provList)
			snap.Raw[fmt.Sprintf("model_%s_providers", safeName)] = strings.Join(provList, ", ")
		}
	}

	// --- Emit per-provider stats in Raw ---
	type provEntry struct {
		name  string
		stats *providerStats
	}
	var sortedProviders []provEntry
	for name, stats := range providerStatsMap {
		sortedProviders = append(sortedProviders, provEntry{name, stats})
	}
	sort.Slice(sortedProviders, func(i, j int) bool {
		return sortedProviders[i].stats.TotalCost > sortedProviders[j].stats.TotalCost
	})

	for _, pe := range sortedProviders {
		safeP := sanitizeProviderName(pe.name)
		snap.Raw[fmt.Sprintf("provider_%s_requests", safeP)] = fmt.Sprintf("%d", pe.stats.Requests)
		snap.Raw[fmt.Sprintf("provider_%s_cost", safeP)] = fmt.Sprintf("$%.6f", pe.stats.TotalCost)
		snap.Raw[fmt.Sprintf("provider_%s_prompt_tokens", safeP)] = fmt.Sprintf("%d", pe.stats.PromptTokens)
		snap.Raw[fmt.Sprintf("provider_%s_completion_tokens", safeP)] = fmt.Sprintf("%d", pe.stats.CompletionTokens)
	}

	return nil
}

// fetchAllGenerations retrieves generation history with pagination.
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

		// If we got fewer results than the page size, we've reached the end
		if len(gen.Data) < limit {
			break
		}

		offset += len(gen.Data)
	}

	return all, nil
}

// ---------------------------------------------------------------------------
// Rate-limit header parsing (both request and token limits)
// ---------------------------------------------------------------------------

func parseRateLimitHeaders(h http.Header, snap *core.QuotaSnapshot) {
	// Request rate limits
	reqLimit := parsers.ParseFloat(h.Get("x-ratelimit-limit-requests"))
	reqRemaining := parsers.ParseFloat(h.Get("x-ratelimit-remaining-requests"))

	if reqLimit != nil || reqRemaining != nil {
		snap.Metrics["rpm_headers"] = core.Metric{
			Limit:     reqLimit,
			Remaining: reqRemaining,
			Unit:      "requests",
			Window:    "1m",
		}
	}

	if rt := parsers.ParseResetTime(h.Get("x-ratelimit-reset-requests")); rt != nil {
		snap.Resets["rpm_reset"] = *rt
	}

	// Token rate limits
	tokLimit := parsers.ParseFloat(h.Get("x-ratelimit-limit-tokens"))
	tokRemaining := parsers.ParseFloat(h.Get("x-ratelimit-remaining-tokens"))

	if tokLimit != nil || tokRemaining != nil {
		snap.Metrics["tpm_headers"] = core.Metric{
			Limit:     tokLimit,
			Remaining: tokRemaining,
			Unit:      "tokens",
			Window:    "1m",
		}
	}

	if rt := parsers.ParseResetTime(h.Get("x-ratelimit-reset-tokens")); rt != nil {
		snap.Resets["tpm_reset"] = *rt
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sanitizeModelName converts a model ID like "anthropic/claude-3.5-sonnet"
// to a safe metric key component like "anthropic_claude-3.5-sonnet".
func sanitizeModelName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

// sanitizeProviderName converts a provider name for use in raw keys.
func sanitizeProviderName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	return name
}
