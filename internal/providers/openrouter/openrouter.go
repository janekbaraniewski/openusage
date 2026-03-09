package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
