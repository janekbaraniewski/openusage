package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
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

func (p *Provider) fetchAuthKey(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	for _, endpoint := range []string{"/key", "/auth/key"} {
		url := baseURL + endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.Client().Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}

		snap.Raw = parsers.RedactHeaders(resp.Header)
		if resp.StatusCode == http.StatusNotFound && endpoint == "/key" {
			resp.Body.Close()
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("reading body: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
			return nil
		case http.StatusOK:
		default:
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var keyResp keyResponse
		if err := json.Unmarshal(body, &keyResp); err != nil {
			snap.Status = core.StatusError
			snap.Message = "failed to parse key response"
			return nil
		}

		applyKeyData(&keyResp.Data, snap)
		parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm_headers", "requests", "1m",
			"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
		parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm_headers", "tokens", "1m",
			"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")
		return nil
	}

	return fmt.Errorf("key endpoint not available (HTTP 404)")
}

func applyKeyData(data *keyData, snap *core.UsageSnapshot) {
	usage := data.Usage
	var remaining *float64
	if data.LimitRemaining != nil {
		remaining = data.LimitRemaining
	} else if data.Limit != nil {
		r := *data.Limit - usage
		remaining = &r
	}

	if data.Limit != nil {
		snap.Metrics["credits"] = core.Metric{
			Limit:     data.Limit,
			Used:      &usage,
			Remaining: remaining,
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

	if remaining != nil {
		snap.Metrics["limit_remaining"] = core.Metric{
			Used:   remaining,
			Unit:   "USD",
			Window: "current_period",
		}
	}

	if data.UsageDaily != nil {
		snap.Metrics["usage_daily"] = core.Metric{Used: data.UsageDaily, Unit: "USD", Window: "1d"}
	}
	if data.UsageWeekly != nil {
		snap.Metrics["usage_weekly"] = core.Metric{Used: data.UsageWeekly, Unit: "USD", Window: "7d"}
	}
	if data.UsageMonthly != nil {
		snap.Metrics["usage_monthly"] = core.Metric{Used: data.UsageMonthly, Unit: "USD", Window: "30d"}
	}
	if data.ByokUsage != nil && *data.ByokUsage > 0 {
		snap.Metrics["byok_usage"] = core.Metric{Used: data.ByokUsage, Unit: "USD", Window: "lifetime"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageDaily != nil && *data.ByokUsageDaily > 0 {
		snap.Metrics["byok_daily"] = core.Metric{Used: data.ByokUsageDaily, Unit: "USD", Window: "1d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageWeekly != nil && *data.ByokUsageWeekly > 0 {
		snap.Metrics["byok_weekly"] = core.Metric{Used: data.ByokUsageWeekly, Unit: "USD", Window: "7d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageMonthly != nil && *data.ByokUsageMonthly > 0 {
		snap.Metrics["byok_monthly"] = core.Metric{Used: data.ByokUsageMonthly, Unit: "USD", Window: "30d"}
		snap.Raw["byok_in_use"] = "true"
	}
	if data.ByokUsageInference != nil && *data.ByokUsageInference > 0 {
		snap.Metrics["today_byok_cost"] = core.Metric{Used: data.ByokUsageInference, Unit: "USD", Window: "1d"}
		snap.Raw["byok_in_use"] = "true"
	}

	if data.RateLimit.Requests > 0 {
		rl := float64(data.RateLimit.Requests)
		snap.Metrics["rpm"] = core.Metric{
			Limit:  &rl,
			Unit:   "requests",
			Window: data.RateLimit.Interval,
		}
	}

	keyLabel := data.Label
	if keyLabel == "" {
		keyLabel = data.Name
	}
	if keyLabel != "" {
		snap.Raw["key_label"] = keyLabel
	}
	if data.IsFreeTier {
		snap.Raw["tier"] = "free"
	} else {
		snap.Raw["tier"] = "paid"
	}

	snap.Raw["is_free_tier"] = fmt.Sprintf("%t", data.IsFreeTier)
	snap.Raw["is_management_key"] = fmt.Sprintf("%t", data.IsManagementKey)
	snap.Raw["is_provisioning_key"] = fmt.Sprintf("%t", data.IsProvisioningKey)
	snap.Raw["include_byok_in_limit"] = fmt.Sprintf("%t", data.IncludeByokInLimit)
	if data.RateLimit.Note != "" {
		snap.Raw["rate_limit_note"] = data.RateLimit.Note
	}

	switch {
	case data.IsManagementKey:
		snap.Raw["key_type"] = "management"
	case data.IsProvisioningKey:
		snap.Raw["key_type"] = "provisioning"
	default:
		snap.Raw["key_type"] = "standard"
	}

	if data.LimitReset != "" {
		snap.Raw["limit_reset"] = data.LimitReset
		if t, err := time.Parse(time.RFC3339, data.LimitReset); err == nil {
			snap.Resets["limit_reset"] = t
		}
	}
	if data.ExpiresAt != "" {
		snap.Raw["expires_at"] = data.ExpiresAt
		if t, err := time.Parse(time.RFC3339, data.ExpiresAt); err == nil {
			snap.Resets["key_expires"] = t
		}
	}

	snap.Status = core.StatusOK
	snap.Message = fmt.Sprintf("$%.4f used", usage)
	if data.Limit != nil {
		snap.Message += fmt.Sprintf(" / $%.2f limit", *data.Limit)
	}
}

func (p *Provider) fetchCreditsDetail(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	url := baseURL + "/credits"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.Client().Do(req)
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

	remaining := detail.Data.TotalCredits - detail.Data.TotalUsage
	if detail.Data.RemainingBalance != nil {
		remaining = *detail.Data.RemainingBalance
	}

	if detail.Data.TotalCredits > 0 || detail.Data.TotalUsage > 0 || remaining > 0 {
		totalCredits := detail.Data.TotalCredits
		totalUsage := detail.Data.TotalUsage

		snap.Metrics["credit_balance"] = core.Metric{
			Limit:     &totalCredits,
			Used:      &totalUsage,
			Remaining: &remaining,
			Unit:      "USD",
			Window:    "lifetime",
		}

		snap.Message = fmt.Sprintf("$%.4f used", totalUsage)
		if totalCredits > 0 {
			snap.Message += fmt.Sprintf(" / $%.2f credits", totalCredits)
		}
	}

	return nil
}

func (p *Provider) fetchKeysMeta(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	const (
		pageSizeHint = 100
		maxPages     = 20
	)

	var allKeys []keyListEntry
	offset := 0

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/keys?include_disabled=true&offset=%d", baseURL, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := p.Client().Do(req)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusForbidden {
			return nil
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var pageResp keysResponse
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return fmt.Errorf("parsing keys list: %w", err)
		}
		if len(pageResp.Data) == 0 {
			break
		}

		allKeys = append(allKeys, pageResp.Data...)
		offset += len(pageResp.Data)
		if len(pageResp.Data) < pageSizeHint {
			break
		}
	}

	snap.Raw["keys_total"] = fmt.Sprintf("%d", len(allKeys))

	active := 0
	for _, k := range allKeys {
		if !k.Disabled {
			active++
		}
	}
	snap.Raw["keys_active"] = fmt.Sprintf("%d", active)
	disabled := len(allKeys) - active
	snap.Raw["keys_disabled"] = fmt.Sprintf("%d", disabled)

	totalF := float64(len(allKeys))
	activeF := float64(active)
	disabledF := float64(disabled)
	snap.Metrics["keys_total"] = core.Metric{Used: &totalF, Unit: "keys", Window: "account"}
	snap.Metrics["keys_active"] = core.Metric{Used: &activeF, Unit: "keys", Window: "account"}
	if disabled > 0 {
		snap.Metrics["keys_disabled"] = core.Metric{Used: &disabledF, Unit: "keys", Window: "account"}
	}

	currentLabel := snap.Raw["key_label"]
	if currentLabel == "" {
		return nil
	}

	var current *keyListEntry
	for i := range allKeys {
		if allKeys[i].Label == currentLabel {
			current = &allKeys[i]
			break
		}
	}
	if current == nil {
		snap.Raw["key_lookup"] = "not_in_keys_list"
		return nil
	}

	if current.Name != "" {
		snap.Raw["key_name"] = current.Name
	}
	snap.Raw["key_disabled"] = fmt.Sprintf("%t", current.Disabled)
	if current.CreatedAt != "" {
		snap.Raw["key_created_at"] = current.CreatedAt
	}
	if current.UpdatedAt != nil && *current.UpdatedAt != "" {
		snap.Raw["key_updated_at"] = *current.UpdatedAt
	}
	if current.Hash != "" {
		hash := current.Hash
		if len(hash) > 12 {
			hash = hash[:12]
		}
		snap.Raw["key_hash_prefix"] = hash
	}

	// For management keys, aggregate usage from all sub-keys.
	// The /auth/key endpoint reports $0 for the management key itself;
	// the real spend is spread across the provisioned sub-keys.
	if snap.Raw["is_management_key"] == "true" {
		var totalUsage, daily, weekly, monthly float64
		for _, k := range allKeys {
			totalUsage += k.Usage
			daily += k.UsageDaily
			weekly += k.UsageWeekly
			monthly += k.UsageMonthly
		}
		if totalUsage > 0 {
			snap.Metrics["credits"] = core.Metric{
				Used:   &totalUsage,
				Unit:   "USD",
				Window: "lifetime",
			}
			if lim := snap.Metrics["credits"].Limit; lim != nil {
				snap.Metrics["credits"] = core.Metric{
					Limit:  lim,
					Used:   &totalUsage,
					Unit:   "USD",
					Window: "lifetime",
				}
			}
		}
		if daily > 0 {
			snap.Metrics["usage_daily"] = core.Metric{Used: &daily, Unit: "USD", Window: "1d"}
		}
		if weekly > 0 {
			snap.Metrics["usage_weekly"] = core.Metric{Used: &weekly, Unit: "USD", Window: "7d"}
		}
		if monthly > 0 {
			snap.Metrics["usage_monthly"] = core.Metric{Used: &monthly, Unit: "USD", Window: "30d"}
		}
	}

	return nil
}

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
		// Internal endpoint is web-dashboard oriented and may require session auth;
		// keep it as a last-resort fallback only.
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
		if err != nil {
			continue
		}
		if !ok {
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

		es, ok := endpointStatsMap[endpointID]
		if !ok {
			es = &endpointStats{Model: modelName, Provider: providerName}
			endpointStatsMap[endpointID] = es
		}
		es.Requests += entry.Requests
		es.TotalCost += cost
		es.ByokCost += byokCost
		es.PromptTokens += entry.PromptTokens
		es.CompletionTokens += entry.CompletionTokens
		es.ReasoningTokens += entry.ReasoningTokens

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
		snap.DailySeries["analytics_cost"] = mapToSortedTimePoints(costByDate)
	}
	if len(tokensByDate) > 0 {
		snap.DailySeries["analytics_tokens"] = mapToSortedTimePoints(tokensByDate)
	}
	if len(requestsByDate) > 0 {
		snap.DailySeries["analytics_requests"] = mapToSortedTimePoints(requestsByDate)
	}
	if len(byokCostByDate) > 0 {
		snap.DailySeries["analytics_byok_cost"] = mapToSortedTimePoints(byokCostByDate)
	}
	if len(reasoningTokensByDate) > 0 {
		snap.DailySeries["analytics_reasoning_tokens"] = mapToSortedTimePoints(reasoningTokensByDate)
	}
	if len(cachedTokensByDate) > 0 {
		snap.DailySeries["analytics_cached_tokens"] = mapToSortedTimePoints(cachedTokensByDate)
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
	if strings.HasPrefix(endpoint, "/api/internal/") {
		if strings.HasSuffix(base, "/api/v1") {
			base = strings.TrimSuffix(base, "/api/v1")
		}
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
	// treat large numeric values as milliseconds since epoch
	if sec > 1_000_000_000_000 {
		sec = sec / 1000
	}
	if sec <= 0 {
		return fmt.Sprintf("%.0f", n)
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func normalizeActivityDate(raw string) (string, time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
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
		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "api",
			Window:     "activity",
		}

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

// filterRouterClientProviders removes entries keyed by router/client app names
// (e.g. "Openusage", "OpenRouter") from analytics provider maps. The /activity
// endpoint sometimes returns the app/key name instead of the actual upstream
// hosting provider. Removing these avoids polluting the "Providers" breakdown
// with client names; real hosting provider data comes from /generations.
func filterRouterClientProviders(maps ...map[string]float64) {
	for _, m := range maps {
		for name := range m {
			if isLikelyRouterClientProviderName(name) {
				delete(m, name)
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
	for p := range providerCost {
		providerSet[p] = struct{}{}
	}
	for p := range providerInputTokens {
		providerSet[p] = struct{}{}
	}
	for p := range providerOutputTokens {
		providerSet[p] = struct{}{}
	}
	for p := range providerReasoningTokens {
		providerSet[p] = struct{}{}
	}
	for p := range providerRequests {
		providerSet[p] = struct{}{}
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
	limit := maxEndpointMetrics
	if len(entries) < limit {
		limit = len(entries)
	}
	for _, entry := range entries[:limit] {
		safe := sanitizeName(entry.id)
		prefix := "endpoint_" + safe

		req := float64(entry.stats.Requests)
		if req > 0 {
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

func parseAPIErrorMessage(body []byte) string {
	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return ""
	}
	return strings.TrimSpace(apiErr.Error.Message)
}

func emitPerModelMetrics(modelStatsMap map[string]*modelStats, snap *core.UsageSnapshot) {
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
		rec := core.ModelUsageRecord{
			RawModelID: e.name,
			RawSource:  "api",
			Window:     "30d",
		}

		inputTokens := float64(e.stats.PromptTokens)
		snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &inputTokens, Unit: "tokens", Window: "30d"}
		rec.InputTokens = core.Float64Ptr(inputTokens)

		outputTokens := float64(e.stats.CompletionTokens)
		snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &outputTokens, Unit: "tokens", Window: "30d"}
		rec.OutputTokens = core.Float64Ptr(outputTokens)

		if e.stats.ReasoningTokens > 0 {
			reasoningTokens := float64(e.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &reasoningTokens, Unit: "tokens", Window: "30d"}
			rec.ReasoningTokens = core.Float64Ptr(reasoningTokens)
		}
		if e.stats.CachedTokens > 0 {
			cachedTokens := float64(e.stats.CachedTokens)
			snap.Metrics[prefix+"_cached_tokens"] = core.Metric{Used: &cachedTokens, Unit: "tokens", Window: "30d"}
			rec.CachedTokens = core.Float64Ptr(cachedTokens)
		}
		totalTokens := float64(e.stats.PromptTokens + e.stats.CompletionTokens + e.stats.ReasoningTokens + e.stats.CachedTokens)
		if totalTokens > 0 {
			snap.Metrics[prefix+"_total_tokens"] = core.Metric{Used: &totalTokens, Unit: "tokens", Window: "30d"}
			rec.TotalTokens = core.Float64Ptr(totalTokens)
		}
		if e.stats.ImageTokens > 0 {
			imageTokens := float64(e.stats.ImageTokens)
			snap.Metrics[prefix+"_image_tokens"] = core.Metric{Used: &imageTokens, Unit: "tokens", Window: "30d"}
		}

		costUSD := e.stats.TotalCost
		snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &costUSD, Unit: "USD", Window: "30d"}
		rec.CostUSD = core.Float64Ptr(costUSD)
		requests := float64(e.stats.Requests)
		snap.Metrics[prefix+"_requests"] = core.Metric{Used: &requests, Unit: "requests", Window: "30d"}
		rec.Requests = core.Float64Ptr(requests)
		if e.stats.NativePrompt > 0 {
			nativeInput := float64(e.stats.NativePrompt)
			snap.Metrics[prefix+"_native_input_tokens"] = core.Metric{Used: &nativeInput, Unit: "tokens", Window: "30d"}
		}
		if e.stats.NativeCompletion > 0 {
			nativeOutput := float64(e.stats.NativeCompletion)
			snap.Metrics[prefix+"_native_output_tokens"] = core.Metric{Used: &nativeOutput, Unit: "tokens", Window: "30d"}
		}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", e.stats.Requests)

		if e.stats.LatencyCount > 0 {
			avgMs := float64(e.stats.TotalLatencyMs) / float64(e.stats.LatencyCount)
			snap.Raw[prefix+"_avg_latency_ms"] = fmt.Sprintf("%.0f", avgMs)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_latency"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
		}
		if e.stats.GenerationCount > 0 {
			avgMs := float64(e.stats.TotalGenMs) / float64(e.stats.GenerationCount)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_generation_time"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
		}
		if e.stats.ModerationCount > 0 {
			avgMs := float64(e.stats.TotalModeration) / float64(e.stats.ModerationCount)
			avgSeconds := avgMs / 1000.0
			snap.Metrics[prefix+"_avg_moderation_latency"] = core.Metric{Used: &avgSeconds, Unit: "seconds", Window: "30d"}
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
			if len(provList) > 0 {
				rec.SetDimension("upstream_providers", strings.Join(provList, ","))
			}
		}
		if rec.InputTokens != nil || rec.OutputTokens != nil || rec.CostUSD != nil || rec.Requests != nil || rec.ReasoningTokens != nil || rec.CachedTokens != nil {
			snap.AppendModelUsage(rec)
		}
	}
}

func emitPerProviderMetrics(providerStatsMap map[string]*providerStats, snap *core.UsageSnapshot) {
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
		requests := float64(e.stats.Requests)
		snap.Metrics[prefix+"_requests"] = core.Metric{Used: &requests, Unit: "requests", Window: "30d"}
		if e.stats.TotalCost > 0 {
			v := e.stats.TotalCost
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "30d"}
		}
		if e.stats.ByokCost > 0 {
			v := e.stats.ByokCost
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "30d"}
		}
		if e.stats.PromptTokens > 0 {
			v := float64(e.stats.PromptTokens)
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		if e.stats.CompletionTokens > 0 {
			v := float64(e.stats.CompletionTokens)
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		if e.stats.ReasoningTokens > 0 {
			v := float64(e.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "30d"}
		}
		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%d", e.stats.Requests)
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", e.stats.TotalCost)
		if e.stats.ByokCost > 0 {
			snap.Raw[prefix+"_byok_cost"] = fmt.Sprintf("$%.6f", e.stats.ByokCost)
		}
		snap.Raw[prefix+"_prompt_tokens"] = fmt.Sprintf("%d", e.stats.PromptTokens)
		snap.Raw[prefix+"_completion_tokens"] = fmt.Sprintf("%d", e.stats.CompletionTokens)
		if e.stats.ReasoningTokens > 0 {
			snap.Raw[prefix+"_reasoning_tokens"] = fmt.Sprintf("%d", e.stats.ReasoningTokens)
		}
	}
}

func emitClientDailySeries(snap *core.UsageSnapshot, tokensByClient, requestsByClient map[string]map[string]float64) {
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	for client, byDate := range tokensByClient {
		if client == "" || len(byDate) == 0 {
			continue
		}
		snap.DailySeries["tokens_client_"+client] = mapToSortedTimePoints(byDate)
	}
	for client, byDate := range requestsByClient {
		if client == "" || len(byDate) == 0 {
			continue
		}
		snap.DailySeries["usage_client_"+client] = mapToSortedTimePoints(byDate)
	}
}

type providerClientAggregate struct {
	InputTokens     float64
	OutputTokens    float64
	ReasoningTokens float64
	Requests        float64
	CostUSD         float64
	Window          string
}

type modelUsageCount struct {
	name  string
	count float64
}

func enrichDashboardRepresentations(snap *core.UsageSnapshot) {
	if snap == nil || len(snap.Metrics) == 0 {
		return
	}
	synthesizeClientMetricsFromProviderMetrics(snap)
	synthesizeLanguageMetricsFromModelRequests(snap)
	synthesizeUsageSummaries(snap)
}

func synthesizeClientMetricsFromProviderMetrics(snap *core.UsageSnapshot) {
	byClient := make(map[string]*providerClientAggregate)
	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}
		client, field, ok := parseProviderMetricKey(key)
		if !ok || client == "" {
			continue
		}
		agg, exists := byClient[client]
		if !exists {
			agg = &providerClientAggregate{}
			byClient[client] = agg
		}
		if agg.Window == "" && metric.Window != "" {
			agg.Window = metric.Window
		}
		switch field {
		case "input_tokens":
			agg.InputTokens = *metric.Used
		case "output_tokens":
			agg.OutputTokens = *metric.Used
		case "reasoning_tokens":
			agg.ReasoningTokens = *metric.Used
		case "requests":
			agg.Requests = *metric.Used
		case "cost_usd":
			agg.CostUSD = *metric.Used
		}
	}

	for client, agg := range byClient {
		window := strings.TrimSpace(agg.Window)
		if window == "" {
			window = "30d"
		}
		clientPrefix := "client_" + client

		if agg.InputTokens > 0 {
			v := agg.InputTokens
			snap.Metrics[clientPrefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.OutputTokens > 0 {
			v := agg.OutputTokens
			snap.Metrics[clientPrefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.ReasoningTokens > 0 {
			v := agg.ReasoningTokens
			snap.Metrics[clientPrefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		totalTokens := agg.InputTokens + agg.OutputTokens + agg.ReasoningTokens
		if totalTokens > 0 {
			v := totalTokens
			snap.Metrics[clientPrefix+"_total_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: window}
		}
		if agg.Requests > 0 {
			v := agg.Requests
			snap.Metrics[clientPrefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: window}
		}
		if agg.CostUSD > 0 {
			v := agg.CostUSD
			snap.Metrics[clientPrefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: window}
		}
	}
}

func parseProviderMetricKey(key string) (name, field string, ok bool) {
	const prefix = "provider_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{
		"_input_tokens",
		"_output_tokens",
		"_reasoning_tokens",
		"_requests",
		"_cost_usd",
	} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func synthesizeLanguageMetricsFromModelRequests(snap *core.UsageSnapshot) {
	byLanguage := make(map[string]float64)
	window := ""
	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}
		model, field, ok := parseModelMetricKey(key)
		if !ok || field != "requests" {
			continue
		}
		if window == "" && strings.TrimSpace(metric.Window) != "" {
			window = strings.TrimSpace(metric.Window)
		}
		lang := inferModelWorkloadLanguage(model)
		byLanguage[lang] += *metric.Used
	}
	if len(byLanguage) == 0 {
		return
	}
	if window == "" {
		window = "30d inferred"
	}
	for lang, count := range byLanguage {
		if count <= 0 {
			continue
		}
		v := count
		snap.Metrics["lang_"+sanitizeName(lang)] = core.Metric{Used: &v, Unit: "requests", Window: window}
	}
	if summary := summarizeCountUsage(byLanguage, "req", 6); summary != "" {
		snap.Raw["language_usage"] = summary
		snap.Raw["language_usage_source"] = "inferred_from_model_ids"
	}
}

func parseModelMetricKey(key string) (name, field string, ok bool) {
	const prefix = "model_"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{"_requests"} {
		if strings.HasSuffix(rest, suffix) {
			return strings.TrimSuffix(rest, suffix), strings.TrimPrefix(suffix, "_"), true
		}
	}
	return "", "", false
}

func inferModelWorkloadLanguage(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "general"
	}
	switch {
	case strings.Contains(model, "coder"), strings.Contains(model, "codestral"), strings.Contains(model, "devstral"), strings.Contains(model, "code"):
		return "code"
	case strings.Contains(model, "vision"), strings.Contains(model, "image"), strings.Contains(model, "multimodal"), strings.Contains(model, "omni"), strings.Contains(model, "vl"):
		return "multimodal"
	case strings.Contains(model, "audio"), strings.Contains(model, "speech"), strings.Contains(model, "voice"), strings.Contains(model, "whisper"), strings.Contains(model, "tts"), strings.Contains(model, "stt"):
		return "audio"
	case strings.Contains(model, "reason"), strings.Contains(model, "thinking"):
		return "reasoning"
	default:
		return "general"
	}
}

func synthesizeUsageSummaries(snap *core.UsageSnapshot) {
	modelTotals := make(map[string]float64)
	modelWindow := ""
	modelUnit := "tok"
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "model_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_total_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_total_tokens")
			modelTotals[name] = *metric.Used
			if modelWindow == "" && strings.TrimSpace(metric.Window) != "" {
				modelWindow = strings.TrimSpace(metric.Window)
			}
		case strings.HasSuffix(key, "_cost_usd"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			if _, ok := modelTotals[name]; !ok {
				modelTotals[name] = *metric.Used
				modelUnit = "usd"
				if modelWindow == "" && strings.TrimSpace(metric.Window) != "" {
					modelWindow = strings.TrimSpace(metric.Window)
				}
			}
		}
	}
	if summary := summarizeShareUsage(modelTotals, 6); summary != "" {
		snap.Raw["model_usage"] = summary
		if modelWindow != "" {
			snap.Raw["model_usage_window"] = modelWindow
		}
		snap.Raw["model_usage_unit"] = modelUnit
	}

	clientTotals := make(map[string]float64)
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "client_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_total_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "client_"), "_total_tokens")
			clientTotals[name] = *metric.Used
		case strings.HasSuffix(key, "_requests"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "client_"), "_requests")
			if _, ok := clientTotals[name]; !ok {
				clientTotals[name] = *metric.Used
			}
		}
	}
	if summary := summarizeShareUsage(clientTotals, 6); summary != "" {
		snap.Raw["client_usage"] = summary
	}
}

func summarizeShareUsage(values map[string]float64, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	list := make([]item, 0, len(values))
	total := 0.0
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
		total += value
	}
	if len(list) == 0 || total <= 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f%%", normalizeUsageLabel(entry.name), entry.value/total*100))
	}
	return strings.Join(parts, ", ")
}

func summarizeCountUsage(values map[string]float64, unit string, maxItems int) string {
	type item struct {
		name  string
		value float64
	}
	list := make([]item, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f %s", normalizeUsageLabel(entry.name), entry.value, unit))
	}
	return strings.Join(parts, ", ")
}

func normalizeUsageLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	name = strings.ReplaceAll(name, "_", " ")
	return name
}

func emitModelDerivedToolUsageMetrics(snap *core.UsageSnapshot, modelRequests map[string]float64, window, source string) {
	if snap == nil || len(modelRequests) == 0 {
		return
	}
	if strings.TrimSpace(window) == "" {
		window = "30d inferred"
	}
	counts := make(map[string]int, len(modelRequests))
	rows := make([]modelUsageCount, 0, len(modelRequests))
	totalCalls := 0.0
	for model, requests := range modelRequests {
		if requests <= 0 {
			continue
		}
		key := "tool_" + sanitizeName(model)
		v := requests
		snap.Metrics[key] = core.Metric{Used: &v, Unit: "calls", Window: window}
		totalCalls += requests
		counts[model] = int(math.Round(requests))
		rows = append(rows, modelUsageCount{name: model, count: requests})
	}
	if totalCalls <= 0 {
		return
	}
	if source != "" {
		snap.Raw["tool_usage_source"] = source
	}
	if summary := summarizeModelCountUsage(rows, 6); summary != "" {
		snap.Raw["tool_usage"] = summary
	} else {
		snap.Raw["tool_usage"] = summarizeTopCounts(counts, 6)
	}
	totalV := totalCalls
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: "30d"}
}

func emitToolOutcomeMetrics(snap *core.UsageSnapshot, totalRequests, totalCancelled int, window string) {
	if snap == nil || totalRequests <= 0 {
		return
	}
	if strings.TrimSpace(window) == "" {
		window = "30d"
	}
	totalV := float64(totalRequests)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: window}
	completed := totalRequests - totalCancelled
	if completed < 0 {
		completed = 0
	}
	completedV := float64(completed)
	snap.Metrics["tool_completed"] = core.Metric{Used: &completedV, Unit: "calls", Window: window}
	if totalCancelled > 0 {
		cancelledV := float64(totalCancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &cancelledV, Unit: "calls", Window: window}
	}
	successRate := completedV / totalV * 100
	snap.Metrics["tool_success_rate"] = core.Metric{Used: &successRate, Unit: "%", Window: window}
}

func summarizeModelCountUsage(rows []modelUsageCount, limit int) string {
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].name < rows[j].name
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s: %.0f calls", row.name, row.count))
	}
	return strings.Join(parts, ", ")
}

func summarizeTopCounts(counts map[string]int, limit int) string {
	type kv struct {
		name  string
		count int
	}
	items := make([]kv, 0, len(counts))
	for name, count := range counts {
		if count <= 0 {
			continue
		}
		items = append(items, kv{name: name, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	parts := make([]string, 0, limit)
	for _, item := range items[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d", item.name, item.count))
	}
	return strings.Join(parts, ", ")
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	safe := strings.Trim(b.String(), "_")
	if safe == "" {
		return "unknown"
	}
	return safe
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.Trim(name, "/")
	name = strings.Join(strings.Fields(name), "-")
	if name == "" {
		return ""
	}
	return name
}
