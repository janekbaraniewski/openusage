package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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

	if acct.RuntimeHints != nil {
		if planType := strings.TrimSpace(acct.RuntimeHints["plan_type"]); planType != "" {
			snap.Raw["plan_type"] = planType
			snap.SetAttribute("plan_type", planType)
		}
		if source := strings.TrimSpace(acct.RuntimeHints["source"]); source != "" {
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
	res, err := p.evaluateMonitorEndpoint(ctx, monitorBase, apiKey, quotaLimitPath, false,
		"quota_limit", "quota_api", snap, state)
	if err != nil || res.Outcome != outcomeOK {
		return err
	}

	if applyQuotaData(res.Envelope.Data, snap, state) {
		state.hasQuotaData = true
		snap.Raw["quota_api"] = "ok"
	} else {
		state.noPackage = true
		snap.Raw["quota_api"] = "empty"
	}
	return nil
}

func (p *Provider) fetchModelUsage(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	res, err := p.evaluateMonitorEndpoint(ctx, monitorBase, apiKey, modelUsagePath, true,
		"model_usage", "model_usage_api", snap, state)
	if err != nil || res.Outcome != outcomeOK {
		return err
	}

	samples := extractUsageSamples(res.Envelope.Data, "model")
	if len(samples) == 0 {
		snap.Raw["model_usage_api"] = "empty"
		return nil
	}
	projectModelUsageSamples(samples, snap)
	state.hasUsageData = true
	snap.Raw["model_usage_api"] = "ok"
	return nil
}

func (p *Provider) fetchToolUsage(ctx context.Context, monitorBase, apiKey string, snap *core.UsageSnapshot, state *providerState) error {
	res, err := p.evaluateMonitorEndpoint(ctx, monitorBase, apiKey, toolUsagePath, true,
		"tool_usage", "tool_usage_api", snap, state)
	if err != nil || res.Outcome != outcomeOK {
		return err
	}

	samples := extractUsageSamples(res.Envelope.Data, "tool")
	if len(samples) == 0 {
		snap.Raw["tool_usage_api"] = "empty"
		return nil
	}
	projectToolUsageSamples(samples, snap)
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
