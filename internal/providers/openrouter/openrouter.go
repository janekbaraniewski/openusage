package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"

	maxGenerationsToFetch = 500
	generationPageSize    = 100
	generationMaxAge      = 30 * 24 * time.Hour
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

type generationEntry struct {
	ID                     string   `json:"id"`
	Model                  string   `json:"model"`
	TotalCost              float64  `json:"total_cost"`
	Usage                  float64  `json:"usage"`
	IsByok                 bool     `json:"is_byok"`
	UpstreamInferenceCost  *float64 `json:"upstream_inference_cost"`
	Cancelled              bool     `json:"cancelled"`
	PromptTokens           int      `json:"tokens_prompt"`
	CompletionTokens       int      `json:"tokens_completion"`
	NativePromptTokens     *int     `json:"native_tokens_prompt"`
	NativeCompletionTokens *int     `json:"native_tokens_completion"`
	NativeReasoningTokens  *int     `json:"native_tokens_reasoning"`
	NativeCachedTokens     *int     `json:"native_tokens_cached"`
	NativeImageTokens      *int     `json:"native_tokens_completion_images"`
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
	NumInputAudioPrompt    *int     `json:"num_input_audio_prompt"`
	NumSearchResults       *int     `json:"num_search_results"`
	Finish                 string   `json:"finish_reason"`
	NativeFinish           string   `json:"native_finish_reason"`
	UpstreamID             string   `json:"upstream_id"`
	ModerationLatency      *int     `json:"moderation_latency"`
	ExternalUser           string   `json:"external_user"`
	APIType                string   `json:"api_type"`
	Router                 string   `json:"router"`
}

type generationStatsResponse struct {
	Data []generationEntry `json:"data"`
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
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.UsageSnapshot{
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

	snap := core.UsageSnapshot{
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

		resp, err := http.DefaultClient.Do(req)
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

		resp, err := http.DefaultClient.Do(req)
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

	return nil
}

func (p *Provider) fetchAnalytics(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	var analytics analyticsResponse
	var activityEndpoint string
	var activityCachedAt string
	forbiddenMsg := ""
	yesterdayUTC := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

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

		resp, err := http.DefaultClient.Do(req)
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

	now := time.Now().UTC()
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
	emitAnalyticsPerProviderMetrics(snap, providerCost, providerByokCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests)
	emitAnalyticsEndpointMetrics(snap, endpointStatsMap)

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
			core.AppendModelUsageRecord(snap, rec)
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

	snap.Raw["generations_fetched"] = fmt.Sprintf("%d", len(allGenerations))

	now := time.Now().UTC()
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
	apiTypeCounts := make(map[string]int)
	finishReasonCounts := make(map[string]int)
	originCounts := make(map[string]int)
	routerCounts := make(map[string]int)

	var cost7d, cost30d, burnCost float64
	var todayByokCost, cost7dByok, cost30dByok float64

	dailyCost := make(map[string]float64)
	dailyRequests := make(map[string]float64)
	dailyModelTokens := make(map[string]map[string]float64) // model -> date -> tokens

	for _, g := range allGenerations {
		totalRequests++
		generationCost := g.TotalCost
		if generationCost == 0 && g.Usage > 0 {
			generationCost = g.Usage
		}

		ts, err := time.Parse(time.RFC3339, g.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339Nano, g.CreatedAt)
			if err != nil {
				continue
			}
		}

		// Period cost aggregation (all fetched generations, up to 30 days)
		cost30d += generationCost
		if ts.After(sevenDaysAgo) {
			cost7d += generationCost
		}
		byokCost := generationByokCost(g)
		cost30dByok += byokCost
		if ts.After(sevenDaysAgo) {
			cost7dByok += byokCost
		}

		// Burn rate: last 60 minutes
		if ts.After(burnCutoff) {
			burnCost += generationCost
		}

		// Daily aggregation
		dateKey := ts.UTC().Format("2006-01-02")
		dailyCost[dateKey] += generationCost
		dailyRequests[dateKey]++

		modelKey := normalizeModelName(g.Model)
		if modelKey == "" {
			modelKey = "unknown"
		}
		if _, ok := dailyModelTokens[modelKey]; !ok {
			dailyModelTokens[modelKey] = make(map[string]float64)
		}
		dailyModelTokens[modelKey][dateKey] += float64(g.PromptTokens + g.CompletionTokens)

		ms, ok := modelStatsMap[modelKey]
		if !ok {
			ms = &modelStats{Providers: make(map[string]int)}
			modelStatsMap[modelKey] = ms
		}
		ms.Requests++
		ms.PromptTokens += g.PromptTokens
		ms.CompletionTokens += g.CompletionTokens
		if g.NativePromptTokens != nil {
			ms.NativePrompt += *g.NativePromptTokens
		}
		if g.NativeCompletionTokens != nil {
			ms.NativeCompletion += *g.NativeCompletionTokens
		}
		if g.NativeReasoningTokens != nil {
			ms.ReasoningTokens += *g.NativeReasoningTokens
		}
		if g.NativeCachedTokens != nil {
			ms.CachedTokens += *g.NativeCachedTokens
		}
		if g.NativeImageTokens != nil {
			ms.ImageTokens += *g.NativeImageTokens
		}
		ms.TotalCost += generationCost
		if g.Latency != nil && *g.Latency > 0 {
			ms.TotalLatencyMs += *g.Latency
			ms.LatencyCount++
		}
		if g.GenerationTime != nil && *g.GenerationTime > 0 {
			ms.TotalGenMs += *g.GenerationTime
			ms.GenerationCount++
		}
		if g.ModerationLatency != nil && *g.ModerationLatency > 0 {
			ms.TotalModeration += *g.ModerationLatency
			ms.ModerationCount++
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
		if g.NativeReasoningTokens != nil {
			ps.ReasoningTokens += *g.NativeReasoningTokens
		}
		ps.ByokCost += byokCost
		ps.TotalCost += generationCost
		ps.Models[modelKey]++

		if !ts.After(todayStart) {
			continue
		}

		todayRequests++
		todayPrompt += g.PromptTokens
		todayCompletion += g.CompletionTokens
		if g.NativePromptTokens != nil {
			todayNativePrompt += *g.NativePromptTokens
		}
		if g.NativeCompletionTokens != nil {
			todayNativeCompletion += *g.NativeCompletionTokens
		}
		todayCost += generationCost
		todayByokCost += byokCost
		if g.Cancelled {
			todayCancelled++
		}
		if g.Streamed {
			todayStreamed++
		}
		if g.NativeReasoningTokens != nil {
			todayReasoning += *g.NativeReasoningTokens
		}
		if g.NativeCachedTokens != nil {
			todayCached += *g.NativeCachedTokens
		}
		if g.NativeImageTokens != nil {
			todayImageTokens += *g.NativeImageTokens
		}
		if g.NumMediaPrompt != nil {
			todayMediaPrompt += *g.NumMediaPrompt
		}
		if g.NumMediaCompletion != nil {
			todayMediaCompletion += *g.NumMediaCompletion
		}
		if g.NumInputAudioPrompt != nil {
			todayAudioInputs += *g.NumInputAudioPrompt
		}
		if g.NumSearchResults != nil {
			todaySearchResults += *g.NumSearchResults
		}

		if g.Latency != nil && *g.Latency > 0 {
			todayLatencyMs += *g.Latency
			todayLatencyCount++
		}
		if g.GenerationTime != nil && *g.GenerationTime > 0 {
			todayGenerationMs += *g.GenerationTime
			todayGenerationCount++
		}
		if g.ModerationLatency != nil && *g.ModerationLatency > 0 {
			todayModerationMs += *g.ModerationLatency
			todayModerationCount++
		}
		if g.APIType != "" {
			apiTypeCounts[g.APIType]++
		}
		if g.Finish != "" {
			finishReasonCounts[g.Finish]++
		}
		if g.Origin != "" {
			originCounts[g.Origin]++
		}
		if g.Router != "" {
			routerCounts[g.Router]++
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

	for apiType, count := range apiTypeCounts {
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

	// Period cost metrics
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

	// Burn rate
	if burnCost > 0 {
		burnRate := burnCost // cost in the last 60 minutes ≈ cost/hour
		dailyProjected := burnRate * 24
		snap.Metrics["burn_rate"] = core.Metric{Used: &burnRate, Unit: "USD/hour", Window: "1h"}
		snap.Metrics["daily_projected"] = core.Metric{Used: &dailyProjected, Unit: "USD", Window: "24h"}
	}

	// DailySeries: cost, requests, and per-model tokens
	snap.DailySeries["cost"] = mapToSortedTimePoints(dailyCost)
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

	hasAnalyticsModelRows := strings.TrimSpace(snap.Raw["activity_rows"]) != "" && strings.TrimSpace(snap.Raw["activity_rows"]) != "0"
	if hasAnalyticsModelRows {
		if analyticsRowsStale(snap, time.Now().UTC()) {
			snap.Raw["activity_rows_stale"] = "true"
		} else {
			snap.Raw["activity_rows_stale"] = "false"
		}
	}
	// Always compute model/provider burn from live generation feed.
	// Analytics endpoints are cached by OpenRouter and can lag model mix updates.
	emitPerModelMetrics(modelStatsMap, snap)
	emitPerProviderMetrics(providerStatsMap, snap)
	snap.Raw["model_mix_source"] = "generation_live"

	return nil
}

func analyticsRowsStale(snap *core.UsageSnapshot, now time.Time) bool {
	cachedAtRaw := strings.TrimSpace(snap.Raw["activity_cached_at"])
	if cachedAtRaw != "" {
		if t, err := time.Parse(time.RFC3339, cachedAtRaw); err == nil {
			// Activity cache older than 10 minutes is considered stale for model mix.
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
	cutoff := time.Now().UTC().Add(-generationMaxAge)

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

func generationByokCost(g generationEntry) float64 {
	if !g.IsByok && g.UpstreamInferenceCost == nil {
		return 0
	}
	if g.UpstreamInferenceCost != nil && *g.UpstreamInferenceCost > 0 {
		return *g.UpstreamInferenceCost
	}
	if g.TotalCost > 0 {
		return g.TotalCost
	}
	return g.Usage
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
			core.AppendModelUsageRecord(snap, rec)
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
