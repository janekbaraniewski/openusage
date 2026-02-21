package zen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

const (
	defaultBaseURL        = "https://opencode.ai/zen/v1"
	defaultFreeProbeModel = "glm-5-free"
	defaultPaidProbeModel = "gpt-5.1-codex-mini"
	defaultProbeInput     = "openusage probe"
	defaultAuthHeader     = "Authorization"
	defaultKeyHeader      = "x-api-key"
	docsURL               = "https://opencode.ai/docs/zen/"
	docsPricingURL        = "https://opencode.ai/docs/zen/#pricing"
	docsUpdatedDate       = "2026-02-21"
	freeProbeTTL          = 5 * time.Minute
	billingProbeTTL       = 1 * time.Hour
	maxRawErrorLength     = 320
	maxRawModelPreview    = 10
	modelCountWindow      = "catalog"
	pricingWindow         = "pricing"
	probeWindow           = "last-probe"
)

var (
	billingURLRe  = regexp.MustCompile(`https://opencode\.ai/workspace/(wrk_[A-Za-z0-9]+)/billing`)
	workspaceIDRe = regexp.MustCompile(`wrk_[A-Za-z0-9]+`)
)

type Provider struct {
	httpClient *http.Client
	now        func() time.Time
}

func New() *Provider {
	return &Provider{httpClient: http.DefaultClient, now: time.Now}
}

func (p *Provider) ID() string { return "zen" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenCode Zen",
		Capabilities: []string{"model_catalog", "pricing_catalog", "usage_probe", "billing_inference", "subscription_inference"},
		DocURL:       docsURL,
	}
}

type modelListResponse struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type errorEnvelope struct {
	Type  string    `json:"type"`
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type chatProbeResponse struct {
	ID      string `json:"id"`
	Request string `json:"request_id"`
	Model   string `json:"model"`
	Cost    any    `json:"cost"`
	Type    string `json:"type"`
	Usage   struct {
		PromptTokens float64 `json:"prompt_tokens"`
		Completion   float64 `json:"completion_tokens"`
		Total        float64 `json:"total_tokens"`
		Details      struct {
			Cached float64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error errorBody `json:"error"`
}

type responsesProbeResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Cost  any    `json:"cost"`
	Type  string `json:"type"`
	Usage struct {
		InputTokens  float64 `json:"input_tokens"`
		OutputTokens float64 `json:"output_tokens"`
		TotalTokens  float64 `json:"total_tokens"`
		PromptTokens float64 `json:"prompt_tokens"`
	} `json:"usage"`
	Error errorBody `json:"error"`
}

type probeOutcome struct {
	StatusCode       int
	Endpoint         string
	Model            string
	RequestID        string
	PromptTokens     float64
	CompletionTokens float64
	TotalTokens      float64
	CachedTokens     float64
	CostUSD          float64
	ErrorType        string
	ErrorMessage     string
}

type cachedProbe struct {
	Expires time.Time
	Value   probeOutcome
}

var (
	probeCacheMu sync.Mutex
	probeCache   = make(map[string]cachedProbe)
)

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  p.now(),
			Status:     core.StatusAuth,
			Message:    fmt.Sprintf("env var %s not set", acct.APIKeyEnv),
		}, nil
	}

	baseURL := strings.TrimRight(acct.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	snap := core.QuotaSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   p.now(),
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}
	setStaticRaw(&snap)
	snap.Raw["api_base_url"] = baseURL

	models, err := p.fetchModels(ctx, baseURL, apiKey, &snap)
	if err != nil {
		return core.QuotaSnapshot{}, err
	}
	if snap.Status == core.StatusAuth {
		return snap, nil
	}

	loadPricingIndex()
	applyCatalogStats(&snap, models)

	freeProbeModel := defaultFreeProbeModel
	if v, ok := acct.ExtraData["free_probe_model"]; ok && strings.TrimSpace(v) != "" {
		freeProbeModel = strings.TrimSpace(v)
	}
	if acct.ProbeModel != "" {
		freeProbeModel = acct.ProbeModel
	}

	freeProbeKey := probeCacheKey("free", acct.ID, apiKey, baseURL, freeProbeModel)
	freeProbe, freeErr := p.getOrRunProbe(ctx, freeProbeKey, freeProbeTTL, func(ctx context.Context) (probeOutcome, error) {
		return p.runChatProbe(ctx, baseURL, apiKey, freeProbeModel)
	})
	if freeErr != nil {
		snap.Raw["free_probe_error"] = truncateRaw(freeErr.Error())
	} else {
		applyProbeOutcome(&snap, "free", freeProbe)
	}

	if row, ok := pricingByModelID[freeProbeModel]; ok {
		applyProbePricingMetrics(&snap, row, "free")
	}

	skipBillingProbe := strings.EqualFold(acct.ExtraData["skip_billing_probe"], "true")
	if !skipBillingProbe {
		paidProbeModel := defaultPaidProbeModel
		if v, ok := acct.ExtraData["paid_probe_model"]; ok && strings.TrimSpace(v) != "" {
			paidProbeModel = strings.TrimSpace(v)
		}
		billingProbeKey := probeCacheKey("billing", acct.ID, apiKey, baseURL, paidProbeModel)
		billingProbe, billingErr := p.getOrRunProbe(ctx, billingProbeKey, billingProbeTTL, func(ctx context.Context) (probeOutcome, error) {
			return p.runResponsesProbe(ctx, baseURL, apiKey, paidProbeModel)
		})
		if billingErr != nil {
			snap.Raw["billing_probe_error"] = truncateRaw(billingErr.Error())
		} else {
			applyProbeOutcome(&snap, "billing", billingProbe)
			applyBillingState(&snap, billingProbe)
			if row, ok := pricingByModelID[paidProbeModel]; ok {
				applyProbePricingMetrics(&snap, row, "billing")
			}
		}
	} else {
		snap.Raw["billing_probe_skipped"] = "true"
	}

	setFinalStatusAndMessage(&snap)
	return snap, nil
}

func setStaticRaw(snap *core.QuotaSnapshot) {
	snap.Raw["provider_docs"] = docsURL
	snap.Raw["pricing_docs"] = docsPricingURL
	snap.Raw["pricing_last_verified"] = docsUpdatedDate
	snap.Raw["billing_model"] = "prepaid_payg"
	snap.Raw["billing_fee_policy"] = "4.4% + $0.30 on card top-ups"
	snap.Raw["monthly_limits_scope"] = "workspace_member_and_key"
	snap.Raw["team_billing_policy"] = "charges_applied_to_workspace_owner"
	snap.Raw["team_model_access"] = "role_based_admin_member"
	snap.Raw["subscription_mutability"] = "billing_and_limits_can_change_over_time"
	snap.Raw["auto_reload_supported"] = "true"
	snap.Raw["monthly_limits_supported"] = "true"
	snap.Raw["team_roles_supported"] = "true"
	snap.Raw["byok_supported"] = "true"
}

func (p *Provider) fetchModels(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) ([]string, error) {
	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("zen: creating models request: %w", err)
	}
	setAuthHeaders(req, apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zen: models request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("zen: reading models response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d - check API key", resp.StatusCode)
		parseAndStoreError("models", body, snap)
		return nil, nil
	case http.StatusOK:
		// continue
	default:
		parseAndStoreError("models", body, snap)
		return nil, fmt.Errorf("zen: models endpoint returned HTTP %d", resp.StatusCode)
	}

	var parsed modelListResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("zen: parsing models response: %w", err)
	}

	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)

	snap.Raw["models_count"] = strconv.Itoa(len(ids))
	if len(ids) > 0 {
		preview := ids
		if len(preview) > maxRawModelPreview {
			preview = preview[:maxRawModelPreview]
		}
		snap.Raw["models_preview"] = strings.Join(preview, ", ")
	}

	return ids, nil
}

func parseAndStoreError(prefix string, body []byte, snap *core.QuotaSnapshot) {
	typ, msg := parseErrorEnvelope(body)
	if typ != "" {
		snap.Raw[prefix+"_error_type"] = typ
	}
	if msg != "" {
		snap.Raw[prefix+"_error"] = truncateRaw(msg)
	}
}

func parseErrorEnvelope(body []byte) (string, string) {
	var parsed errorEnvelope
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", strings.TrimSpace(string(body))
	}
	errType := strings.TrimSpace(parsed.Error.Type)
	if errType == "" {
		errType = strings.TrimSpace(parsed.Type)
	}
	errMsg := strings.TrimSpace(parsed.Error.Message)
	if errMsg == "" {
		errMsg = strings.TrimSpace(string(body))
	}
	return errType, errMsg
}

func (p *Provider) runChatProbe(ctx context.Context, baseURL, apiKey, model string) (probeOutcome, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": defaultProbeInput,
		}},
		"max_tokens": 8,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: marshal chat probe payload: %w", err)
	}

	url := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: creating chat probe request: %w", err)
	}
	setAuthHeaders(req, apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: chat probe request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: reading chat probe response: %w", err)
	}

	out := probeOutcome{StatusCode: resp.StatusCode, Endpoint: "/chat/completions", Model: model}
	if resp.StatusCode != http.StatusOK {
		out.ErrorType, out.ErrorMessage = parseErrorEnvelope(raw)
		return out, nil
	}

	var parsed chatProbeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return probeOutcome{}, fmt.Errorf("zen: parsing chat probe response: %w", err)
	}

	out.RequestID = parsed.Request
	if out.RequestID == "" {
		out.RequestID = parsed.ID
	}
	if parsed.Model != "" {
		out.Model = parsed.Model
	}
	out.PromptTokens = parsed.Usage.PromptTokens
	out.CompletionTokens = parsed.Usage.Completion
	out.TotalTokens = parsed.Usage.Total
	out.CachedTokens = parsed.Usage.Details.Cached
	out.CostUSD = parseCostField(parsed.Cost)
	return out, nil
}

func (p *Provider) runResponsesProbe(ctx context.Context, baseURL, apiKey, model string) (probeOutcome, error) {
	payload := map[string]any{
		"model":             model,
		"input":             defaultProbeInput,
		"max_output_tokens": 1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: marshal responses probe payload: %w", err)
	}

	url := baseURL + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: creating responses probe request: %w", err)
	}
	setAuthHeaders(req, apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: responses probe request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return probeOutcome{}, fmt.Errorf("zen: reading responses probe response: %w", err)
	}

	out := probeOutcome{StatusCode: resp.StatusCode, Endpoint: "/responses", Model: model}
	if resp.StatusCode != http.StatusOK {
		out.ErrorType, out.ErrorMessage = parseErrorEnvelope(raw)
		return out, nil
	}

	var parsed responsesProbeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return probeOutcome{}, fmt.Errorf("zen: parsing responses probe response: %w", err)
	}

	out.RequestID = parsed.ID
	if parsed.Model != "" {
		out.Model = parsed.Model
	}
	if parsed.Usage.PromptTokens > 0 {
		out.PromptTokens = parsed.Usage.PromptTokens
	} else {
		out.PromptTokens = parsed.Usage.InputTokens
	}
	out.CompletionTokens = parsed.Usage.OutputTokens
	out.TotalTokens = parsed.Usage.TotalTokens
	if out.TotalTokens == 0 {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	out.CostUSD = parseCostField(parsed.Cost)
	return out, nil
}

func parseCostField(v any) float64 {
	switch t := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func (p *Provider) getOrRunProbe(ctx context.Context, key string, ttl time.Duration, fn func(context.Context) (probeOutcome, error)) (probeOutcome, error) {
	now := p.now()
	probeCacheMu.Lock()
	if cached, ok := probeCache[key]; ok && now.Before(cached.Expires) {
		probeCacheMu.Unlock()
		return cached.Value, nil
	}
	probeCacheMu.Unlock()

	out, err := fn(ctx)
	if err != nil {
		return probeOutcome{}, err
	}

	probeCacheMu.Lock()
	probeCache[key] = cachedProbe{Expires: now.Add(ttl), Value: out}
	probeCacheMu.Unlock()
	return out, nil
}

func probeCacheKey(kind, accountID, apiKey, baseURL, model string) string {
	h := sha256.Sum256([]byte(apiKey))
	short := hex.EncodeToString(h[:6])
	return strings.Join([]string{kind, accountID, baseURL, model, short}, "|")
}

func applyProbeOutcome(snap *core.QuotaSnapshot, probeType string, out probeOutcome) {
	prefix := probeType + "_probe"
	snap.Raw[prefix+"_status"] = strconv.Itoa(out.StatusCode)
	snap.Raw[prefix+"_endpoint"] = out.Endpoint
	snap.Raw[prefix+"_model"] = out.Model
	if out.RequestID != "" {
		snap.Raw[prefix+"_request_id"] = out.RequestID
	}
	if out.ErrorType != "" {
		snap.Raw[prefix+"_error_type"] = out.ErrorType
	}
	if out.ErrorMessage != "" {
		snap.Raw[prefix+"_error"] = truncateRaw(out.ErrorMessage)
	}
	if out.StatusCode != http.StatusOK {
		applyProbeErrorStatus(snap, out, probeType)
		return
	}

	setUsedMetric(snap, prefix+"_input_tokens", out.PromptTokens, "tokens", probeWindow)
	setUsedMetric(snap, prefix+"_output_tokens", out.CompletionTokens, "tokens", probeWindow)
	setUsedMetric(snap, prefix+"_total_tokens", out.TotalTokens, "tokens", probeWindow)
	setUsedMetric(snap, prefix+"_cached_tokens", out.CachedTokens, "tokens", probeWindow)
	setUsedMetric(snap, prefix+"_cost_usd", out.CostUSD, "USD", probeWindow)

	today := snap.Timestamp.UTC().Format("2006-01-02")
	snap.DailySeries[prefix+"_tokens"] = []core.TimePoint{{Date: today, Value: out.TotalTokens}}
	snap.DailySeries[prefix+"_cost"] = []core.TimePoint{{Date: today, Value: out.CostUSD}}
}

func setAuthHeaders(req *http.Request, apiKey string) {
	req.Header.Set(defaultAuthHeader, "Bearer "+apiKey)
	req.Header.Set(defaultKeyHeader, apiKey)
}

func applyProbeErrorStatus(snap *core.QuotaSnapshot, out probeOutcome, probeType string) {
	errType := strings.ToLower(strings.TrimSpace(out.ErrorType))
	errMsg := strings.ToLower(strings.TrimSpace(out.ErrorMessage))

	if out.StatusCode == http.StatusTooManyRequests {
		snap.Status = core.StatusLimited
		if snap.Message == "" {
			snap.Message = fmt.Sprintf("%s probe rate limited", probeType)
		}
		return
	}

	if out.StatusCode == http.StatusUnauthorized || out.StatusCode == http.StatusForbidden {
		if strings.Contains(errType, "credits") || strings.Contains(errMsg, "no payment method") || strings.Contains(errMsg, "no credits") {
			return
		}
		if snap.Status == "" || snap.Status == core.StatusOK {
			snap.Status = core.StatusAuth
			if snap.Message == "" {
				snap.Message = "auth required - check Zen API key"
			}
		}
		return
	}

	if out.StatusCode >= 500 && (snap.Status == "" || snap.Status == core.StatusOK) {
		snap.Status = core.StatusError
		if snap.Message == "" {
			snap.Message = fmt.Sprintf("%s probe API error (HTTP %d)", probeType, out.StatusCode)
		}
	}
}

func applyBillingState(snap *core.QuotaSnapshot, out probeOutcome) {
	if out.StatusCode == http.StatusOK {
		snap.Raw["subscription_status"] = "active"
		setUsedMetric(snap, "subscription_active", 1, "flag", "account")
		if snap.Message == "" {
			snap.Message = "Billing-enabled account"
		}
		return
	}

	errType := strings.ToLower(out.ErrorType)
	errMsg := strings.ToLower(out.ErrorMessage)
	if out.StatusCode == http.StatusUnauthorized || out.StatusCode == http.StatusForbidden {
		if strings.Contains(errType, "auth") || strings.Contains(errMsg, "missing api key") || strings.Contains(errMsg, "invalid") {
			snap.Status = core.StatusAuth
			snap.Message = "auth required - check Zen API key"
			snap.Raw["subscription_status"] = "unknown"
			return
		}
	}

	if strings.Contains(errType, "credits") || strings.Contains(errMsg, "no payment method") || strings.Contains(errMsg, "no credits") {
		snap.Status = core.StatusLimited
		snap.Raw["payment_required"] = "true"
		setUsedMetric(snap, "subscription_active", 0, "flag", "account")
		if strings.Contains(errMsg, "no payment method") {
			snap.Raw["billing_status"] = "payment_method_missing"
			snap.Raw["subscription_status"] = "inactive_no_payment_method"
			setUsedMetric(snap, "billing_payment_method_missing", 1, "flag", "account")
			if snap.Message == "" {
				snap.Message = "Connected, but billing is not enabled"
			}
		} else if strings.Contains(errMsg, "no credits") {
			snap.Raw["billing_status"] = "out_of_credits"
			snap.Raw["subscription_status"] = "active_no_credits"
			setUsedMetric(snap, "billing_out_of_credits", 1, "flag", "account")
			if snap.Message == "" {
				snap.Message = "Connected, but account has no credits"
			}
		} else {
			snap.Raw["billing_status"] = "credits_error"
			snap.Raw["subscription_status"] = "limited"
		}
		if billingURL, workspaceID := extractBillingInfo(out.ErrorMessage); billingURL != "" {
			snap.Raw["billing_url"] = billingURL
			if workspaceID != "" {
				snap.Raw["workspace_id"] = workspaceID
			}
		}
		return
	}

	if snap.Raw["subscription_status"] == "" {
		snap.Raw["subscription_status"] = "unknown"
	}
}

func extractBillingInfo(msg string) (billingURL, workspaceID string) {
	m := billingURLRe.FindStringSubmatch(msg)
	if len(m) == 2 {
		return m[0], m[1]
	}
	url := billingURLRe.FindString(msg)
	if url == "" {
		return "", ""
	}
	id := workspaceIDRe.FindString(url)
	return url, id
}

func setFinalStatusAndMessage(snap *core.QuotaSnapshot) {
	if snap.Status == "" {
		snap.Status = core.StatusOK
	}
	if snap.Message != "" {
		return
	}
	if snap.Status == core.StatusLimited {
		switch snap.Raw["billing_status"] {
		case "payment_method_missing":
			snap.Message = "Paid models unavailable - add payment method"
		case "out_of_credits":
			snap.Message = "Paid models unavailable - no credits"
		default:
			snap.Message = "Limited"
		}
		return
	}
	if snap.Status == core.StatusAuth {
		snap.Message = "auth required - check Zen API key"
		return
	}

	models := snap.Raw["models_count"]
	if models == "" {
		snap.Message = "Connected"
		return
	}
	if m, ok := snap.Metrics["free_probe_total_tokens"]; ok && m.Used != nil {
		cost := 0.0
		if c, ok := snap.Metrics["free_probe_cost_usd"]; ok && c.Used != nil {
			cost = *c.Used
		}
		snap.Message = fmt.Sprintf("Catalog %s models · last free probe %.0f tok ($%.4f)", models, *m.Used, cost)
		return
	}
	snap.Message = fmt.Sprintf("Catalog %s models", models)
}

func setUsedMetric(snap *core.QuotaSnapshot, key string, used float64, unit, window string) {
	v := used
	snap.Metrics[key] = core.Metric{Used: &v, Unit: unit, Window: window}
}

func setLimitMetric(snap *core.QuotaSnapshot, key, unit, window string, used, limit float64) {
	u := used
	l := limit
	r := l - u
	if r < 0 {
		r = 0
	}
	snap.Metrics[key] = core.Metric{Used: &u, Limit: &l, Remaining: &r, Unit: unit, Window: window}
}

func truncateRaw(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxRawErrorLength {
		return s
	}
	return s[:maxRawErrorLength] + "..."
}

type pricingRow struct {
	Label       string
	ModelID     string
	Input       float64
	Output      float64
	CachedRead  *float64
	CachedWrite *float64
	Free        bool
}

var (
	pricingOnce      sync.Once
	pricingByModelID map[string]pricingRow
	freeModelIDs     map[string]bool
)

func loadPricingIndex() {
	pricingOnce.Do(func() {
		pricingByModelID = make(map[string]pricingRow)
		freeModelIDs = make(map[string]bool)
		for _, row := range pricingRows {
			if row.ModelID == "" {
				continue
			}
			if existing, ok := pricingByModelID[row.ModelID]; !ok || row.Input < existing.Input {
				pricingByModelID[row.ModelID] = row
			}
			if row.Free {
				freeModelIDs[row.ModelID] = true
			}
		}
	})
}

func applyCatalogStats(snap *core.QuotaSnapshot, models []string) {
	setUsedMetric(snap, "models_total", float64(len(models)), "models", modelCountWindow)

	freeCount := 0
	paidCount := 0
	unknownCount := 0
	endpointCounts := map[string]int{
		"chat":      0,
		"responses": 0,
		"messages":  0,
		"google":    0,
		"unknown":   0,
	}

	for _, model := range models {
		if freeModelIDs[model] {
			freeCount++
		} else if _, ok := pricingByModelID[model]; ok {
			paidCount++
		} else {
			unknownCount++
		}

		endpoint, ok := modelEndpointByID[model]
		if !ok {
			endpointCounts["unknown"]++
			continue
		}
		endpointCounts[endpoint]++
	}

	setUsedMetric(snap, "models_free", float64(freeCount), "models", modelCountWindow)
	setUsedMetric(snap, "models_paid", float64(paidCount), "models", modelCountWindow)
	setUsedMetric(snap, "models_unknown", float64(unknownCount), "models", modelCountWindow)

	setUsedMetric(snap, "endpoint_chat_models", float64(endpointCounts["chat"]), "models", modelCountWindow)
	setUsedMetric(snap, "endpoint_responses_models", float64(endpointCounts["responses"]), "models", modelCountWindow)
	setUsedMetric(snap, "endpoint_messages_models", float64(endpointCounts["messages"]), "models", modelCountWindow)
	setUsedMetric(snap, "endpoint_google_models", float64(endpointCounts["google"]), "models", modelCountWindow)

	snap.Raw["models_free_count"] = strconv.Itoa(freeCount)
	snap.Raw["models_paid_count"] = strconv.Itoa(paidCount)
	snap.Raw["models_unknown_count"] = strconv.Itoa(unknownCount)
	if endpointCounts["unknown"] > 0 {
		snap.Raw["endpoint_unknown_models"] = strconv.Itoa(endpointCounts["unknown"])
	}

	inputMinPaid, inputMax, outputMinPaid, outputMax := pricingRanges()
	setUsedMetric(snap, "pricing_input_min_paid_per_1m", inputMinPaid, "USD/1Mtok", pricingWindow)
	setUsedMetric(snap, "pricing_input_max_per_1m", inputMax, "USD/1Mtok", pricingWindow)
	setUsedMetric(snap, "pricing_output_min_paid_per_1m", outputMinPaid, "USD/1Mtok", pricingWindow)
	setUsedMetric(snap, "pricing_output_max_per_1m", outputMax, "USD/1Mtok", pricingWindow)
	setUsedMetric(snap, "pricing_models", float64(len(pricingRows)), "rows", pricingWindow)

	snap.Raw["pricing_rows"] = strconv.Itoa(len(pricingRows))
}

func applyProbePricingMetrics(snap *core.QuotaSnapshot, row pricingRow, prefix string) {
	setUsedMetric(snap, prefix+"_probe_price_input_per_1m", row.Input, "USD/1Mtok", pricingWindow)
	setUsedMetric(snap, prefix+"_probe_price_output_per_1m", row.Output, "USD/1Mtok", pricingWindow)
	if row.CachedRead != nil {
		setUsedMetric(snap, prefix+"_probe_price_cached_read_per_1m", *row.CachedRead, "USD/1Mtok", pricingWindow)
	}
	if row.CachedWrite != nil {
		setUsedMetric(snap, prefix+"_probe_price_cached_write_per_1m", *row.CachedWrite, "USD/1Mtok", pricingWindow)
	}
}

func pricingRanges() (inputMinPaid, inputMax, outputMinPaid, outputMax float64) {
	inputMinPaid = -1
	outputMinPaid = -1
	for _, row := range pricingRows {
		if row.Input > inputMax {
			inputMax = row.Input
		}
		if row.Output > outputMax {
			outputMax = row.Output
		}
		if row.Free {
			continue
		}
		if inputMinPaid < 0 || row.Input < inputMinPaid {
			inputMinPaid = row.Input
		}
		if outputMinPaid < 0 || row.Output < outputMinPaid {
			outputMinPaid = row.Output
		}
	}
	if inputMinPaid < 0 {
		inputMinPaid = 0
	}
	if outputMinPaid < 0 {
		outputMinPaid = 0
	}
	return inputMinPaid, inputMax, outputMinPaid, outputMax
}

func floatPtr(v float64) *float64 {
	return &v
}

var pricingRows = []pricingRow{
	{Label: "Big Pickle", ModelID: "big-pickle", Input: 0, Output: 0, CachedRead: floatPtr(0), Free: true},
	{Label: "MiniMax M2.5 Free", ModelID: "minimax-m2.5-free", Input: 0, Output: 0, CachedRead: floatPtr(0), Free: true},
	{Label: "MiniMax M2.5", ModelID: "minimax-m2.5", Input: 0.30, Output: 1.20, CachedRead: floatPtr(0.06)},
	{Label: "MiniMax M2.1", ModelID: "minimax-m2.1", Input: 0.30, Output: 1.20, CachedRead: floatPtr(0.10)},
	{Label: "GLM 5 Free", ModelID: "glm-5-free", Input: 0, Output: 0, CachedRead: floatPtr(0), Free: true},
	{Label: "GLM 5", ModelID: "glm-5", Input: 1.00, Output: 3.20, CachedRead: floatPtr(0.20)},
	{Label: "GLM 4.7", ModelID: "glm-4.7", Input: 0.60, Output: 2.20, CachedRead: floatPtr(0.10)},
	{Label: "GLM 4.6", ModelID: "glm-4.6", Input: 0.60, Output: 2.20, CachedRead: floatPtr(0.10)},
	{Label: "Kimi K2.5 Free", ModelID: "kimi-k2.5-free", Input: 0, Output: 0, CachedRead: floatPtr(0), Free: true},
	{Label: "Kimi K2.5", ModelID: "kimi-k2.5", Input: 0.60, Output: 3.00, CachedRead: floatPtr(0.08)},
	{Label: "Kimi K2 Thinking", ModelID: "kimi-k2-thinking", Input: 0.40, Output: 2.50},
	{Label: "Kimi K2", ModelID: "kimi-k2", Input: 0.40, Output: 2.50},
	{Label: "Qwen3 Coder 480B", ModelID: "qwen3-coder", Input: 0.45, Output: 1.50},
	{Label: "Claude Opus 4.6 (<= 200K)", ModelID: "claude-opus-4-6", Input: 5.00, Output: 25.00, CachedRead: floatPtr(0.50), CachedWrite: floatPtr(6.25)},
	{Label: "Claude Opus 4.6 (> 200K)", ModelID: "claude-opus-4-6", Input: 10.00, Output: 37.50, CachedRead: floatPtr(1.00), CachedWrite: floatPtr(12.50)},
	{Label: "Claude Opus 4.5", ModelID: "claude-opus-4-5", Input: 5.00, Output: 25.00, CachedRead: floatPtr(0.50), CachedWrite: floatPtr(6.25)},
	{Label: "Claude Opus 4.1", ModelID: "claude-opus-4-1", Input: 15.00, Output: 75.00, CachedRead: floatPtr(1.50), CachedWrite: floatPtr(18.75)},
	{Label: "Claude Sonnet 4.6 (<= 200K)", ModelID: "claude-sonnet-4-6", Input: 3.00, Output: 15.00, CachedRead: floatPtr(0.30), CachedWrite: floatPtr(3.75)},
	{Label: "Claude Sonnet 4.6 (> 200K)", ModelID: "claude-sonnet-4-6", Input: 6.00, Output: 22.50, CachedRead: floatPtr(0.60), CachedWrite: floatPtr(7.50)},
	{Label: "Claude Sonnet 4.5 (<= 200K)", ModelID: "claude-sonnet-4-5", Input: 3.00, Output: 15.00, CachedRead: floatPtr(0.30), CachedWrite: floatPtr(3.75)},
	{Label: "Claude Sonnet 4.5 (> 200K)", ModelID: "claude-sonnet-4-5", Input: 6.00, Output: 22.50, CachedRead: floatPtr(0.60), CachedWrite: floatPtr(7.50)},
	{Label: "Claude Sonnet 4 (<= 200K)", ModelID: "claude-sonnet-4", Input: 3.00, Output: 15.00, CachedRead: floatPtr(0.30), CachedWrite: floatPtr(3.75)},
	{Label: "Claude Sonnet 4 (> 200K)", ModelID: "claude-sonnet-4", Input: 6.00, Output: 22.50, CachedRead: floatPtr(0.60), CachedWrite: floatPtr(7.50)},
	{Label: "Claude Haiku 4.5", ModelID: "claude-haiku-4-5", Input: 1.00, Output: 5.00, CachedRead: floatPtr(0.10), CachedWrite: floatPtr(1.25)},
	{Label: "Claude Haiku 3.5", ModelID: "claude-3-5-haiku", Input: 0.80, Output: 4.00, CachedRead: floatPtr(0.08), CachedWrite: floatPtr(1.00)},
	{Label: "Gemini 3.1 Pro (<= 200K)", ModelID: "gemini-3.1-pro", Input: 2.00, Output: 12.00, CachedRead: floatPtr(0.20)},
	{Label: "Gemini 3.1 Pro (> 200K)", ModelID: "gemini-3.1-pro", Input: 4.00, Output: 18.00, CachedRead: floatPtr(0.40)},
	{Label: "Gemini 3 Pro (<= 200K)", ModelID: "gemini-3-pro", Input: 2.00, Output: 12.00, CachedRead: floatPtr(0.20)},
	{Label: "Gemini 3 Pro (> 200K)", ModelID: "gemini-3-pro", Input: 4.00, Output: 18.00, CachedRead: floatPtr(0.40)},
	{Label: "Gemini 3 Flash", ModelID: "gemini-3-flash", Input: 0.50, Output: 3.00, CachedRead: floatPtr(0.05)},
	{Label: "GPT 5.2", ModelID: "gpt-5.2", Input: 1.75, Output: 14.00, CachedRead: floatPtr(0.175)},
	{Label: "GPT 5.2 Codex", ModelID: "gpt-5.2-codex", Input: 1.75, Output: 14.00, CachedRead: floatPtr(0.175)},
	{Label: "GPT 5.1", ModelID: "gpt-5.1", Input: 1.07, Output: 8.50, CachedRead: floatPtr(0.107)},
	{Label: "GPT 5.1 Codex", ModelID: "gpt-5.1-codex", Input: 1.07, Output: 8.50, CachedRead: floatPtr(0.107)},
	{Label: "GPT 5.1 Codex Max", ModelID: "gpt-5.1-codex-max", Input: 1.25, Output: 10.00, CachedRead: floatPtr(0.125)},
	{Label: "GPT 5.1 Codex Mini", ModelID: "gpt-5.1-codex-mini", Input: 0.25, Output: 2.00, CachedRead: floatPtr(0.025)},
	{Label: "GPT 5", ModelID: "gpt-5", Input: 1.07, Output: 8.50, CachedRead: floatPtr(0.107)},
	{Label: "GPT 5 Codex", ModelID: "gpt-5-codex", Input: 1.07, Output: 8.50, CachedRead: floatPtr(0.107)},
	{Label: "GPT 5 Nano", ModelID: "gpt-5-nano", Input: 0, Output: 0, CachedRead: floatPtr(0), Free: true},
}

var modelEndpointByID = map[string]string{
	"gpt-5.2":            "responses",
	"gpt-5.2-codex":      "responses",
	"gpt-5.1":            "responses",
	"gpt-5.1-codex":      "responses",
	"gpt-5.1-codex-max":  "responses",
	"gpt-5.1-codex-mini": "responses",
	"gpt-5":              "responses",
	"gpt-5-codex":        "responses",
	"gpt-5-nano":         "responses",

	"claude-opus-4-6":   "messages",
	"claude-opus-4-5":   "messages",
	"claude-opus-4-1":   "messages",
	"claude-sonnet-4-6": "messages",
	"claude-sonnet-4-5": "messages",
	"claude-sonnet-4":   "messages",
	"claude-haiku-4-5":  "messages",
	"claude-3-5-haiku":  "messages",

	"gemini-3.1-pro": "google",
	"gemini-3-pro":   "google",
	"gemini-3-flash": "google",

	"minimax-m2.5":      "chat",
	"minimax-m2.5-free": "chat",
	"minimax-m2.1":      "chat",
	"glm-5":             "chat",
	"glm-5-free":        "chat",
	"glm-4.7":           "chat",
	"glm-4.6":           "chat",
	"kimi-k2.5":         "chat",
	"kimi-k2.5-free":    "chat",
	"kimi-k2-thinking":  "chat",
	"kimi-k2":           "chat",
	"qwen3-coder":       "chat",
	"big-pickle":        "chat",
}

func resetProbeCache() {
	probeCacheMu.Lock()
	defer probeCacheMu.Unlock()
	probeCache = make(map[string]cachedProbe)
}
