package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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
	Client    string
	Source    string
	Provider  string
	Interface string
	Endpoint  string
	Language  string
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

type payloadNumericStat struct {
	Count int
	Sum   float64
	Last  float64
	Min   float64
	Max   float64
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

	resp, err := p.Client().Do(req)
	if err != nil {
		return fmt.Errorf("zai: models request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("zai: reading models response: %w", err)
	}
	captureEndpointPayload(snap, "models", body)
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
			snap.Message = fmt.Sprintf("models error (HTTP %d): %s", resp.StatusCode, core.FirstNonEmpty(msg, code))
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
		snap.Message = core.FirstNonEmpty(payload.Error.Message, "models API returned an error")
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
	captureEndpointPayload(snap, "quota_limit", body)

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
	if isNoPackageCode(code, core.FirstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
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
	captureEndpointPayload(snap, "model_usage", body)

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
	if isNoPackageCode(code, core.FirstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
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
	captureEndpointPayload(snap, "tool_usage", body)

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
	if isNoPackageCode(code, core.FirstNonEmpty(envelope.Msg, apiErrorMessage(envelope.Error))) {
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
	captureEndpointPayload(snap, "credits", body)
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

	available, hasAvailable := parseNumberFromMap(root,
		"total_available", "totalAvailable",
		"remaining_balance", "remainingBalance",
		"available_balance", "availableBalance",
		"available", "balance", "remaining")
	used, hasUsed := parseNumberFromMap(root,
		"total_used", "totalUsed", "usage", "total_usage",
		"used_balance", "usedBalance", "spent_balance", "spentBalance",
		"spent", "consumed")
	limit, hasLimit := parseNumberFromMap(root,
		"total_granted", "totalGranted", "total_credits", "totalCredits",
		"credit_limit", "creditLimit", "limit")

	data := firstAnyFromMap(root, "data", "credit_grants", "creditGrants", "grants")
	if nested, ok := data.(map[string]any); ok {
		if !hasAvailable {
			available, hasAvailable = parseNumberFromMap(nested,
				"total_available", "totalAvailable",
				"remaining_balance", "remainingBalance",
				"available_balance", "availableBalance",
				"available", "balance", "remaining")
		}
		if !hasUsed {
			used, hasUsed = parseNumberFromMap(nested,
				"total_used", "totalUsed", "usage", "total_usage",
				"used_balance", "usedBalance", "spent_balance", "spentBalance",
				"spent", "consumed")
		}
		if !hasLimit {
			limit, hasLimit = parseNumberFromMap(nested,
				"total_granted", "totalGranted", "total_credits", "totalCredits",
				"credit_limit", "creditLimit", "limit")
		}
	}

	grantRows := extractCreditGrantRows(root)
	if len(grantRows) > 0 {
		grantLimitTotal := 0.0
		grantUsedTotal := 0.0
		grantAvailableTotal := 0.0
		hasGrantLimit := false
		hasGrantUsed := false
		hasGrantAvailable := false
		activeGrants := 0
		expiringSoon := 0
		now := time.Now().UTC()

		for _, grant := range grantRows {
			grantLimit, okLimitGrant := parseNumberFromMap(grant,
				"grant_amount", "grantAmount",
				"total_granted", "totalGranted",
				"amount", "total_amount", "totalAmount")
			grantUsed, okUsedGrant := parseNumberFromMap(grant,
				"used_amount", "usedAmount",
				"used", "usage", "spent")
			grantAvailable, okAvailableGrant := parseNumberFromMap(grant,
				"available_amount", "availableAmount",
				"remaining_amount", "remainingAmount",
				"remaining_balance", "remainingBalance",
				"available_balance", "availableBalance",
				"available", "remaining")

			if !okAvailableGrant && okLimitGrant && okUsedGrant {
				grantAvailable = math.Max(grantLimit-grantUsed, 0)
				okAvailableGrant = true
			}
			if !okUsedGrant && okLimitGrant && okAvailableGrant {
				grantUsed = math.Max(grantLimit-grantAvailable, 0)
				okUsedGrant = true
			}
			if !okLimitGrant && okAvailableGrant && okUsedGrant {
				grantLimit = grantAvailable + grantUsed
				okLimitGrant = true
			}

			if okLimitGrant {
				grantLimitTotal += grantLimit
				hasGrantLimit = true
			}
			if okUsedGrant {
				grantUsedTotal += grantUsed
				hasGrantUsed = true
			}
			if okAvailableGrant {
				grantAvailableTotal += grantAvailable
				hasGrantAvailable = true
				if grantAvailable > 0 {
					activeGrants++
				}
			}

			if exp, ok := parseCreditGrantExpiry(grant); ok &&
				exp.After(now) && exp.Before(now.Add(30*24*time.Hour)) && okAvailableGrant && grantAvailable > 0 {
				expiringSoon++
			}
		}

		if !hasAvailable && hasGrantAvailable {
			available = grantAvailableTotal
			hasAvailable = true
		}
		if !hasUsed && hasGrantUsed {
			used = grantUsedTotal
			hasUsed = true
		}
		if !hasLimit && hasGrantLimit {
			limit = grantLimitTotal
			hasLimit = true
		}

		snap.Raw["credit_grants_count"] = strconv.Itoa(len(grantRows))
		setUsedMetric(snap, "credit_grants_count", float64(len(grantRows)), "grants", "current")
		if activeGrants > 0 {
			snap.Raw["credit_active_grants"] = strconv.Itoa(activeGrants)
			snap.SetAttribute("credit_active_grants", strconv.Itoa(activeGrants))
			setUsedMetric(snap, "credit_active_grants", float64(activeGrants), "grants", "current")
		}
		if expiringSoon > 0 {
			snap.Raw["credit_expiring_30d"] = strconv.Itoa(expiringSoon)
			setUsedMetric(snap, "credit_expiring_30d", float64(expiringSoon), "grants", "30d")
		}
	}

	if !hasAvailable && hasLimit && hasUsed {
		available = math.Max(limit-used, 0)
		hasAvailable = true
	}
	if !hasUsed && hasLimit && hasAvailable {
		used = math.Max(limit-available, 0)
		hasUsed = true
	}
	if !hasLimit && hasAvailable && hasUsed {
		limit = available + used
		hasLimit = true
	}

	if !hasAvailable && !hasUsed && !hasLimit {
		snap.Raw["credits_api"] = "empty"
		return nil
	}

	credit := core.Metric{Unit: "USD", Window: "current"}
	if hasAvailable {
		credit.Remaining = core.Float64Ptr(available)
		setUsedMetric(snap, "available_balance", available, "USD", "current")
		setUsedMetric(snap, "limit_remaining", available, "USD", "current")
	}
	if hasUsed {
		credit.Used = core.Float64Ptr(used)
	}
	if hasLimit {
		credit.Limit = core.Float64Ptr(limit)
	}
	if hasLimit && hasUsed && credit.Remaining == nil {
		credit.Remaining = core.Float64Ptr(math.Max(limit-used, 0))
	}

	snap.Metrics["credit_balance"] = credit
	snap.Metrics["credits"] = credit

	if hasLimit && hasUsed {
		remaining := math.Max(limit-used, 0)
		snap.Metrics["spend_limit"] = core.Metric{
			Limit:     core.Float64Ptr(limit),
			Used:      core.Float64Ptr(used),
			Remaining: core.Float64Ptr(remaining),
			Unit:      "USD",
			Window:    "current",
		}
		snap.Metrics["plan_spend"] = core.Metric{
			Limit:     core.Float64Ptr(limit),
			Used:      core.Float64Ptr(used),
			Remaining: core.Float64Ptr(remaining),
			Unit:      "USD",
			Window:    "current",
		}
		pct := 0.0
		if limit > 0 {
			pct = clamp((used/limit)*100, 0, 100)
		}
		snap.Metrics["plan_percent_used"] = core.Metric{
			Used:   core.Float64Ptr(pct),
			Limit:  core.Float64Ptr(100),
			Unit:   "%",
			Window: "current",
		}
	}

	snap.Raw["credits_api"] = "ok"
	state.hasUsageData = true
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

	status, body, err := doMonitorRequest(ctx, reqURL, token, false, p.Client())
	if err != nil {
		return 0, nil, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		status, body, err = doMonitorRequest(ctx, reqURL, token, true, p.Client())
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
			snap.Message = core.FirstNonEmpty(state.limitedReason, "Insufficient balance or no active coding package")
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

func applyModelUsageSamples(samples []usageSample, snap *core.UsageSnapshot) {
	today := time.Now().UTC().Format("2006-01-02")
	hasNamedModelRows := false
	for _, sample := range samples {
		if strings.TrimSpace(sample.Name) != "" {
			hasNamedModelRows = true
			break
		}
	}

	total := usageRollup{}
	todayRollup := usageRollup{}
	modelTotals := make(map[string]*usageRollup)
	clientTotals := make(map[string]*usageRollup)
	sourceTotals := make(map[string]*usageRollup)
	providerTotals := make(map[string]*usageRollup)
	interfaceTotals := make(map[string]*usageRollup)
	endpointTotals := make(map[string]*usageRollup)
	languageTotals := make(map[string]*usageRollup)
	dailyCost := make(map[string]float64)
	dailyReq := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	modelDailyTokens := make(map[string]map[string]float64)
	clientDailyReq := make(map[string]map[string]float64)
	sourceDailyReq := make(map[string]map[string]float64)
	sourceTodayReq := make(map[string]float64)

	for _, sample := range samples {
		modelName := strings.TrimSpace(sample.Name)
		useRow := !hasNamedModelRows || modelName != ""
		if !useRow {
			lang := normalizeUsageDimension(sample.Language)
			if lang != "" {
				accumulateUsageRollup(languageTotals, lang, sample)
			}
			if client := normalizeUsageDimension(sample.Client); client != "" {
				accumulateUsageRollup(clientTotals, client, sample)
				if sample.Date != "" {
					if _, ok := clientDailyReq[client]; !ok {
						clientDailyReq[client] = make(map[string]float64)
					}
					clientDailyReq[client][sample.Date] += sample.Requests
				}
			}
			if source := normalizeUsageDimension(sample.Source); source != "" {
				accumulateUsageRollup(sourceTotals, source, sample)
				if sample.Date == today {
					sourceTodayReq[source] += sample.Requests
				}
				if sample.Date != "" {
					if _, ok := sourceDailyReq[source]; !ok {
						sourceDailyReq[source] = make(map[string]float64)
					}
					sourceDailyReq[source][sample.Date] += sample.Requests
				}
			}
			if provider := normalizeUsageDimension(sample.Provider); provider != "" {
				accumulateUsageRollup(providerTotals, provider, sample)
			}
			if iface := normalizeUsageDimension(sample.Interface); iface != "" {
				accumulateUsageRollup(interfaceTotals, iface, sample)
			}
			if endpoint := normalizeUsageDimension(sample.Endpoint); endpoint != "" {
				accumulateUsageRollup(endpointTotals, endpoint, sample)
			}
			continue
		}
		accumulateRollupValues(&total, sample)
		if modelName != "" {
			accumulateUsageRollup(modelTotals, modelName, sample)
		}

		if sample.Date == today {
			accumulateRollupValues(&todayRollup, sample)
		}

		if sample.Date != "" && modelName != "" {
			dailyCost[sample.Date] += sample.CostUSD
			dailyReq[sample.Date] += sample.Requests
			dailyTokens[sample.Date] += sample.Total
			if _, ok := modelDailyTokens[modelName]; !ok {
				modelDailyTokens[modelName] = make(map[string]float64)
			}
			modelDailyTokens[modelName][sample.Date] += sample.Total
		}

		if client := normalizeUsageDimension(sample.Client); client != "" {
			accumulateUsageRollup(clientTotals, client, sample)
			if sample.Date != "" {
				if _, ok := clientDailyReq[client]; !ok {
					clientDailyReq[client] = make(map[string]float64)
				}
				clientDailyReq[client][sample.Date] += sample.Requests
			}
		}

		if source := normalizeUsageDimension(sample.Source); source != "" {
			accumulateUsageRollup(sourceTotals, source, sample)
			if sample.Date == today {
				sourceTodayReq[source] += sample.Requests
			}
			if sample.Date != "" {
				if _, ok := sourceDailyReq[source]; !ok {
					sourceDailyReq[source] = make(map[string]float64)
				}
				sourceDailyReq[source][sample.Date] += sample.Requests
			}
		}

		if provider := normalizeUsageDimension(sample.Provider); provider != "" {
			accumulateUsageRollup(providerTotals, provider, sample)
		}
		if iface := normalizeUsageDimension(sample.Interface); iface != "" {
			accumulateUsageRollup(interfaceTotals, iface, sample)
		}
		if endpoint := normalizeUsageDimension(sample.Endpoint); endpoint != "" {
			accumulateUsageRollup(endpointTotals, endpoint, sample)
		}
		lang := normalizeUsageDimension(sample.Language)
		if lang == "" {
			lang = inferModelUsageLanguage(modelName)
		}
		if lang != "" {
			accumulateUsageRollup(languageTotals, lang, sample)
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
	setUsedMetric(snap, "window_requests", total.Requests, "requests", "7d")
	setUsedMetric(snap, "window_tokens", total.Total, "tokens", "7d")
	setUsedMetric(snap, "window_cost", total.CostUSD, "USD", "7d")

	setUsedMetric(snap, "active_models", float64(len(modelTotals)), "models", "7d")
	snap.Raw["model_usage_window"] = "7d"
	snap.Raw["activity_models"] = strconv.Itoa(len(modelTotals))
	snap.SetAttribute("activity_models", strconv.Itoa(len(modelTotals)))

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

	clientKeys := sortedUsageRollupKeys(clientTotals)
	for _, client := range clientKeys {
		stats := clientTotals[client]
		slug := sanitizeMetricSlug(client)
		setUsedMetric(snap, "client_"+slug+"_total_tokens", stats.Total, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_reasoning_tokens", stats.Reasoning, "tokens", "7d")
		setUsedMetric(snap, "client_"+slug+"_requests", stats.Requests, "requests", "7d")
		snap.Raw["client_"+slug+"_name"] = client
	}

	sourceKeys := sortedUsageRollupKeys(sourceTotals)
	for _, source := range sourceKeys {
		stats := sourceTotals[source]
		slug := sanitizeMetricSlug(source)
		setUsedMetric(snap, "source_"+slug+"_requests", stats.Requests, "requests", "7d")
		if reqToday := sourceTodayReq[source]; reqToday > 0 {
			setUsedMetric(snap, "source_"+slug+"_requests_today", reqToday, "requests", "1d")
		}
	}

	providerKeys := sortedUsageRollupKeys(providerTotals)
	for _, provider := range providerKeys {
		stats := providerTotals[provider]
		slug := sanitizeMetricSlug(provider)
		setUsedMetric(snap, "provider_"+slug+"_cost_usd", stats.CostUSD, "USD", "7d")
		setUsedMetric(snap, "provider_"+slug+"_requests", stats.Requests, "requests", "7d")
		setUsedMetric(snap, "provider_"+slug+"_input_tokens", stats.Input, "tokens", "7d")
		setUsedMetric(snap, "provider_"+slug+"_output_tokens", stats.Output, "tokens", "7d")
		snap.Raw["provider_"+slug+"_name"] = provider
	}

	interfaceKeys := sortedUsageRollupKeys(interfaceTotals)
	for _, iface := range interfaceKeys {
		stats := interfaceTotals[iface]
		slug := sanitizeMetricSlug(iface)
		setUsedMetric(snap, "interface_"+slug, stats.Requests, "calls", "7d")
	}

	endpointKeys := sortedUsageRollupKeys(endpointTotals)
	for _, endpoint := range endpointKeys {
		stats := endpointTotals[endpoint]
		slug := sanitizeMetricSlug(endpoint)
		setUsedMetric(snap, "endpoint_"+slug+"_requests", stats.Requests, "requests", "7d")
	}

	languageKeys := sortedUsageRollupKeys(languageTotals)
	languageReqSummary := make(map[string]float64, len(languageKeys))
	for _, lang := range languageKeys {
		stats := languageTotals[lang]
		slug := sanitizeMetricSlug(lang)
		value := stats.Requests
		if value <= 0 {
			value = stats.Total
		}
		setUsedMetric(snap, "lang_"+slug, value, "requests", "7d")
		languageReqSummary[lang] = stats.Requests
	}
	setUsedMetric(snap, "active_languages", float64(len(languageTotals)), "languages", "7d")
	setUsedMetric(snap, "activity_providers", float64(len(providerTotals)), "providers", "7d")

	snap.DailySeries["cost"] = core.SortedTimePoints(dailyCost)
	snap.DailySeries["requests"] = core.SortedTimePoints(dailyReq)
	snap.DailySeries["tokens"] = core.SortedTimePoints(dailyTokens)

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
			snap.DailySeries[key] = core.SortedTimePoints(dayMap)
		}
	}

	for client, dayMap := range clientDailyReq {
		if len(dayMap) == 0 {
			continue
		}
		snap.DailySeries["usage_client_"+sanitizeMetricSlug(client)] = core.SortedTimePoints(dayMap)
	}
	for source, dayMap := range sourceDailyReq {
		if len(dayMap) == 0 {
			continue
		}
		snap.DailySeries["usage_source_"+sanitizeMetricSlug(source)] = core.SortedTimePoints(dayMap)
	}

	modelShare := make(map[string]float64, len(modelTotals))
	modelUnit := "tok"
	for model, stats := range modelTotals {
		if stats.Total > 0 {
			modelShare[model] = stats.Total
			continue
		}
		if stats.Requests > 0 {
			modelShare[model] = stats.Requests
			modelUnit = "req"
		}
	}
	if summary := summarizeShareUsage(modelShare, 6); summary != "" {
		snap.Raw["model_usage"] = summary
		snap.Raw["model_usage_unit"] = modelUnit
	}
	clientShare := make(map[string]float64, len(clientTotals))
	for client, stats := range clientTotals {
		if stats.Total > 0 {
			clientShare[client] = stats.Total
		} else if stats.Requests > 0 {
			clientShare[client] = stats.Requests
		}
	}
	if summary := summarizeShareUsage(clientShare, 6); summary != "" {
		snap.Raw["client_usage"] = summary
	}
	sourceShare := make(map[string]float64, len(sourceTotals))
	for source, stats := range sourceTotals {
		if stats.Requests > 0 {
			sourceShare[source] = stats.Requests
		}
	}
	if summary := summarizeCountUsage(sourceShare, "req", 6); summary != "" {
		snap.Raw["source_usage"] = summary
	}
	providerShare := make(map[string]float64, len(providerTotals))
	for provider, stats := range providerTotals {
		if stats.CostUSD > 0 {
			providerShare[provider] = stats.CostUSD
		} else if stats.Requests > 0 {
			providerShare[provider] = stats.Requests
		}
	}
	if summary := summarizeShareUsage(providerShare, 6); summary != "" {
		snap.Raw["provider_usage"] = summary
	}
	if summary := summarizeCountUsage(languageReqSummary, "req", 8); summary != "" {
		snap.Raw["language_usage"] = summary
	}

	snap.Raw["activity_days"] = strconv.Itoa(len(dailyReq))
	snap.Raw["activity_clients"] = strconv.Itoa(len(clientTotals))
	snap.Raw["activity_sources"] = strconv.Itoa(len(sourceTotals))
	snap.Raw["activity_providers"] = strconv.Itoa(len(providerTotals))
	snap.Raw["activity_languages"] = strconv.Itoa(len(languageTotals))
	snap.Raw["activity_endpoints"] = strconv.Itoa(len(endpointTotals))
	snap.SetAttribute("activity_days", snap.Raw["activity_days"])
	snap.SetAttribute("activity_clients", snap.Raw["activity_clients"])
	snap.SetAttribute("activity_sources", snap.Raw["activity_sources"])
	snap.SetAttribute("activity_providers", snap.Raw["activity_providers"])
	snap.SetAttribute("activity_languages", snap.Raw["activity_languages"])
	snap.SetAttribute("activity_endpoints", snap.Raw["activity_endpoints"])
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
		setUsedMetric(snap, "tool_"+slug, stats.Requests, "calls", "7d")
		setUsedMetric(snap, "toolcost_"+slug+"_usd", stats.CostUSD, "USD", "7d")
		snap.Raw["tool_"+slug+"_name"] = tool
	}

	if len(dailyCalls) > 0 {
		snap.DailySeries["tool_calls"] = core.SortedTimePoints(dailyCalls)
	}

	toolSummary := make(map[string]float64, len(toolTotals))
	for tool, stats := range toolTotals {
		if stats.Requests > 0 {
			toolSummary[tool] = stats.Requests
		}
	}
	if summary := summarizeCountUsage(toolSummary, "calls", 8); summary != "" {
		snap.Raw["tool_usage"] = summary
	}
}
