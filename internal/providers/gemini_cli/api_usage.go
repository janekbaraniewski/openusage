package gemini_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) fetchUsageFromAPI(ctx context.Context, snap *core.UsageSnapshot, creds oauthCreds, acct core.AccountConfig) error {
	client := p.Client()
	cacheKey := apiCacheKey(acct.ID, creds.RefreshToken)

	accessToken, tokenSource, err := p.resolveAccessToken(ctx, creds, cacheKey, client, false)
	if err != nil {
		snap.Status = core.StatusAuth
		snap.Message = "OAuth token refresh failed — run `gemini` to re-authenticate"
		return fmt.Errorf("token refresh: %w", err)
	}
	snap.Raw["oauth_status"] = tokenSource.status()

	projectID := ""
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		projectID = v
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		projectID = v
	}
	if projectID == "" {
		projectID = acct.Hint("project_id", "")
	}
	loadKey := cacheKey + "\x00" + projectID

	loadResp, cachedLoad := p.apiCache.loadCodeAssist(loadKey, p.now())
	if !cachedLoad {
		loadResp, err = loadCodeAssistDetailsWithEndpoint(ctx, accessToken, projectID, p.codeAssistBaseURL(), client)
		if isCodeAssistUnauthorized(err) && tokenSource != tokenSourceRefreshed {
			// The reused token was revoked or rotated early: refresh once and retry.
			accessToken, tokenSource, err = p.resolveAccessToken(ctx, creds, cacheKey, client, true)
			if err != nil {
				snap.Status = core.StatusAuth
				snap.Message = "OAuth token refresh failed — run `gemini` to re-authenticate"
				return fmt.Errorf("token refresh: %w", err)
			}
			snap.Raw["oauth_status"] = tokenSource.status()
			loadResp, err = loadCodeAssistDetailsWithEndpoint(ctx, accessToken, projectID, p.codeAssistBaseURL(), client)
		}
		if err != nil {
			return fmt.Errorf("loadCodeAssist: %w", err)
		}
		p.apiCache.storeLoadCodeAssist(loadKey, loadResp, p.now())
	}
	if loadResp != nil {
		applyLoadCodeAssistMetadata(snap, loadResp)
		if projectID == "" {
			projectID = loadResp.CloudAICompanionProject
		}
	}

	if projectID == "" {
		return fmt.Errorf("could not determine project ID")
	}
	snap.Raw["project_id"] = projectID

	quota, method, err := retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, p.codeAssistBaseURL(), client)
	if isCodeAssistUnauthorized(err) && tokenSource != tokenSourceRefreshed {
		accessToken, tokenSource, err = p.resolveAccessToken(ctx, creds, cacheKey, client, true)
		if err != nil {
			snap.Status = core.StatusAuth
			snap.Message = "OAuth token refresh failed — run `gemini` to re-authenticate"
			return fmt.Errorf("token refresh: %w", err)
		}
		quota, method, err = retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, p.codeAssistBaseURL(), client)
	}
	snap.Raw["oauth_status"] = tokenSource.status()
	if err != nil {
		return fmt.Errorf("retrieveUserQuota: %w", err)
	}

	if len(quota.Buckets) == 0 {
		snap.Raw["quota_api"] = fmt.Sprintf("ok (0 buckets, %s)", method)
		snap.Raw["quota_api_method"] = method
		return nil
	}

	snap.Raw["quota_api"] = fmt.Sprintf("ok (%d buckets, %s)", len(quota.Buckets), method)
	snap.Raw["quota_api_method"] = method
	snap.Raw["quota_bucket_count"] = fmt.Sprintf("%d", len(quota.Buckets))

	result := applyQuotaBuckets(snap, quota.Buckets)
	applyQuotaStatus(snap, result.worstFraction)

	return nil
}

type accessTokenSource int

const (
	tokenSourceCredsFile accessTokenSource = iota
	tokenSourceCached
	tokenSourceRefreshed
)

func (s accessTokenSource) status() string {
	switch s {
	case tokenSourceCredsFile:
		return "valid"
	case tokenSourceCached:
		return "valid (cached)"
	default:
		return "valid (refreshed)"
	}
}

// resolveAccessToken returns an access token for the Code Assist API without
// hitting the OAuth endpoint when it can avoid it: a token this provider
// refreshed earlier (memory only) or the token in oauth_creds.json is reused
// until it is within accessTokenRefreshSkew of expiry, and only then is a new
// one requested. forceRefresh skips both reuse paths (used after a 401).
func (p *Provider) resolveAccessToken(ctx context.Context, creds oauthCreds, cacheKey string, client *http.Client, forceRefresh bool) (string, accessTokenSource, error) {
	now := p.now()
	if forceRefresh {
		p.apiCache.dropToken(cacheKey)
	} else {
		// A token we refreshed ourselves wins: it exists only because the
		// file token was stale or rejected, so prefer it while it is fresh.
		if token, ok := p.apiCache.token(cacheKey, now); ok {
			return token, tokenSourceCached, nil
		}
		if creds.AccessToken != "" && creds.ExpiryDate > 0 && tokenUsable(time.UnixMilli(creds.ExpiryDate), now) {
			return creds.AccessToken, tokenSourceCredsFile, nil
		}
	}

	resp, err := requestAccessToken(ctx, creds.RefreshToken, p.tokenURL(), client)
	if err != nil {
		return "", tokenSourceRefreshed, err
	}
	lifetime := time.Duration(resp.ExpiresIn) * time.Second
	if lifetime <= 0 {
		lifetime = defaultAccessTokenLifetime
	}
	p.apiCache.storeToken(cacheKey, resp.AccessToken, now.Add(lifetime))
	return resp.AccessToken, tokenSourceRefreshed, nil
}

func refreshAccessTokenWithEndpoint(ctx context.Context, refreshToken, endpoint string, client *http.Client) (string, error) {
	resp, err := requestAccessToken(ctx, refreshToken, endpoint, client)
	if err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}

func requestAccessToken(ctx context.Context, refreshToken, endpoint string, client *http.Client) (tokenRefreshResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	data := url.Values{
		"client_id":     {oauthClientID},
		"client_secret": {oauthClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return tokenRefreshResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return tokenRefreshResponse{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Only surface the OAuth error code, never the raw body, so nothing
		// token-shaped can end up in logs or snapshot Raw fields.
		return tokenRefreshResponse{}, fmt.Errorf("token refresh HTTP %d%s", resp.StatusCode, oauthErrorSuffix(body))
	}

	var tokenResp tokenRefreshResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return tokenRefreshResponse{}, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return tokenRefreshResponse{}, fmt.Errorf("empty access_token in refresh response")
	}

	return tokenResp, nil
}

func oauthErrorSuffix(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Error == "" {
		return ""
	}
	return ": " + truncate(payload.Error, 64)
}

func (p *Provider) tokenURL() string {
	if p.tokenEndpoint != "" {
		return p.tokenEndpoint
	}
	return tokenEndpoint
}

func (p *Provider) codeAssistBaseURL() string {
	if p.codeAssistEndpoint != "" {
		return p.codeAssistEndpoint
	}
	return codeAssistEndpoint
}

func (p *Provider) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// codeAssistHTTPError is returned for non-200 Code Assist responses so callers
// can react to auth failures.
type codeAssistHTTPError struct {
	method string
	status int
	body   string
}

func (e *codeAssistHTTPError) Error() string {
	return fmt.Sprintf("%s HTTP %d: %s", e.method, e.status, e.body)
}

func isCodeAssistUnauthorized(err error) bool {
	var httpErr *codeAssistHTTPError
	return errors.As(err, &httpErr) && httpErr.status == http.StatusUnauthorized
}

func loadCodeAssistDetailsWithEndpoint(ctx context.Context, accessToken, existingProjectID, baseURL string, client *http.Client) (*loadCodeAssistResponse, error) {
	reqBody := loadCodeAssistRequest{
		CloudAICompanionProject: existingProjectID,
		Metadata: clientMetadata{
			IDEType:    "IDE_UNSPECIFIED",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
			Project:    existingProjectID,
		},
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "loadCodeAssist", reqBody, baseURL, client)
	if err != nil {
		return nil, err
	}

	var resp loadCodeAssistResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	return &resp, nil
}

func retrieveUserQuotaWithEndpoint(ctx context.Context, accessToken, projectID, baseURL string, client *http.Client) (*retrieveUserQuotaResponse, string, error) {
	reqBody := retrieveUserQuotaRequest{
		Project: projectID,
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "retrieveUserQuota", reqBody, baseURL, client)
	if err != nil {
		return nil, "", err
	}

	var resp retrieveUserQuotaResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, "", fmt.Errorf("parse retrieveUserQuota response: %w", err)
	}

	return &resp, "retrieveUserQuota", nil
}

func codeAssistPostWithEndpoint(ctx context.Context, accessToken, method string, body interface{}, baseURL string, client *http.Client) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	apiURL := fmt.Sprintf("%s/%s:%s", baseURL, codeAssistAPIVersion, method)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, &codeAssistHTTPError{method: method, status: resp.StatusCode, body: truncate(string(respBody), 200)}
	}

	return respBody, nil
}

func formatWindow(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours >= 24 {
		days := hours / 24
		if days == 1 {
			return "~1 day"
		}
		return fmt.Sprintf("~%dd", days)
	}
	if hours > 0 && minutes > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}

func truncate(s string, maxLen int) string { return shared.Truncate(s, maxLen) }

type quotaAggregationResult struct {
	bucketCount   int
	modelCount    int
	worstFraction float64
}

type quotaAggregate struct {
	modelID           string
	tokenType         string
	remainingFraction float64
	resetAt           time.Time
	hasReset          bool
}

func applyLoadCodeAssistMetadata(snap *core.UsageSnapshot, resp *loadCodeAssistResponse) {
	if resp == nil {
		return
	}

	snap.Raw["gcp_managed"] = fmt.Sprintf("%t", resp.GCPManaged)
	if resp.UpgradeSubscriptionURI != "" {
		snap.Raw["upgrade_uri"] = resp.UpgradeSubscriptionURI
	}
	if resp.UpgradeSubscriptionType != "" {
		snap.Raw["upgrade_type"] = resp.UpgradeSubscriptionType
	}

	if resp.CurrentTier != nil {
		if resp.CurrentTier.ID != "" {
			snap.Raw["tier_id"] = resp.CurrentTier.ID
		}
		if resp.CurrentTier.Name != "" {
			snap.Raw["tier_name"] = resp.CurrentTier.Name
		}
		if resp.CurrentTier.Description != "" {
			snap.Raw["tier_description"] = truncate(strings.TrimSpace(resp.CurrentTier.Description), 200)
		}
		snap.Raw["tier_uses_gcp_tos"] = fmt.Sprintf("%t", resp.CurrentTier.UsesGCPTOS)
		snap.Raw["tier_user_project"] = fmt.Sprintf("%t", resp.CurrentTier.UserDefinedCloudAICompanionProject)
	}

	allowedTiers := float64(len(resp.AllowedTiers))
	ineligibleTiers := float64(len(resp.IneligibleTiers))
	snap.Metrics["allowed_tiers"] = core.Metric{Used: &allowedTiers, Unit: "tiers", Window: "current"}
	snap.Metrics["ineligible_tiers"] = core.Metric{Used: &ineligibleTiers, Unit: "tiers", Window: "current"}

	if len(resp.AllowedTiers) > 0 {
		names := make([]string, 0, len(resp.AllowedTiers))
		for _, tier := range resp.AllowedTiers {
			if tier.Name != "" {
				names = append(names, tier.Name)
			} else if tier.ID != "" {
				names = append(names, tier.ID)
			}
		}
		if len(names) > 0 {
			snap.Raw["allowed_tier_names"] = strings.Join(names, ", ")
		}
	}

	if len(resp.IneligibleTiers) > 0 {
		reasons := make([]string, 0, len(resp.IneligibleTiers))
		for _, tier := range resp.IneligibleTiers {
			if tier.ReasonMessage != "" {
				reasons = append(reasons, tier.ReasonMessage)
			} else if tier.ReasonCode != "" {
				reasons = append(reasons, tier.ReasonCode)
			}
		}
		if len(reasons) > 0 {
			snap.Raw["ineligible_reasons"] = strings.Join(reasons, " | ")
		}
	}
}

func applyQuotaBuckets(snap *core.UsageSnapshot, buckets []bucketInfo) quotaAggregationResult {
	result := quotaAggregationResult{bucketCount: len(buckets), worstFraction: 1.0}
	if len(buckets) == 0 {
		return result
	}

	aggregates := make(map[string]quotaAggregate)
	expiredCount := 0
	now := time.Now()
	for _, bucket := range buckets {
		fraction, ok := bucketRemainingFraction(bucket)
		if !ok {
			continue
		}
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}

		modelID := normalizeQuotaModelID(bucket.ModelID)
		tokenType := strings.ToLower(strings.TrimSpace(bucket.TokenType))
		if tokenType == "" {
			tokenType = "requests"
		}

		var resetAt time.Time
		hasReset := false
		if bucket.ResetTime != "" {
			if parsed, err := time.Parse(time.RFC3339, bucket.ResetTime); err == nil {
				resetAt = parsed
				hasReset = true
				if !resetAt.After(now) {
					expiredCount++
					continue
				}
			}
		}

		key := modelID + "|" + tokenType
		current, exists := aggregates[key]
		if !exists || fraction < current.remainingFraction {
			aggregates[key] = quotaAggregate{
				modelID:           modelID,
				tokenType:         tokenType,
				remainingFraction: fraction,
				resetAt:           resetAt,
				hasReset:          hasReset,
			}
			continue
		}
		if exists && fraction == current.remainingFraction && hasReset && (!current.hasReset || resetAt.Before(current.resetAt)) {
			current.resetAt = resetAt
			current.hasReset = true
			aggregates[key] = current
		}
	}

	if len(aggregates) == 0 {
		if expiredCount > 0 {
			snap.Raw["quota_expired_buckets_ignored"] = fmt.Sprintf("%d", expiredCount)
		}
		return result
	}

	keys := core.SortedStringKeys(aggregates)

	modelWorst := make(map[string]float64)
	var summary []string

	worstFraction := 1.0
	var worstMetric core.Metric
	worstFound := false
	var worstReset time.Time
	worstHasReset := false

	proFraction := 1.0
	var proMetric core.Metric
	proFound := false
	var proReset time.Time
	proHasReset := false

	flashFraction := 1.0
	var flashMetric core.Metric
	flashFound := false
	var flashReset time.Time
	flashHasReset := false

	for _, key := range keys {
		agg := aggregates[key]
		window := "daily"
		if agg.hasReset {
			window = formatWindow(time.Until(agg.resetAt))
		}
		metric := quotaMetricFromFraction(agg.remainingFraction, window)

		metricKey := "quota_model_" + sanitizeMetricName(agg.modelID) + "_" + sanitizeMetricName(agg.tokenType)
		snap.Metrics[metricKey] = metric
		if agg.hasReset {
			snap.Resets[metricKey+"_reset"] = agg.resetAt
		}

		usedPct := 100 - agg.remainingFraction*100
		summary = append(summary, fmt.Sprintf("%s %.1f%% used", agg.modelID, usedPct))

		if prev, ok := modelWorst[agg.modelID]; !ok || agg.remainingFraction < prev {
			modelWorst[agg.modelID] = agg.remainingFraction
		}

		if !worstFound || agg.remainingFraction < worstFraction {
			worstFraction = agg.remainingFraction
			worstMetric = metric
			worstFound = true
			worstReset = agg.resetAt
			worstHasReset = agg.hasReset
		}

		modelLower := strings.ToLower(agg.modelID)
		if strings.Contains(modelLower, "pro") && (!proFound || agg.remainingFraction < proFraction) {
			proFraction = agg.remainingFraction
			proMetric = metric
			proFound = true
			proReset = agg.resetAt
			proHasReset = agg.hasReset
		}
		if strings.Contains(modelLower, "flash") && (!flashFound || agg.remainingFraction < flashFraction) {
			flashFraction = agg.remainingFraction
			flashMetric = metric
			flashFound = true
			flashReset = agg.resetAt
			flashHasReset = agg.hasReset
		}
	}

	if len(summary) > maxBreakdownRaw {
		summary = summary[:maxBreakdownRaw]
	}
	if len(summary) > 0 {
		snap.Raw["quota_models"] = strings.Join(summary, ", ")
	}

	if worstFound {
		snap.Metrics["quota"] = worstMetric
		if worstHasReset {
			snap.Resets["quota_reset"] = worstReset
		}
		result.worstFraction = worstFraction
	}
	if proFound {
		snap.Metrics["quota_pro"] = proMetric
		if proHasReset {
			snap.Resets["quota_pro_reset"] = proReset
		}
	}
	if flashFound {
		snap.Metrics["quota_flash"] = flashMetric
		if flashHasReset {
			snap.Resets["quota_flash_reset"] = flashReset
		}
	}

	lowCount := 0
	exhaustedCount := 0
	for _, fraction := range modelWorst {
		if fraction <= 0 {
			exhaustedCount++
		}
		if fraction < quotaNearLimitFraction {
			lowCount++
		}
	}
	modelCount := len(modelWorst)
	result.modelCount = modelCount
	snap.Raw["quota_models_tracked"] = fmt.Sprintf("%d", modelCount)
	if expiredCount > 0 {
		snap.Raw["quota_expired_buckets_ignored"] = fmt.Sprintf("%d", expiredCount)
	}

	modelCountF := float64(modelCount)
	lowCountF := float64(lowCount)
	exhaustedCountF := float64(exhaustedCount)
	snap.Metrics["quota_models_tracked"] = core.Metric{Used: &modelCountF, Unit: "models", Window: "daily"}
	snap.Metrics["quota_models_low"] = core.Metric{Used: &lowCountF, Unit: "models", Window: "daily"}
	snap.Metrics["quota_models_exhausted"] = core.Metric{Used: &exhaustedCountF, Unit: "models", Window: "daily"}

	return result
}

func quotaMetricFromFraction(remainingFraction float64, window string) core.Metric {
	limit := 100.0
	remaining := remainingFraction * 100
	used := 100 - remaining
	return core.Metric{
		Limit:     &limit,
		Remaining: &remaining,
		Used:      &used,
		Unit:      "%",
		Window:    window,
	}
}

func normalizeQuotaModelID(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "all_models"
	}
	modelID = strings.TrimPrefix(modelID, "models/")
	modelID = strings.TrimSuffix(modelID, "_vertex")
	return modelID
}

func bucketRemainingFraction(bucket bucketInfo) (float64, bool) {
	if bucket.RemainingFraction != nil {
		return *bucket.RemainingFraction, true
	}
	if bucket.RemainingAmount == "" {
		return 0, false
	}
	return parseRemainingAmountFraction(bucket.RemainingAmount)
}

func parseRemainingAmountFraction(raw string) (float64, bool) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return 0, false
	}

	if strings.HasSuffix(s, "%") {
		value, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return 0, false
		}
		return value / 100, true
	}

	if strings.Contains(s, "/") {
		parts := strings.SplitN(s, "/", 2)
		if len(parts) != 2 {
			return 0, false
		}
		numerator, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		denominator, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 != nil || err2 != nil || denominator <= 0 {
			return 0, false
		}
		return numerator / denominator, true
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if value > 1 {
		return value / 100, true
	}
	return value, true
}

func applyQuotaStatus(snap *core.UsageSnapshot, worstFraction float64) {
	if worstFraction < 0 {
		return
	}

	desired := core.StatusOK
	if worstFraction <= 0 {
		desired = core.StatusLimited
	} else if worstFraction < quotaNearLimitFraction {
		desired = core.StatusNearLimit
	}

	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		return
	}

	severity := map[core.Status]int{
		core.StatusOK:        0,
		core.StatusNearLimit: 1,
		core.StatusLimited:   2,
	}
	if severity[desired] > severity[snap.Status] {
		snap.Status = desired
	}
}

func applyGeminiMCPMetadata(snap *core.UsageSnapshot, settings geminiSettings, enablementPath string) {
	configured := make(map[string]bool)
	for name := range settings.MCPServers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		configured[name] = true
	}

	enabled := make(map[string]bool)
	disabled := make(map[string]bool)
	if data, err := os.ReadFile(enablementPath); err == nil {
		var state map[string]geminiMCPEnablement
		if json.Unmarshal(data, &state) == nil {
			for name, cfg := range state {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				configured[name] = true
				if cfg.Enabled {
					enabled[name] = true
					delete(disabled, name)
					continue
				}
				if !enabled[name] {
					disabled[name] = true
				}
			}
		}
	}

	configuredNames := mapKeysSorted(configured)
	enabledNames := mapKeysSorted(enabled)
	disabledNames := mapKeysSorted(disabled)

	if len(configuredNames) == 0 {
		return
	}

	setUsedMetric(snap, "mcp_servers_configured", float64(len(configuredNames)), "servers", defaultUsageWindowLabel)
	if len(enabledNames) > 0 {
		setUsedMetric(snap, "mcp_servers_enabled", float64(len(enabledNames)), "servers", defaultUsageWindowLabel)
	}
	if len(disabledNames) > 0 {
		setUsedMetric(snap, "mcp_servers_disabled", float64(len(disabledNames)), "servers", defaultUsageWindowLabel)
	}
	if len(enabledNames)+len(disabledNames) > 0 {
		setUsedMetric(snap, "mcp_servers_tracked", float64(len(enabledNames)+len(disabledNames)), "servers", defaultUsageWindowLabel)
	}

	if summary := formatGeminiNameList(configuredNames, maxBreakdownRaw); summary != "" {
		snap.Raw["mcp_servers"] = summary
	}
	if summary := formatGeminiNameList(enabledNames, maxBreakdownRaw); summary != "" {
		snap.Raw["mcp_servers_enabled"] = summary
	}
	if summary := formatGeminiNameList(disabledNames, maxBreakdownRaw); summary != "" {
		snap.Raw["mcp_servers_disabled"] = summary
	}
}
