package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultGlobalCodingBaseURL  = "https://api.z.ai/api/coding/paas/v4"
	defaultChinaCodingBaseURL   = "https://open.bigmodel.cn/api/coding/paas/v4"
	defaultGlobalMonitorBaseURL = "https://api.z.ai"
	defaultChinaMonitorBaseURL  = "https://open.bigmodel.cn"

	modelsPath     = "/models"
	quotaLimitPath = "/api/monitor/usage/quota/limit"
	modelUsagePath = "/api/monitor/usage/model-usage"
	toolUsagePath  = "/api/monitor/usage/tool-usage"
	creditsPath    = "/api/paas/v4/user/credit_grants"
)

type Provider struct {
	providerbase.Base
}

type providerState struct {
	hasQuotaData  bool
	hasUsageData  bool
	noPackage     bool
	limited       bool
	nearLimit     bool
	limitedReason string
}

type modelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID string `json:"id"`
	} `json:"data"`
	Error *apiError `json:"error"`
}

type monitorEnvelope struct {
	Code    any             `json:"code"`
	Msg     string          `json:"msg"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *apiError       `json:"error"`
}

type apiError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

type usageSample struct {
	Name      string
	Date      string
	Requests  float64
	Input     float64
	Output    float64
	Reasoning float64
	Total     float64
	CostUSD   float64
}

type usageRollup struct {
	Requests  float64
	Input     float64
	Output    float64
	Reasoning float64
	Total     float64
	CostUSD   float64
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "zai",
			Info: core.ProviderInfo{
				Name:         "Z.AI",
				Capabilities: []string{"coding_models", "quota_limit", "model_usage", "tool_usage", "credits"},
				DocURL:       "https://docs.z.ai/api-reference/model-api/list-models",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "ZAI_API_KEY",
				DefaultAccountID: "zai",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set ZAI_API_KEY to your Z.AI coding API token.",
					"Optional: set ZHIPUAI_API_KEY for China-region accounts.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	codingBase, monitorBase, region := resolveAPIBases(acct)

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.DailySeries = make(map[string][]core.TimePoint)
	snap.Raw["provider_region"] = region
	snap.SetAttribute("provider_region", region)

	if acct.ExtraData != nil {
		if planType := strings.TrimSpace(acct.ExtraData["plan_type"]); planType != "" {
			snap.Raw["plan_type"] = planType
			snap.SetAttribute("plan_type", planType)
		}
		if source := strings.TrimSpace(acct.ExtraData["source"]); source != "" {
			snap.Raw["auth_type"] = source
			snap.SetAttribute("auth_type", source)
		}
	}

	var state providerState

	if err := p.fetchModels(ctx, codingBase, apiKey, &snap, &state); err != nil {
		return core.UsageSnapshot{}, err
	}
	if snap.Status == core.StatusAuth {
		return snap, nil
	}

	if err := p.fetchQuotaLimit(ctx, monitorBase, apiKey, &snap, &state); err != nil {
		snap.Raw["quota_api"] = "error"
		snap.Raw["quota_limit_error"] = err.Error()
	}
	if err := p.fetchModelUsage(ctx, monitorBase, apiKey, &snap, &state); err != nil {
		snap.Raw["model_usage_api"] = "error"
		snap.Raw["model_usage_error"] = err.Error()
	}
	if err := p.fetchToolUsage(ctx, monitorBase, apiKey, &snap, &state); err != nil {
		snap.Raw["tool_usage_api"] = "error"
		snap.Raw["tool_usage_error"] = err.Error()
	}
	if err := p.fetchCredits(ctx, monitorBase, apiKey, &snap, &state); err != nil {
		snap.Raw["credits_api"] = "error"
		snap.Raw["credits_error"] = err.Error()
	}

	p.finalizeStatusAndMessage(&snap, &state)
	return snap, nil
}

func (p *Provider) fetchModels(ctx context.Context, codingBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	reqURL := joinURL(codingBase, modelsPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("zai: creating models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("zai: models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("zai: reading models response: %w", err)
	}
	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return nil
	case http.StatusTooManyRequests:
		code, msg := parseAPIError(body)
		if isNoPackageCode(code, msg) {
			state.limited = true
			state.noPackage = true
			state.limitedReason = "Insufficient balance or no active coding package"
			return nil
		}
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		code, msg := parseAPIError(body)
		if code != "" || msg != "" {
			snap.Status = core.StatusError
			snap.Message = fmt.Sprintf("models error (HTTP %d): %s", resp.StatusCode, firstNonEmpty(msg, code))
			return nil
		}
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("models error (HTTP %d)", resp.StatusCode)
		return nil
	}

	var payload modelsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("zai: parsing models response: %w", err)
	}

	if payload.Error != nil {
		code := anyToString(payload.Error.Code)
		if isNoPackageCode(code, payload.Error.Message) {
			state.limited = true
			state.noPackage = true
			state.limitedReason = "Insufficient balance or no active coding package"
			return nil
		}
		snap.Status = core.StatusError
		snap.Message = firstNonEmpty(payload.Error.Message, "models API returned an error")
		return nil
	}

	count := float64(len(payload.Data))
	setUsedMetric(snap, "available_models", count, "models", "current")
	snap.Raw["models_count"] = strconv.Itoa(len(payload.Data))
	snap.SetAttribute("models_count", strconv.Itoa(len(payload.Data)))
	if len(payload.Data) > 0 {
		active := strings.TrimSpace(payload.Data[0].ID)
		if active != "" {
			snap.Raw["active_model"] = active
			snap.SetAttribute("active_model", active)
		}
	}

	return nil
}

func (p *Provider) fetchQuotaLimit(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	status, body, err := p.requestMonitor(ctx, monitorBase, apiKey, quotaLimitPath, false)
	if err != nil {
		return fmt.Errorf("zai: quota limit request failed: %w", err)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("HTTP %d", status)
	}
	if status == http.StatusTooManyRequests {
		code, msg := parseAPIError(body)
		if isNoPackageCode(code, msg) {
			state.limited = true
			state.noPackage = true
			state.limitedReason = "Insufficient balance or no active coding package"
			snap.Raw["quota_api"] = "limited"
			return nil
		}
		return fmt.Errorf("HTTP 429")
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}

	var envelope monitorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parsing quota envelope: %w", err)
	}
	code := anyToString(envelope.Code)
	if envelope.Error != nil && code == "" {
		code = anyToString(envelope.Error.Code)
	}
	if isNoPackageCode(code, firstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
		state.limited = true
		state.noPackage = true
		state.limitedReason = "Insufficient balance or no active coding package"
		snap.Raw["quota_api"] = "limited"
		return nil
	}

	if isJSONEmpty(envelope.Data) {
		state.noPackage = true
		snap.Raw["quota_api"] = "empty"
		return nil
	}

	hasData := applyQuotaData(envelope.Data, snap, state)
	if hasData {
		state.hasQuotaData = true
		snap.Raw["quota_api"] = "ok"
	} else {
		state.noPackage = true
		snap.Raw["quota_api"] = "empty"
	}
	return nil
}

func (p *Provider) fetchModelUsage(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	status, body, err := p.requestMonitor(ctx, monitorBase, apiKey, modelUsagePath, true)
	if err != nil {
		return fmt.Errorf("zai: model usage request failed: %w", err)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("HTTP %d", status)
	}
	if status == http.StatusTooManyRequests {
		code, msg := parseAPIError(body)
		if isNoPackageCode(code, msg) {
			state.noPackage = true
			state.limited = true
			state.limitedReason = "Insufficient balance or no active coding package"
			snap.Raw["model_usage_api"] = "limited"
			return nil
		}
		return fmt.Errorf("HTTP 429")
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}

	var envelope monitorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parsing model usage envelope: %w", err)
	}
	code := anyToString(envelope.Code)
	if envelope.Error != nil && code == "" {
		code = anyToString(envelope.Error.Code)
	}
	if isNoPackageCode(code, firstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
		state.noPackage = true
		state.limited = true
		state.limitedReason = "Insufficient balance or no active coding package"
		snap.Raw["model_usage_api"] = "limited"
		return nil
	}

	samples := extractUsageSamples(envelope.Data, "model")
	if len(samples) == 0 {
		snap.Raw["model_usage_api"] = "empty"
		return nil
	}

	applyModelUsageSamples(samples, snap)
	state.hasUsageData = true
	snap.Raw["model_usage_api"] = "ok"
	return nil
}

func (p *Provider) fetchToolUsage(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	status, body, err := p.requestMonitor(ctx, monitorBase, apiKey, toolUsagePath, true)
	if err != nil {
		return fmt.Errorf("zai: tool usage request failed: %w", err)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("HTTP %d", status)
	}
	if status == http.StatusTooManyRequests {
		code, msg := parseAPIError(body)
		if isNoPackageCode(code, msg) {
			state.noPackage = true
			state.limited = true
			state.limitedReason = "Insufficient balance or no active coding package"
			snap.Raw["tool_usage_api"] = "limited"
			return nil
		}
		return fmt.Errorf("HTTP 429")
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}

	var envelope monitorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("parsing tool usage envelope: %w", err)
	}
	code := anyToString(envelope.Code)
	if envelope.Error != nil && code == "" {
		code = anyToString(envelope.Error.Code)
	}
	if isNoPackageCode(code, firstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
		state.noPackage = true
		state.limited = true
		state.limitedReason = "Insufficient balance or no active coding package"
		snap.Raw["tool_usage_api"] = "limited"
		return nil
	}

	samples := extractUsageSamples(envelope.Data, "tool")
	if len(samples) == 0 {
		snap.Raw["tool_usage_api"] = "empty"
		return nil
	}

	applyToolUsageSamples(samples, snap)
	state.hasUsageData = true
	snap.Raw["tool_usage_api"] = "ok"
	return nil
}

func (p *Provider) fetchCredits(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	status, body, err := p.requestMonitor(ctx, monitorBase, apiKey, creditsPath, false)
	if err != nil {
		return fmt.Errorf("zai: credits request failed: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parsing credits response: %w", err)
	}

	root, ok := payload.(map[string]any)
	if !ok {
		return nil
	}

	data := root
	if nested, ok := root["data"].(map[string]any); ok {
		data = nested
	}

	available, okAvailable := parseNumberFromMap(data,
		"total_available", "totalAvailable", "remaining_balance", "remainingBalance")
	used, okUsed := parseNumberFromMap(data, "total_used", "totalUsed", "usage", "total_usage")
	limit, okLimit := parseNumberFromMap(data, "total_granted", "totalGranted", "total_credits", "totalCredits")

	if okAvailable {
		credit := core.Metric{Remaining: core.Float64Ptr(available), Unit: "USD", Window: "current"}
		if okUsed {
			credit.Used = core.Float64Ptr(used)
		}
		if okLimit {
			credit.Limit = core.Float64Ptr(limit)
		}
		snap.Metrics["credit_balance"] = credit
		snap.Metrics["credits"] = credit
		snap.Raw["credits_api"] = "ok"
		state.hasUsageData = true
		return nil
	}

	snap.Raw["credits_api"] = "empty"
	return nil
}

func (p *Provider) requestMonitor(ctx context.Context, monitorBase, token, endpoint string, includeTimeRange bool) (int, []byte, error) {
	reqURL := joinURL(monitorBase, endpoint)
	if includeTimeRange {
		withRange, err := applyUsageRange(reqURL)
		if err != nil {
			return 0, nil, fmt.Errorf("building usage range: %w", err)
		}
		reqURL = withRange
	}

	status, body, err := doMonitorRequest(ctx, reqURL, token, false)
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		status, body, err = doMonitorRequest(ctx, reqURL, token, true)
	}
	return status, body, err
}

func (p *Provider) finalizeStatusAndMessage(snap *core.UsageSnapshot, state *providerState) {
	if snap.Status == core.StatusAuth {
		return
	}

	if state.hasQuotaData || state.hasUsageData {
		snap.Raw["subscription_status"] = "active"
		snap.SetAttribute("subscription_status", "active")
	} else if state.noPackage {
		snap.Raw["subscription_status"] = "inactive_or_free"
		snap.SetAttribute("subscription_status", "inactive_or_free")
	}

	if state.limited {
		snap.Status = core.StatusLimited
		if snap.Message == "" {
			snap.Message = firstNonEmpty(state.limitedReason, "Insufficient balance or no active coding package")
		}
		return
	}

	if state.nearLimit {
		snap.Status = core.StatusNearLimit
		if snap.Message == "" {
			if usage, ok := snap.Metrics["usage_five_hour"]; ok && usage.Used != nil {
				snap.Message = fmt.Sprintf("5h token usage %.0f%%", *usage.Used)
			} else {
				snap.Message = "Usage nearing limit"
			}
		}
		return
	}

	snap.Status = core.StatusOK
	if snap.Message != "" {
		return
	}

	if usage, ok := snap.Metrics["usage_five_hour"]; ok && usage.Used != nil {
		msg := fmt.Sprintf("5h token usage %.0f%%", *usage.Used)
		if mcp, ok := snap.Metrics["mcp_monthly_usage"]; ok && mcp.Used != nil && mcp.Limit != nil {
			msg += fmt.Sprintf(" · MCP %.0f/%.0f", *mcp.Used, *mcp.Limit)
		}
		snap.Message = msg
		return
	}

	if state.noPackage || (!state.hasQuotaData && !state.hasUsageData) {
		snap.Message = "Connected, but no active coding package/balance"
		return
	}

	if credit, ok := snap.Metrics["credit_balance"]; ok && credit.Remaining != nil {
		snap.Message = fmt.Sprintf("$%.2f remaining", *credit.Remaining)
		return
	}

	snap.Message = "OK"
}

func resolveAPIBases(acct core.AccountConfig) (codingBase, monitorBase, region string) {
	planType := ""
	if acct.ExtraData != nil {
		planType = strings.TrimSpace(acct.ExtraData["plan_type"])
	}

	isChina := strings.Contains(strings.ToLower(planType), "china")
	if acct.BaseURL != "" {
		base := strings.TrimRight(acct.BaseURL, "/")
		parsed, err := url.Parse(base)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			root := parsed.Scheme + "://" + parsed.Host
			path := strings.TrimRight(parsed.Path, "/")
			switch {
			case strings.Contains(path, "/api/coding/paas/v4"):
				codingBase = root + "/api/coding/paas/v4"
			case strings.HasSuffix(path, "/models"):
				codingBase = root + strings.TrimSuffix(path, "/models")
			case path == "" || path == "/":
				codingBase = root + "/api/coding/paas/v4"
			default:
				codingBase = root + path
			}
			monitorBase = root
			hostLower := strings.ToLower(parsed.Host)
			if strings.Contains(hostLower, "bigmodel.cn") {
				isChina = true
			}
		} else {
			codingBase = base
			monitorBase = strings.TrimSuffix(base, "/api/coding/paas/v4")
			monitorBase = strings.TrimSuffix(monitorBase, "/")
		}
	}

	if codingBase == "" || monitorBase == "" {
		if isChina {
			codingBase = defaultChinaCodingBaseURL
			monitorBase = defaultChinaMonitorBaseURL
		} else {
			codingBase = defaultGlobalCodingBaseURL
			monitorBase = defaultGlobalMonitorBaseURL
		}
	}

	region = "global"
	if isChina || strings.Contains(strings.ToLower(monitorBase), "bigmodel.cn") {
		region = "china"
	}
	return codingBase, monitorBase, region
}

func doMonitorRequest(ctx context.Context, reqURL, token string, bearer bool) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}

	authValue := token
	if bearer {
		authValue = "Bearer " + token
	}
	req.Header.Set("Authorization", authValue)
	req.Header.Set("Accept-Language", "en-US,en")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading response: %w", err)
	}
	return resp.StatusCode, body, nil
}

func applyQuotaData(raw json.RawMessage, snap *core.UsageSnapshot, state *providerState) bool {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}

	rows := extractLimitRows(payload)
	if len(rows) == 0 {
		return false
	}

	found := false
	for _, row := range rows {
		kind := strings.ToUpper(strings.TrimSpace(firstStringFromMap(row, "type", "limitType")))
		percentage, hasPct := parseNumberFromMap(row, "percentage", "usedPercent", "used_percentage")
		if hasPct && percentage <= 1 {
			percentage *= 100
		}

		switch kind {
		case "TOKENS_LIMIT":
			if hasPct {
				snap.Metrics["usage_five_hour"] = core.Metric{
					Used:   core.Float64Ptr(clamp(percentage, 0, 100)),
					Limit:  core.Float64Ptr(100),
					Unit:   "%",
					Window: "5h",
				}
				if percentage >= 100 {
					state.limited = true
				} else if percentage >= 80 {
					state.nearLimit = true
				}
			}

			limit, hasLimit := parseNumberFromMap(row, "usage", "limit", "quota")
			current, hasCurrent := parseNumberFromMap(row, "currentValue", "current", "used")
			if hasLimit && hasCurrent {
				remaining := math.Max(limit-current, 0)
				snap.Metrics["tokens_five_hour"] = core.Metric{
					Limit:     core.Float64Ptr(limit),
					Used:      core.Float64Ptr(current),
					Remaining: core.Float64Ptr(remaining),
					Unit:      "tokens",
					Window:    "5h",
				}
			}

			if resetRaw := firstAnyFromMap(row, "nextResetTime", "resetTime", "reset_at"); resetRaw != nil {
				if reset, ok := parseTimeValue(resetRaw); ok {
					snap.Resets["usage_five_hour"] = reset
				}
			}
			found = true

		case "TIME_LIMIT":
			limit, hasLimit := parseNumberFromMap(row, "usage", "limit", "quota")
			current, hasCurrent := parseNumberFromMap(row, "currentValue", "current", "used")
			if hasLimit && hasCurrent {
				remaining := math.Max(limit-current, 0)
				snap.Metrics["mcp_monthly_usage"] = core.Metric{
					Limit:     core.Float64Ptr(limit),
					Used:      core.Float64Ptr(current),
					Remaining: core.Float64Ptr(remaining),
					Unit:      "calls",
					Window:    "1mo",
				}
				found = true
			}
			if hasPct {
				if percentage >= 100 {
					state.limited = true
				} else if percentage >= 80 {
					state.nearLimit = true
				}
			}
		}
	}

	return found
}

func applyModelUsageSamples(samples []usageSample, snap *core.UsageSnapshot) {
	today := time.Now().UTC().Format("2006-01-02")

	total := usageRollup{}
	todayRollup := usageRollup{}
	modelTotals := make(map[string]*usageRollup)
	dailyCost := make(map[string]float64)
	dailyReq := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	modelDailyTokens := make(map[string]map[string]float64)

	for _, sample := range samples {
		key := sample.Name
		if key == "" {
			key = "unknown"
		}

		acc, ok := modelTotals[key]
		if !ok {
			acc = &usageRollup{}
			modelTotals[key] = acc
		}

		acc.Requests += sample.Requests
		acc.Input += sample.Input
		acc.Output += sample.Output
		acc.Reasoning += sample.Reasoning
		acc.Total += sample.Total
		acc.CostUSD += sample.CostUSD

		total.Requests += sample.Requests
		total.Input += sample.Input
		total.Output += sample.Output
		total.Reasoning += sample.Reasoning
		total.Total += sample.Total
		total.CostUSD += sample.CostUSD

		if sample.Date == today {
			todayRollup.Requests += sample.Requests
			todayRollup.Input += sample.Input
			todayRollup.Output += sample.Output
			todayRollup.Reasoning += sample.Reasoning
			todayRollup.Total += sample.Total
			todayRollup.CostUSD += sample.CostUSD
		}

		if sample.Date != "" {
			dailyCost[sample.Date] += sample.CostUSD
			dailyReq[sample.Date] += sample.Requests
			dailyTokens[sample.Date] += sample.Total
			if _, ok := modelDailyTokens[key]; !ok {
				modelDailyTokens[key] = make(map[string]float64)
			}
			modelDailyTokens[key][sample.Date] += sample.Total
		}
	}

	setUsedMetric(snap, "today_requests", todayRollup.Requests, "requests", "today")
	setUsedMetric(snap, "requests_today", todayRollup.Requests, "requests", "today")
	setUsedMetric(snap, "today_input_tokens", todayRollup.Input, "tokens", "today")
	setUsedMetric(snap, "today_output_tokens", todayRollup.Output, "tokens", "today")
	setUsedMetric(snap, "today_reasoning_tokens", todayRollup.Reasoning, "tokens", "today")
	setUsedMetric(snap, "today_tokens", todayRollup.Total, "tokens", "today")
	setUsedMetric(snap, "today_api_cost", todayRollup.CostUSD, "USD", "today")
	setUsedMetric(snap, "today_cost", todayRollup.CostUSD, "USD", "today")

	setUsedMetric(snap, "7d_requests", total.Requests, "requests", "7d")
	setUsedMetric(snap, "7d_tokens", total.Total, "tokens", "7d")
	setUsedMetric(snap, "7d_api_cost", total.CostUSD, "USD", "7d")

	setUsedMetric(snap, "active_models", float64(len(modelTotals)), "models", "7d")
	snap.Raw["model_usage_window"] = "7d"

	modelKeys := make([]string, 0, len(modelTotals))
	for k := range modelTotals {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)

	for _, model := range modelKeys {
		stats := modelTotals[model]
		slug := sanitizeMetricSlug(model)
		setUsedMetric(snap, "model_"+slug+"_requests", stats.Requests, "requests", "7d")
		setUsedMetric(snap, "model_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_total_tokens", stats.Total, "tokens", "7d")
		setUsedMetric(snap, "model_"+slug+"_cost_usd", stats.CostUSD, "USD", "7d")
		snap.Raw["model_"+slug+"_name"] = model

		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "api",
			Window:     "7d",
		}
		if stats.Input > 0 {
			rec.InputTokens = core.Float64Ptr(stats.Input)
		}
		if stats.Output > 0 {
			rec.OutputTokens = core.Float64Ptr(stats.Output)
		}
		if stats.Reasoning > 0 {
			rec.ReasoningTokens = core.Float64Ptr(stats.Reasoning)
		}
		if stats.Total > 0 {
			rec.TotalTokens = core.Float64Ptr(stats.Total)
		}
		if stats.CostUSD > 0 {
			rec.CostUSD = core.Float64Ptr(stats.CostUSD)
		}
		if stats.Requests > 0 {
			rec.Requests = core.Float64Ptr(stats.Requests)
		}
		snap.AppendModelUsage(rec)
	}

	snap.DailySeries["cost"] = mapToSeries(dailyCost)
	snap.DailySeries["requests"] = mapToSeries(dailyReq)
	snap.DailySeries["tokens"] = mapToSeries(dailyTokens)

	type modelTotal struct {
		name   string
		tokens float64
	}
	var ranked []modelTotal
	for model, stats := range modelTotals {
		ranked = append(ranked, modelTotal{name: model, tokens: stats.Total})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].tokens > ranked[j].tokens })
	if len(ranked) > 3 {
		ranked = ranked[:3]
	}
	for _, entry := range ranked {
		if dayMap, ok := modelDailyTokens[entry.name]; ok {
			key := "tokens_" + sanitizeMetricSlug(entry.name)
			snap.DailySeries[key] = mapToSeries(dayMap)
		}
	}
}

func applyToolUsageSamples(samples []usageSample, snap *core.UsageSnapshot) {
	today := time.Now().UTC().Format("2006-01-02")
	totalCalls := 0.0
	todayCalls := 0.0
	toolTotals := make(map[string]*usageRollup)
	dailyCalls := make(map[string]float64)

	for _, sample := range samples {
		tool := sample.Name
		if tool == "" {
			tool = "unknown"
		}

		acc, ok := toolTotals[tool]
		if !ok {
			acc = &usageRollup{}
			toolTotals[tool] = acc
		}
		acc.Requests += sample.Requests
		acc.CostUSD += sample.CostUSD

		totalCalls += sample.Requests
		if sample.Date == today {
			todayCalls += sample.Requests
		}
		if sample.Date != "" {
			dailyCalls[sample.Date] += sample.Requests
		}
	}

	setUsedMetric(snap, "tool_calls_today", todayCalls, "calls", "today")
	setUsedMetric(snap, "today_tool_calls", todayCalls, "calls", "today")
	setUsedMetric(snap, "7d_tool_calls", totalCalls, "calls", "7d")

	keys := make([]string, 0, len(toolTotals))
	for tool := range toolTotals {
		keys = append(keys, tool)
	}
	sort.Strings(keys)
	for _, tool := range keys {
		stats := toolTotals[tool]
		slug := sanitizeMetricSlug(tool)
		setUsedMetric(snap, "tool_"+slug+"_calls", stats.Requests, "calls", "7d")
		setUsedMetric(snap, "tool_"+slug+"_cost_usd", stats.CostUSD, "USD", "7d")
		snap.Raw["tool_"+slug+"_name"] = tool
	}

	if len(dailyCalls) > 0 {
		snap.DailySeries["tool_calls"] = mapToSeries(dailyCalls)
	}
}

func extractUsageSamples(raw json.RawMessage, kind string) []usageSample {
	if isJSONEmpty(raw) {
		return nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	rows := extractUsageRows(payload)
	if len(rows) == 0 {
		return nil
	}

	samples := make([]usageSample, 0, len(rows))
	namedRows := 0
	for _, row := range rows {
		sample := usageSample{
			Date: normalizeDate(firstAnyFromMap(row, "date", "day", "time", "timestamp", "created_at", "createdAt")),
		}

		if kind == "model" {
			sample.Name = firstStringFromMap(row, "model", "model_id", "modelId", "model_name", "modelName")
		} else {
			sample.Name = firstStringFromMap(row, "tool", "tool_name", "toolName", "name", "tool_id", "toolId")
		}
		if sample.Name != "" {
			namedRows++
		}

		sample.Requests, _ = parseNumberFromMap(row, "requests", "request_count", "requestCount", "calls", "count", "usageCount")
		sample.Input, _ = parseNumberFromMap(row, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")
		sample.Output, _ = parseNumberFromMap(row, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
		sample.Reasoning, _ = parseNumberFromMap(row, "reasoning_tokens", "reasoningTokens")
		sample.Total, _ = parseNumberFromMap(row, "total_tokens", "totalTokens", "tokens")
		if sample.Total == 0 {
			sample.Total = sample.Input + sample.Output + sample.Reasoning
		}
		sample.CostUSD = parseCostUSD(row)

		if sample.Requests > 0 || sample.Total > 0 || sample.CostUSD > 0 || sample.Name != "" {
			samples = append(samples, sample)
		}
	}

	if namedRows == 0 {
		return samples
	}

	filtered := make([]usageSample, 0, len(samples))
	for _, sample := range samples {
		if strings.TrimSpace(sample.Name) != "" {
			filtered = append(filtered, sample)
		}
	}
	return filtered
}

func extractUsageRows(v any) []map[string]any {
	switch value := v.(type) {
	case []any:
		rows := mapsFromArray(value)
		if len(rows) > 0 {
			return rows
		}
		var nested []map[string]any
		for _, item := range value {
			nested = append(nested, extractUsageRows(item)...)
		}
		return nested
	case map[string]any:
		if looksLikeUsageRow(value) {
			return []map[string]any{value}
		}

		keys := []string{
			"data", "items", "list", "rows", "records", "usage", "model_usage", "tool_usage", "result",
		}
		for _, key := range keys {
			if nested, ok := value[key]; ok {
				rows := extractUsageRows(nested)
				if len(rows) > 0 {
					return rows
				}
			}
		}

		var best []map[string]any
		for _, nested := range value {
			rows := extractUsageRows(nested)
			if len(rows) > len(best) {
				best = rows
			}
		}
		return best
	default:
		return nil
	}
}

func extractLimitRows(v any) []map[string]any {
	switch value := v.(type) {
	case []any:
		return mapsFromArray(value)
	case map[string]any:
		if _, ok := value["type"]; ok {
			return []map[string]any{value}
		}
		for _, key := range []string{"limits", "items", "data"} {
			if nested, ok := value[key]; ok {
				rows := extractLimitRows(nested)
				if len(rows) > 0 {
					return rows
				}
			}
		}
		var all []map[string]any
		for _, nested := range value {
			rows := extractLimitRows(nested)
			all = append(all, rows...)
		}
		return all
	default:
		return nil
	}
}

func mapsFromArray(values []any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for _, item := range values {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func looksLikeUsageRow(row map[string]any) bool {
	if row == nil {
		return false
	}
	hasName := firstStringFromMap(row, "model", "model_id", "modelName", "tool", "tool_name", "name") != ""
	if hasName {
		return true
	}
	_, hasReq := parseNumberFromMap(row, "requests", "request_count", "calls", "count")
	_, hasTokens := parseNumberFromMap(row, "total_tokens", "tokens", "input_tokens", "output_tokens")
	_, hasCost := parseNumberFromMap(row, "cost", "total_cost", "cost_usd", "total_cost_usd")
	return hasReq || hasTokens || hasCost
}

func applyUsageRange(reqURL string) (string, error) {
	parsed, err := url.Parse(reqURL)
	if err != nil {
		return "", err
	}
	start, end := usageWindow()
	q := parsed.Query()
	q.Set("startTime", start)
	q.Set("endTime", end)
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func usageWindow() (start, end string) {
	now := time.Now().UTC()
	startTime := time.Date(now.Year(), now.Month(), now.Day()-6, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	return startTime.Format("2006-01-02 15:04:05"), endTime.Format("2006-01-02 15:04:05")
}

func joinURL(base, endpoint string) string {
	trimmedBase := strings.TrimRight(base, "/")
	trimmedEndpoint := strings.TrimLeft(endpoint, "/")
	return trimmedBase + "/" + trimmedEndpoint
}

func parseAPIError(body []byte) (code, msg string) {
	var payload struct {
		Code    any       `json:"code"`
		Msg     string    `json:"msg"`
		Message string    `json:"message"`
		Error   *apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}

	if payload.Error != nil {
		if payload.Error.Message != "" {
			msg = payload.Error.Message
		}
		if payload.Error.Code != nil {
			code = anyToString(payload.Error.Code)
		}
	}
	if code == "" && payload.Code != nil {
		code = anyToString(payload.Code)
	}
	if msg == "" {
		msg = firstNonEmpty(payload.Message, payload.Msg)
	}
	return code, msg
}

func parseCostUSD(row map[string]any) float64 {
	value, key, ok := firstNumberWithKey(row,
		"cost_usd", "costUSD", "total_cost_usd", "totalCostUSD",
		"total_cost", "totalCost", "api_cost", "apiCost",
		"cost", "amount", "total_amount", "totalAmount",
		"cost_cents", "costCents", "total_cost_cents", "totalCostCents",
	)
	if !ok {
		return 0
	}
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "cent") {
		return value / 100
	}
	return value
}

func parseNumberFromMap(row map[string]any, keys ...string) (float64, bool) {
	value, _, ok := firstNumberWithKey(row, keys...)
	return value, ok
}

func firstNumberWithKey(row map[string]any, keys ...string) (float64, string, bool) {
	for _, key := range keys {
		raw, ok := row[key]
		if !ok {
			continue
		}
		if parsed, ok := parseFloat(raw); ok {
			return parsed, key, true
		}
	}
	return 0, "", false
}

func parseFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case int32:
		return float64(value), true
	case int16:
		return float64(value), true
	case int8:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint64:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint8:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func firstStringFromMap(row map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := row[key]
		if !ok || raw == nil {
			continue
		}
		str := strings.TrimSpace(anyToString(raw))
		if str != "" {
			return str
		}
	}
	return ""
}

func firstAnyFromMap(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if raw, ok := row[key]; ok {
			return raw
		}
	}
	return nil
}

func anyToString(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case json.Number:
		return value.String()
	case float64:
		if math.Mod(value, 1) == 0 {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case bool:
		return strconv.FormatBool(value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func normalizeDate(raw any) string {
	if raw == nil {
		return ""
	}

	if ts, ok := parseTimeValue(raw); ok {
		return ts.UTC().Format("2006-01-02")
	}

	value := strings.TrimSpace(anyToString(raw))
	if value == "" {
		return ""
	}
	if len(value) >= 10 {
		candidate := value[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func parseTimeValue(raw any) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}

	if n, ok := parseFloat(raw); ok {
		if n <= 0 {
			return time.Time{}, false
		}
		sec := int64(n)
		if n > 1e12 {
			sec = int64(n / 1000)
		}
		return time.Unix(sec, 0).UTC(), true
	}

	value := strings.TrimSpace(anyToString(raw))
	if value == "" {
		return time.Time{}, false
	}

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}

	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n > 1e12 {
			return time.Unix(n/1000, 0).UTC(), true
		}
		return time.Unix(n, 0).UTC(), true
	}

	return time.Time{}, false
}

func isJSONEmpty(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null" || trimmed == "{}" || trimmed == "[]"
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if key == "" || value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   core.Float64Ptr(value),
		Unit:   unit,
		Window: window,
	}
}

func mapToSeries(input map[string]float64) []core.TimePoint {
	out := make([]core.TimePoint, 0, len(input))
	for day, value := range input {
		if strings.TrimSpace(day) == "" {
			continue
		}
		out = append(out, core.TimePoint{
			Date:  day,
			Value: value,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func sanitizeMetricSlug(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '_':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return "unknown"
	}
	return slug
}

func clamp(value, minVal, maxVal float64) float64 {
	return math.Min(math.Max(value, minVal), maxVal)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func apiErrorMessage(err *apiError) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Message)
}

func isNoPackageCode(code, msg string) bool {
	code = strings.TrimSpace(code)
	if code == "1113" {
		return true
	}
	lowerMsg := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(lowerMsg, "insufficient balance") ||
		strings.Contains(lowerMsg, "no resource package") ||
		strings.Contains(lowerMsg, "no active coding package")
}
