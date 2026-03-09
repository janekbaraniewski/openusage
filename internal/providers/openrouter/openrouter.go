package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"

	maxGenerationsToFetch = 500
	generationPageSize    = 100
	generationMaxAge      = 30 * 24 * time.Hour
	// Keep enrichment bounded: only a subset of ambiguous rows are upgraded
	// via /generation?id=<id> to recover upstream hosting providers.
	maxGenerationProviderDetailLookups = 20
)

var errGenerationListUnsupported = errors.New("generation list endpoint unsupported")

type keyResponse struct {
	Data keyData `json:"data"`
}

type keyData struct {
	Label              string    `json:"label"`
	Name               string    `json:"name"`
	Usage              float64   `json:"usage"`
	Limit              *float64  `json:"limit"`
	LimitRemaining     *float64  `json:"limit_remaining"`
	UsageDaily         *float64  `json:"usage_daily"`
	UsageWeekly        *float64  `json:"usage_weekly"`
	UsageMonthly       *float64  `json:"usage_monthly"`
	ByokUsage          *float64  `json:"byok_usage"`
	ByokUsageInference *float64  `json:"byok_usage_inference"`
	ByokUsageDaily     *float64  `json:"byok_usage_daily"`
	ByokUsageWeekly    *float64  `json:"byok_usage_weekly"`
	ByokUsageMonthly   *float64  `json:"byok_usage_monthly"`
	IsFreeTier         bool      `json:"is_free_tier"`
	IsManagementKey    bool      `json:"is_management_key"`
	IsProvisioningKey  bool      `json:"is_provisioning_key"`
	IncludeByokInLimit bool      `json:"include_byok_in_limit"`
	LimitReset         string    `json:"limit_reset"`
	ExpiresAt          string    `json:"expires_at"`
	RateLimit          rateLimit `json:"rate_limit"`
}

type creditsDetailResponse struct {
	Data struct {
		TotalCredits     float64  `json:"total_credits"`
		TotalUsage       float64  `json:"total_usage"`
		RemainingBalance *float64 `json:"remaining_balance"`
	} `json:"data"`
}

type rateLimit struct {
	Requests int    `json:"requests"`
	Interval string `json:"interval"`
	Note     string `json:"note"`
}

type keysResponse struct {
	Data []keyListEntry `json:"data"`
}

type keyListEntry struct {
	Hash               string   `json:"hash"`
	Name               string   `json:"name"`
	Label              string   `json:"label"`
	Disabled           bool     `json:"disabled"`
	Limit              *float64 `json:"limit"`
	LimitRemaining     *float64 `json:"limit_remaining"`
	LimitReset         string   `json:"limit_reset"`
	IncludeByokInLimit bool     `json:"include_byok_in_limit"`
	Usage              float64  `json:"usage"`
	UsageDaily         float64  `json:"usage_daily"`
	UsageWeekly        float64  `json:"usage_weekly"`
	UsageMonthly       float64  `json:"usage_monthly"`
	ByokUsage          float64  `json:"byok_usage"`
	ByokUsageDaily     float64  `json:"byok_usage_daily"`
	ByokUsageWeekly    float64  `json:"byok_usage_weekly"`
	ByokUsageMonthly   float64  `json:"byok_usage_monthly"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          *string  `json:"updated_at"`
	ExpiresAt          *string  `json:"expires_at"`
}

type providerResolutionSource string

const (
	providerSourceResponses     providerResolutionSource = "responses"
	providerSourceEntryField    providerResolutionSource = "entry_field"
	providerSourceUpstreamID    providerResolutionSource = "upstream_id"
	providerSourceProviderName  providerResolutionSource = "provider_name"
	providerSourceModelPrefix   providerResolutionSource = "model_prefix"
	providerSourceFallbackLabel providerResolutionSource = "fallback_label"
)

var knownModelVendorPrefixes = []string{
	"black-forest-labs",
	"meta-llama",
	"moonshotai",
	"deepseek",
	"nvidia",
	"openai",
	"anthropic",
	"google",
	"mistral",
	"qwen",
	"z-ai",
	"x-ai",
	"xai",
	"alibaba",
}

type analyticsEntry struct {
	Date               string  `json:"date"`
	Model              string  `json:"model"`
	ModelPermaslug     string  `json:"model_permaslug"`
	Variant            string  `json:"variant"`
	ProviderName       string  `json:"provider_name"`
	EndpointID         string  `json:"endpoint_id"`
	Usage              float64 `json:"usage"`
	ByokUsageInference float64 `json:"byok_usage_inference"`
	ByokRequests       int     `json:"byok_requests"`
	TotalCost          float64 `json:"total_cost"`
	TotalTokens        int     `json:"total_tokens"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	ReasoningTokens    int     `json:"reasoning_tokens"`
	CachedTokens       int     `json:"cached_tokens"`
	Requests           int     `json:"requests"`
}

type analyticsResponse struct {
	Data []analyticsEntry `json:"data"`
}

type analyticsEnvelopeResponse struct {
	Data struct {
		Data     []analyticsEntry `json:"data"`
		CachedAt json.RawMessage  `json:"cachedAt"`
	} `json:"data"`
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
		Name    string `json:"name"`
	} `json:"error"`
	Success bool `json:"success"`
}

type modelStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	NativePrompt     int
	NativeCompletion int
	ReasoningTokens  int
	CachedTokens     int
	ImageTokens      int
	TotalCost        float64
	TotalLatencyMs   int
	LatencyCount     int
	TotalGenMs       int
	GenerationCount  int
	TotalModeration  int
	ModerationCount  int
	CacheDiscountUSD float64
	Providers        map[string]int
}

type providerStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	ByokCost         float64
	TotalCost        float64
	Models           map[string]int
}

type endpointStats struct {
	Requests         int
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	ByokCost         float64
	TotalCost        float64
	Model            string
	Provider         string
}

type Provider struct {
	providerbase.Base
	clock core.Clock
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "openrouter",
			Info: core.ProviderInfo{
				Name:         "OpenRouter",
				Capabilities: []string{"key_endpoint", "credits_endpoint", "activity_endpoint", "generation_stats", "per_model_breakdown", "headers"},
				DocURL:       "https://openrouter.ai/docs/api-reference/api-keys/get-current-key",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OPENROUTER_API_KEY",
				DefaultAccountID: "openrouter",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set OPENROUTER_API_KEY to a valid OpenRouter API key."},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

func (p *Provider) now() time.Time {
	if p == nil || p.clock == nil {
		return time.Now()
	}
	return p.clock.Now()
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
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	if err := p.fetchAuthKey(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("auth/key error: %v", err)
		return snap, nil
	}

	if err := p.fetchCreditsDetail(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["credits_detail_error"] = err.Error()
	}

	if snap.Raw["is_management_key"] == "true" {
		if err := p.fetchKeysMeta(ctx, baseURL, apiKey, &snap); err != nil {
			snap.Raw["keys_error"] = err.Error()
		}
	}

	snap.DailySeries = make(map[string][]core.TimePoint)

	if err := p.fetchAnalytics(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["analytics_error"] = err.Error()
	}

	if err := p.fetchGenerationStats(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["generation_error"] = err.Error()
	}
	enrichDashboardRepresentations(&snap)

	return snap, nil
}
