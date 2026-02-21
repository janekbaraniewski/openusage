package gemini_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
	oauthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	oauthClientSecret = "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"
	tokenEndpoint     = "https://oauth2.googleapis.com/token"

	codeAssistEndpoint   = "https://cloudcode-pa.googleapis.com"
	codeAssistAPIVersion = "v1internal"

	defaultUsageWindowLabel = "all-time"

	maxBreakdownMetrics = 8
	maxBreakdownRaw     = 6

	quotaNearLimitFraction = 0.15
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "gemini_cli",
			Info: core.ProviderInfo{
				Name:         "Gemini CLI",
				Capabilities: []string{"local_config", "oauth_status", "conversation_count", "local_sessions", "token_usage", "by_model", "by_client", "quota_api"},
				DocURL:       "https://github.com/google-gemini/gemini-cli",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeOAuth,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install and authenticate Gemini CLI locally.",
					"Verify OAuth credentials are available in the Gemini CLI config directory.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	ExpiryDate   int64  `json:"expiry_date"` // Unix millis
	RefreshToken string `json:"refresh_token"`
}

type googleAccounts struct {
	Active string   `json:"active"`
	Old    []string `json:"old"`
}

type geminiSettings struct {
	Security struct {
		Auth struct {
			SelectedType string `json:"selectedType"`
		} `json:"auth"`
	} `json:"security"`
	General struct {
		PreviewFeatures  bool `json:"previewFeatures"`
		EnableAutoUpdate bool `json:"enableAutoUpdate"`
	} `json:"general"`
	Experimental struct {
		Plan bool `json:"plan"`
	} `json:"experimental"`
}

type tokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type loadCodeAssistRequest struct {
	CloudAICompanionProject string         `json:"cloudaicompanionProject,omitempty"`
	Metadata                clientMetadata `json:"metadata"`
}

type clientMetadata struct {
	IDEType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
	Project    string `json:"duetProject,omitempty"`
}

type loadCodeAssistResponse struct {
	CurrentTier             *geminiTierInfo            `json:"currentTier,omitempty"`
	AllowedTiers            []geminiTierInfo           `json:"allowedTiers,omitempty"`
	IneligibleTiers         []geminiIneligibleTier     `json:"ineligibleTiers,omitempty"`
	CloudAICompanionProject string                     `json:"cloudaicompanionProject,omitempty"`
	GCPManaged              bool                       `json:"gcpManaged,omitempty"`
	UpgradeSubscriptionURI  string                     `json:"upgradeSubscriptionUri,omitempty"`
	UpgradeSubscriptionText string                     `json:"upgradeSubscriptionText,omitempty"`
	UpgradeSubscriptionType string                     `json:"upgradeSubscriptionType,omitempty"`
	Diagnostics             map[string]json.RawMessage `json:"-"`
}

type geminiTierInfo struct {
	ID                                 string `json:"id,omitempty"`
	Name                               string `json:"name,omitempty"`
	Description                        string `json:"description,omitempty"`
	UserDefinedCloudAICompanionProject bool   `json:"userDefinedCloudaicompanionProject,omitempty"`
	IsDefault                          bool   `json:"isDefault,omitempty"`
	UsesGCPTOS                         bool   `json:"usesGcpTos,omitempty"`
}

type geminiIneligibleTier struct {
	ReasonCode    string `json:"reasonCode,omitempty"`
	ReasonMessage string `json:"reasonMessage,omitempty"`
	TierID        string `json:"tierId,omitempty"`
	TierName      string `json:"tierName,omitempty"`
}

type retrieveUserQuotaRequest struct {
	Project string `json:"project"`
}

type retrieveUserQuotaResponse struct {
	Buckets []bucketInfo `json:"buckets,omitempty"`
}

type bucketInfo struct {
	RemainingAmount   string   `json:"remainingAmount,omitempty"`
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         string   `json:"resetTime,omitempty"` // ISO-8601
	TokenType         string   `json:"tokenType,omitempty"`
	ModelID           string   `json:"modelId,omitempty"`
}

type geminiChatFile struct {
	SessionID   string              `json:"sessionId"`
	StartTime   string              `json:"startTime"`
	LastUpdated string              `json:"lastUpdated"`
	ProjectHash string              `json:"projectHash"`
	Messages    []geminiChatMessage `json:"messages"`
}

type geminiChatMessage struct {
	Type      string              `json:"type"`
	Timestamp string              `json:"timestamp"`
	Model     string              `json:"model"`
	Content   json.RawMessage     `json:"content,omitempty"`
	Tokens    *geminiMessageToken `json:"tokens,omitempty"`
	ToolCalls []geminiToolCall    `json:"toolCalls,omitempty"`
}

type geminiToolCall struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type geminiMessageToken struct {
	Input    int `json:"input"`
	Output   int `json:"output"`
	Cached   int `json:"cached"`
	Thoughts int `json:"thoughts"`
	Tool     int `json:"tool"`
	Total    int `json:"total"`
}

type tokenUsage struct {
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	ReasoningTokens   int
	ToolTokens        int
	TotalTokens       int
}

type usageEntry struct {
	Name string
	Data tokenUsage
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	configDir := ""
	if acct.ExtraData != nil {
		configDir = acct.ExtraData["config_dir"]
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			configDir = filepath.Join(home, ".gemini")
		}
	}
	if configDir == "" {
		snap.Status = core.StatusError
		snap.Message = "Cannot determine Gemini CLI config directory"
		return snap, nil
	}

	var hasData bool
	var creds oauthCreds

	oauthFile := filepath.Join(configDir, "oauth_creds.json")
	if data, err := os.ReadFile(oauthFile); err == nil {
		if json.Unmarshal(data, &creds) == nil {
			hasData = true

			if creds.ExpiryDate > 0 {
				expiry := time.Unix(creds.ExpiryDate/1000, 0)
				if time.Now().Before(expiry) {
					snap.Raw["oauth_status"] = "valid"
					snap.Raw["oauth_expires"] = expiry.Format(time.RFC3339)
				} else if creds.RefreshToken != "" {
					snap.Raw["oauth_status"] = "expired (will refresh)"
				} else {
					snap.Raw["oauth_status"] = "expired"
					snap.Raw["oauth_expired_at"] = expiry.Format(time.RFC3339)
					snap.Status = core.StatusAuth
					snap.Message = "OAuth token expired — run `gemini` to re-authenticate"
				}
			}

			if creds.Scope != "" {
				snap.Raw["oauth_scope"] = creds.Scope
			}
		}
	}

	accountsFile := filepath.Join(configDir, "google_accounts.json")
	if data, err := os.ReadFile(accountsFile); err == nil {
		var accounts googleAccounts
		if json.Unmarshal(data, &accounts) == nil {
			hasData = true
			if accounts.Active != "" {
				snap.Raw["account_email"] = accounts.Active
			}
		}
	}

	settingsFile := filepath.Join(configDir, "settings.json")
	if data, err := os.ReadFile(settingsFile); err == nil {
		var settings geminiSettings
		if json.Unmarshal(data, &settings) == nil {
			hasData = true
			if settings.Security.Auth.SelectedType != "" {
				snap.Raw["auth_type"] = settings.Security.Auth.SelectedType
			}
			if settings.Experimental.Plan {
				snap.Raw["plan_mode"] = "enabled"
			}
			if settings.General.PreviewFeatures {
				snap.Raw["preview_features"] = "enabled"
			}
		}
	}

	idFile := filepath.Join(configDir, "installation_id")
	if data, err := os.ReadFile(idFile); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			snap.Raw["installation_id"] = id
		}
	}

	convDir := filepath.Join(configDir, "antigravity", "conversations")
	if entries, err := os.ReadDir(convDir); err == nil {
		count := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".pb") {
				count++
			}
		}
		if count > 0 {
			hasData = true
			convCount := float64(count)
			snap.Metrics["total_conversations"] = core.Metric{
				Used:   &convCount,
				Unit:   "conversations",
				Window: "all-time",
			}
		}
	}

	sessionCount, err := p.readSessionUsageBreakdowns(filepath.Join(configDir, "tmp"), &snap)
	if err != nil {
		snap.Raw["session_usage_error"] = err.Error()
	}
	if sessionCount > 0 {
		hasData = true
		existing, ok := snap.Metrics["total_conversations"]
		if !ok || existing.Used == nil || *existing.Used < float64(sessionCount) {
			conversations := float64(sessionCount)
			snap.Metrics["total_conversations"] = core.Metric{
				Used:   &conversations,
				Unit:   "conversations",
				Window: defaultUsageWindowLabel,
			}
		}
	}

	binary := acct.Binary
	if binary == "" {
		binary = "gemini"
	}
	if binPath, err := exec.LookPath(binary); err == nil {
		snap.Raw["binary"] = binPath
		var vOut strings.Builder
		vCmd := exec.CommandContext(ctx, binary, "--version")
		vCmd.Stdout = &vOut
		if vCmd.Run() == nil {
			version := strings.TrimSpace(vOut.String())
			if version != "" {
				snap.Raw["cli_version"] = version
			}
		}
	}

	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" && snap.Raw["account_email"] == "" {
			snap.Raw["account_email"] = email
		}
	}

	if creds.RefreshToken != "" {
		if err := p.fetchUsageFromAPI(ctx, &snap, creds, acct); err != nil {
			log.Printf("[gemini_cli] quota API error: %v", err)
			snap.Raw["quota_api_error"] = err.Error()
		}
	} else {
		snap.Raw["quota_api"] = "skipped (no refresh token)"
	}

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Gemini CLI data found"
		return snap, nil
	}

	if snap.Message == "" {
		if email := snap.Raw["account_email"]; email != "" {
			snap.Message = fmt.Sprintf("Gemini CLI (%s)", email)
		} else {
			snap.Message = "Gemini CLI local data"
		}
	}

	return snap, nil
}

func (p *Provider) fetchUsageFromAPI(ctx context.Context, snap *core.UsageSnapshot, creds oauthCreds, acct core.AccountConfig) error {
	accessToken, err := refreshAccessToken(ctx, creds.RefreshToken)
	if err != nil {
		snap.Status = core.StatusAuth
		snap.Message = "OAuth token refresh failed — run `gemini` to re-authenticate"
		return fmt.Errorf("token refresh: %w", err)
	}
	snap.Raw["oauth_status"] = "valid (refreshed)"

	projectID := ""
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		projectID = v
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		projectID = v
	}
	if projectID == "" && acct.ExtraData != nil {
		projectID = acct.ExtraData["project_id"]
	}

	loadResp, err := loadCodeAssistDetails(ctx, accessToken, projectID)
	if err != nil {
		return fmt.Errorf("loadCodeAssist: %w", err)
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

	quota, method, err := retrieveUserQuota(ctx, accessToken, projectID)
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

func refreshAccessToken(ctx context.Context, refreshToken string) (string, error) {
	return refreshAccessTokenWithEndpoint(ctx, refreshToken, tokenEndpoint)
}

func refreshAccessTokenWithEndpoint(ctx context.Context, refreshToken, endpoint string) (string, error) {
	data := url.Values{
		"client_id":     {oauthClientID},
		"client_secret": {oauthClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token refresh HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenRefreshResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in refresh response")
	}

	return tokenResp.AccessToken, nil
}

func loadCodeAssist(ctx context.Context, accessToken, existingProjectID string) (string, error) {
	resp, err := loadCodeAssistDetailsWithEndpoint(ctx, accessToken, existingProjectID, codeAssistEndpoint)
	if err != nil {
		return "", err
	}
	return resp.CloudAICompanionProject, nil
}

func loadCodeAssistWithEndpoint(ctx context.Context, accessToken, existingProjectID, baseURL string) (string, error) {
	resp, err := loadCodeAssistDetailsWithEndpoint(ctx, accessToken, existingProjectID, baseURL)
	if err != nil {
		return "", err
	}
	return resp.CloudAICompanionProject, nil
}

func loadCodeAssistDetails(ctx context.Context, accessToken, existingProjectID string) (*loadCodeAssistResponse, error) {
	return loadCodeAssistDetailsWithEndpoint(ctx, accessToken, existingProjectID, codeAssistEndpoint)
}

func loadCodeAssistDetailsWithEndpoint(ctx context.Context, accessToken, existingProjectID, baseURL string) (*loadCodeAssistResponse, error) {
	reqBody := loadCodeAssistRequest{
		CloudAICompanionProject: existingProjectID,
		Metadata: clientMetadata{
			IDEType:    "IDE_UNSPECIFIED",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
			Project:    existingProjectID,
		},
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "loadCodeAssist", reqBody, baseURL)
	if err != nil {
		return nil, err
	}

	var resp loadCodeAssistResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	return &resp, nil
}

func retrieveUserQuota(ctx context.Context, accessToken, projectID string) (*retrieveUserQuotaResponse, string, error) {
	return retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, codeAssistEndpoint)
}

func retrieveUserQuotaWithEndpoint(ctx context.Context, accessToken, projectID, baseURL string) (*retrieveUserQuotaResponse, string, error) {
	reqBody := retrieveUserQuotaRequest{
		Project: projectID,
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "retrieveUserQuota", reqBody, baseURL)
	if err != nil && shouldTryLegacyQuotaMethod(err) {
		legacyResp, legacyErr := retrieveUserUsageWithEndpoint(ctx, accessToken, projectID, baseURL)
		if legacyErr != nil {
			return nil, "", fmt.Errorf("retrieveUserQuota: %w; retrieveUserUsage: %w", err, legacyErr)
		}
		return legacyResp, "retrieveUserUsage", nil
	}
	if err != nil {
		return nil, "", err
	}

	var resp retrieveUserQuotaResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, "", fmt.Errorf("parse retrieveUserQuota response: %w", err)
	}

	return &resp, "retrieveUserQuota", nil
}

// retrieveUserUsage remains as a compatibility fallback for older endpoints.
func retrieveUserUsage(ctx context.Context, accessToken, projectID string) (*retrieveUserQuotaResponse, error) {
	return retrieveUserUsageWithEndpoint(ctx, accessToken, projectID, codeAssistEndpoint)
}

func retrieveUserUsageWithEndpoint(ctx context.Context, accessToken, projectID, baseURL string) (*retrieveUserQuotaResponse, error) {
	reqBody := retrieveUserQuotaRequest{
		Project: projectID,
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "retrieveUserUsage", reqBody, baseURL)
	if err != nil {
		return nil, err
	}

	var resp retrieveUserQuotaResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse retrieveUserUsage response: %w", err)
	}

	return &resp, nil
}

func codeAssistPost(ctx context.Context, accessToken, method string, body interface{}) ([]byte, error) {
	return codeAssistPostWithEndpoint(ctx, accessToken, method, body, codeAssistEndpoint)
}

func codeAssistPostWithEndpoint(ctx context.Context, accessToken, method string, body interface{}, baseURL string) ([]byte, error) {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s HTTP %d: %s", method, resp.StatusCode, truncate(string(respBody), 200))
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

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

func shouldTryLegacyQuotaMethod(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 404") || strings.Contains(msg, "not found")
}

func applyQuotaBuckets(snap *core.UsageSnapshot, buckets []bucketInfo) quotaAggregationResult {
	result := quotaAggregationResult{bucketCount: len(buckets), worstFraction: 1.0}
	if len(buckets) == 0 {
		return result
	}

	aggregates := make(map[string]quotaAggregate)
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
		return result
	}

	keys := make([]string, 0, len(aggregates))
	for key := range aggregates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

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

func (t geminiMessageToken) toUsage() tokenUsage {
	total := t.Total
	if total <= 0 {
		total = t.Input + t.Output + t.Cached + t.Thoughts + t.Tool
	}
	return tokenUsage{
		InputTokens:       t.Input,
		CachedInputTokens: t.Cached,
		OutputTokens:      t.Output,
		ReasoningTokens:   t.Thoughts,
		ToolTokens:        t.Tool,
		TotalTokens:       total,
	}
}

func (p *Provider) readSessionUsageBreakdowns(tmpDir string, snap *core.UsageSnapshot) (int, error) {
	files, err := findGeminiSessionFiles(tmpDir)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}

	modelTotals := make(map[string]tokenUsage)
	clientTotals := make(map[string]tokenUsage)
	toolTotals := make(map[string]int)
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	clientSessions := make(map[string]int)
	modelRequests := make(map[string]int)
	modelSessions := make(map[string]int)

	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	dailyInputTokens := make(map[string]float64)
	dailyOutputTokens := make(map[string]float64)
	dailyCachedTokens := make(map[string]float64)
	dailyReasoningTokens := make(map[string]float64)
	dailyToolTokens := make(map[string]float64)

	sessionIDs := make(map[string]bool)
	sessionCount := 0
	totalMessages := 0
	totalTurns := 0
	totalToolCalls := 0
	totalInfoMessages := 0
	totalErrorMessages := 0
	totalAssistantMessages := 0
	totalToolSuccess := 0
	totalToolFailed := 0
	quotaLimitEvents := 0

	var lastModelName string
	var lastModelTokens int
	foundLatest := false

	for _, path := range files {
		chat, err := readGeminiChatFile(path)
		if err != nil {
			continue
		}

		sessionID := strings.TrimSpace(chat.SessionID)
		if sessionID == "" {
			sessionID = path
		}
		if sessionIDs[sessionID] {
			continue
		}
		sessionIDs[sessionID] = true
		sessionCount++

		clientName := normalizeClientName("CLI")
		clientSessions[clientName]++

		sessionDay := dayFromSession(chat.StartTime, chat.LastUpdated)
		if sessionDay != "" {
			dailySessions[sessionDay]++
		}

		var previous tokenUsage
		var hasPrevious bool
		fileHasUsage := false
		sessionModels := make(map[string]bool)

		for _, msg := range chat.Messages {
			day := dayFromTimestamp(msg.Timestamp)
			if day == "" {
				day = sessionDay
			}

			switch strings.ToLower(strings.TrimSpace(msg.Type)) {
			case "info":
				totalInfoMessages++
			case "error":
				totalErrorMessages++
			case "gemini", "assistant", "model":
				totalAssistantMessages++
			}

			if isQuotaLimitMessage(msg.Content) {
				quotaLimitEvents++
			}

			if strings.EqualFold(msg.Type, "user") {
				totalMessages++
				if day != "" {
					dailyMessages[day]++
				}
			}

			if len(msg.ToolCalls) > 0 {
				totalToolCalls += len(msg.ToolCalls)
				if day != "" {
					dailyToolCalls[day] += float64(len(msg.ToolCalls))
				}
				for _, tc := range msg.ToolCalls {
					if tc.Name != "" {
						toolTotals[tc.Name]++
					}
					if strings.EqualFold(tc.Status, "success") {
						totalToolSuccess++
					} else if strings.TrimSpace(tc.Status) != "" {
						totalToolFailed++
					}
				}
			}

			if msg.Tokens == nil {
				continue
			}

			modelName := normalizeModelName(msg.Model)
			total := msg.Tokens.toUsage()

			// Track latest model usage from the most recent session file
			if !foundLatest {
				lastModelName = modelName
				lastModelTokens = total.TotalTokens
				fileHasUsage = true
			}
			modelRequests[modelName]++
			sessionModels[modelName] = true

			delta := total
			if hasPrevious {
				delta = usageDelta(total, previous)
				if !validUsageDelta(delta) {
					delta = total
				}
			}
			previous = total
			hasPrevious = true

			if delta.TotalTokens <= 0 {
				continue
			}

			addUsage(modelTotals, modelName, delta)
			addUsage(clientTotals, clientName, delta)

			if day != "" {
				addDailyUsage(modelDaily, modelName, day, float64(delta.TotalTokens))
				addDailyUsage(clientDaily, clientName, day, float64(delta.TotalTokens))
				dailyTokens[day] += float64(delta.TotalTokens)
				dailyInputTokens[day] += float64(delta.InputTokens)
				dailyOutputTokens[day] += float64(delta.OutputTokens)
				dailyCachedTokens[day] += float64(delta.CachedInputTokens)
				dailyReasoningTokens[day] += float64(delta.ReasoningTokens)
				dailyToolTokens[day] += float64(delta.ToolTokens)
			}

			totalTurns++
		}

		for modelName := range sessionModels {
			modelSessions[modelName]++
		}

		if fileHasUsage {
			foundLatest = true
		}
	}

	if sessionCount == 0 {
		return 0, nil
	}

	if lastModelName != "" && lastModelTokens > 0 {
		limit := getModelContextLimit(lastModelName)
		if limit > 0 {
			used := float64(lastModelTokens)
			lim := float64(limit)
			snap.Metrics["context_window"] = core.Metric{
				Used:   &used,
				Limit:  &lim,
				Unit:   "tokens",
				Window: "current",
			}
			snap.Raw["active_model"] = lastModelName
		}
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)
	emitModelRequestMetrics(modelRequests, modelSessions, snap)
	emitToolMetrics(toolTotals, snap)

	storeSeries(snap, "messages", dailyMessages)
	storeSeries(snap, "sessions", dailySessions)
	storeSeries(snap, "tool_calls", dailyToolCalls)
	storeSeries(snap, "tokens_total", dailyTokens)
	storeSeries(snap, "tokens_input", dailyInputTokens)
	storeSeries(snap, "tokens_output", dailyOutputTokens)
	storeSeries(snap, "tokens_cached", dailyCachedTokens)
	storeSeries(snap, "tokens_reasoning", dailyReasoningTokens)
	storeSeries(snap, "tokens_tool", dailyToolTokens)

	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_sessions", float64(sessionCount), "sessions", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_turns", float64(totalTurns), "turns", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tool_calls", float64(totalToolCalls), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_info_messages", float64(totalInfoMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_error_messages", float64(totalErrorMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_assistant_messages", float64(totalAssistantMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_calls_success", float64(totalToolSuccess), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "tool_calls_failed", float64(totalToolFailed), "calls", defaultUsageWindowLabel)
	setUsedMetric(snap, "quota_limit_events", float64(quotaLimitEvents), "events", defaultUsageWindowLabel)

	if cliUsage, ok := clientTotals["CLI"]; ok {
		setUsedMetric(snap, "client_cli_messages", float64(totalMessages), "messages", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_turns", float64(totalTurns), "turns", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_tool_calls", float64(totalToolCalls), "calls", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_input_tokens", float64(cliUsage.InputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_output_tokens", float64(cliUsage.OutputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_cached_tokens", float64(cliUsage.CachedInputTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_reasoning_tokens", float64(cliUsage.ReasoningTokens), "tokens", defaultUsageWindowLabel)
		setUsedMetric(snap, "client_cli_total_tokens", float64(cliUsage.TotalTokens), "tokens", defaultUsageWindowLabel)
	}

	total := aggregateTokenTotals(modelTotals)
	setUsedMetric(snap, "total_input_tokens", float64(total.InputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_output_tokens", float64(total.OutputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_cached_tokens", float64(total.CachedInputTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_reasoning_tokens", float64(total.ReasoningTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tool_tokens", float64(total.ToolTokens), "tokens", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tokens", float64(total.TotalTokens), "tokens", defaultUsageWindowLabel)

	if total.InputTokens > 0 {
		cacheEfficiency := float64(total.CachedInputTokens) / float64(total.InputTokens) * 100
		setPercentMetric(snap, "cache_efficiency", cacheEfficiency, defaultUsageWindowLabel)
	}
	if total.TotalTokens > 0 {
		reasoningShare := float64(total.ReasoningTokens) / float64(total.TotalTokens) * 100
		toolShare := float64(total.ToolTokens) / float64(total.TotalTokens) * 100
		setPercentMetric(snap, "reasoning_share", reasoningShare, defaultUsageWindowLabel)
		setPercentMetric(snap, "tool_token_share", toolShare, defaultUsageWindowLabel)
	}
	if totalTurns > 0 {
		avgTokensPerTurn := float64(total.TotalTokens) / float64(totalTurns)
		setUsedMetric(snap, "avg_tokens_per_turn", avgTokensPerTurn, "tokens", defaultUsageWindowLabel)
	}
	if sessionCount > 0 {
		avgToolsPerSession := float64(totalToolCalls) / float64(sessionCount)
		setUsedMetric(snap, "avg_tools_per_session", avgToolsPerSession, "calls", defaultUsageWindowLabel)
	}

	if _, v := latestSeriesValue(dailyMessages); v > 0 {
		setUsedMetric(snap, "messages_today", v, "messages", "today")
	}
	if _, v := latestSeriesValue(dailySessions); v > 0 {
		setUsedMetric(snap, "sessions_today", v, "sessions", "today")
	}
	if _, v := latestSeriesValue(dailyToolCalls); v > 0 {
		setUsedMetric(snap, "tool_calls_today", v, "calls", "today")
	}
	if _, v := latestSeriesValue(dailyTokens); v > 0 {
		setUsedMetric(snap, "tokens_today", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyInputTokens); v > 0 {
		setUsedMetric(snap, "today_input_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyOutputTokens); v > 0 {
		setUsedMetric(snap, "today_output_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyCachedTokens); v > 0 {
		setUsedMetric(snap, "today_cached_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyReasoningTokens); v > 0 {
		setUsedMetric(snap, "today_reasoning_tokens", v, "tokens", "today")
	}
	if _, v := latestSeriesValue(dailyToolTokens); v > 0 {
		setUsedMetric(snap, "today_tool_tokens", v, "tokens", "today")
	}

	setUsedMetric(snap, "7d_messages", sumLastNDays(dailyMessages, 7), "messages", "7d")
	setUsedMetric(snap, "7d_sessions", sumLastNDays(dailySessions, 7), "sessions", "7d")
	setUsedMetric(snap, "7d_tool_calls", sumLastNDays(dailyToolCalls, 7), "calls", "7d")
	setUsedMetric(snap, "7d_tokens", sumLastNDays(dailyTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_input_tokens", sumLastNDays(dailyInputTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_output_tokens", sumLastNDays(dailyOutputTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_cached_tokens", sumLastNDays(dailyCachedTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_reasoning_tokens", sumLastNDays(dailyReasoningTokens, 7), "tokens", "7d")
	setUsedMetric(snap, "7d_tool_tokens", sumLastNDays(dailyToolTokens, 7), "tokens", "7d")

	if quotaLimitEvents > 0 {
		snap.Raw["quota_limit_detected"] = "true"
		if _, hasQuota := snap.Metrics["quota"]; !hasQuota {
			limit := 100.0
			remaining := 0.0
			used := 100.0
			snap.Metrics["quota"] = core.Metric{
				Limit:     &limit,
				Remaining: &remaining,
				Used:      &used,
				Unit:      "%",
				Window:    "daily",
			}
			applyQuotaStatus(snap, 0)
		}
	}

	return sessionCount, nil
}

func findGeminiSessionFiles(tmpDir string) ([]string, error) {
	if strings.TrimSpace(tmpDir) == "" {
		return nil, nil
	}
	if _, err := os.Stat(tmpDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat tmp dir: %w", err)
	}

	type item struct {
		path    string
		modTime time.Time
	}
	var files []item

	walkErr := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		files = append(files, item{path: path, modTime: info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk gemini tmp dir: %w", walkErr)
	}
	if len(files) == 0 {
		return nil, nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path > files[j].path
		}
		return files[i].modTime.After(files[j].modTime)
	})

	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.path)
	}
	return out, nil
}

func readGeminiChatFile(path string) (*geminiChatFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chat geminiChatFile
	if err := json.NewDecoder(f).Decode(&chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func emitBreakdownMetrics(prefix string, totals map[string]tokenUsage, daily map[string]map[string]float64, snap *core.UsageSnapshot) {
	entries := sortUsageEntries(totals)
	if len(entries) == 0 {
		return
	}

	for i, entry := range entries {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := prefix + "_" + sanitizeMetricName(entry.Name)
		setUsageMetric(snap, keyPrefix+"_total_tokens", float64(entry.Data.TotalTokens))
		setUsageMetric(snap, keyPrefix+"_input_tokens", float64(entry.Data.InputTokens))
		setUsageMetric(snap, keyPrefix+"_output_tokens", float64(entry.Data.OutputTokens))

		if entry.Data.CachedInputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_cached_tokens", float64(entry.Data.CachedInputTokens))
		}
		if entry.Data.ReasoningTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			seriesKey := "tokens_" + prefix + "_" + sanitizeMetricName(entry.Name)
			snap.DailySeries[seriesKey] = mapToSortedTimePoints(byDay)
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "json",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningTokens))
			}
			core.AppendModelUsageRecord(snap, rec)
		}
	}

	snap.Raw[prefix+"_usage"] = formatUsageSummary(entries, maxBreakdownRaw)
}

func emitClientSessionMetrics(clientSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range clientSessions {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		value := float64(item.count)
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{
			Used:   &value,
			Unit:   "sessions",
			Window: defaultUsageWindowLabel,
		}
	}
}

func emitModelRequestMetrics(modelRequests, modelSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name     string
		requests int
		sessions int
	}

	all := make([]entry, 0, len(modelRequests))
	for name, requests := range modelRequests {
		if requests <= 0 {
			continue
		}
		all = append(all, entry{name: name, requests: requests, sessions: modelSessions[name]})
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].requests == all[j].requests {
			return all[i].name < all[j].name
		}
		return all[i].requests > all[j].requests
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := "model_" + sanitizeMetricName(item.name)
		req := float64(item.requests)
		sess := float64(item.sessions)
		snap.Metrics[keyPrefix+"_requests"] = core.Metric{
			Used:   &req,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
		if item.sessions > 0 {
			snap.Metrics[keyPrefix+"_sessions"] = core.Metric{
				Used:   &sess,
				Unit:   "sessions",
				Window: defaultUsageWindowLabel,
			}
		}
	}
}

func emitToolMetrics(toolTotals map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range toolTotals {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	var parts []string
	limit := maxBreakdownRaw
	for i, item := range all {
		if i < limit {
			parts = append(parts, fmt.Sprintf("%s (%d)", item.name, item.count))
		}

		val := float64(item.count)
		snap.Metrics["tool_"+sanitizeMetricName(item.name)] = core.Metric{
			Used:   &val,
			Unit:   "calls",
			Window: defaultUsageWindowLabel,
		}
	}

	if len(all) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(all)-limit))
	}

	if len(parts) > 0 {
		snap.Raw["tool_usage"] = strings.Join(parts, ", ")
	}
}

func aggregateTokenTotals(modelTotals map[string]tokenUsage) tokenUsage {
	var total tokenUsage
	for _, usage := range modelTotals {
		total.InputTokens += usage.InputTokens
		total.CachedInputTokens += usage.CachedInputTokens
		total.OutputTokens += usage.OutputTokens
		total.ReasoningTokens += usage.ReasoningTokens
		total.ToolTokens += usage.ToolTokens
		total.TotalTokens += usage.TotalTokens
	}
	return total
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   &value,
		Unit:   "tokens",
		Window: defaultUsageWindowLabel,
	}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningTokens += delta.ReasoningTokens
	current.ToolTokens += delta.ToolTokens
	current.TotalTokens += delta.TotalTokens
	target[name] = current
}

func addDailyUsage(target map[string]map[string]float64, name, day string, value float64) {
	if day == "" || value <= 0 {
		return
	}
	if target[name] == nil {
		target[name] = make(map[string]float64)
	}
	target[name][day] += value
}

func sortUsageEntries(values map[string]tokenUsage) []usageEntry {
	out := make([]usageEntry, 0, len(values))
	for name, data := range values {
		out = append(out, usageEntry{Name: name, Data: data})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Data.TotalTokens == out[j].Data.TotalTokens {
			return out[i].Name < out[j].Name
		}
		return out[i].Data.TotalTokens > out[j].Data.TotalTokens
	})
	return out
}

func formatUsageSummary(entries []usageEntry, max int) string {
	total := 0
	for _, entry := range entries {
		total += entry.Data.TotalTokens
	}
	if total <= 0 {
		return ""
	}

	limit := max
	if limit > len(entries) {
		limit = len(entries)
	}

	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		entry := entries[i]
		pct := float64(entry.Data.TotalTokens) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entry.Name, formatTokenCount(entry.Data.TotalTokens), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func formatTokenCount(value int) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:       current.InputTokens - previous.InputTokens,
		CachedInputTokens: current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:      current.OutputTokens - previous.OutputTokens,
		ReasoningTokens:   current.ReasoningTokens - previous.ReasoningTokens,
		ToolTokens:        current.ToolTokens - previous.ToolTokens,
		TotalTokens:       current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningTokens >= 0 &&
		delta.ToolTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func normalizeClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Other"
	}
	return name
}

func sanitizeMetricName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

// getModelContextLimit returns the known context window size for a given Gemini model.
// Since the Gemini CLI's internal API does not expose model metadata like context limits
// in the session payload, we fallback to static configuration based on public documentation.
//
// Sources:
// - Gemini 1.5 Pro (2M): https://blog.google/technology/ai/google-gemini-update-flash-ai-assistant-io-2024/#gemini-1-5-pro
// - Gemini 1.5 Flash (1M): https://blog.google/technology/ai/google-gemini-update-flash-ai-assistant-io-2024/#gemini-1-5-flash
// - Gemini 2.0 Flash (1M): https://ai.google.dev/gemini-api/docs/models/gemini-v2
func getModelContextLimit(model string) int {
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "1.5-pro"), strings.Contains(model, "1.5-flash-8b"):
		return 2_000_000
	case strings.Contains(model, "1.5-flash"):
		return 1_000_000
	case strings.Contains(model, "2.0-flash"):
		return 1_000_000
	case strings.Contains(model, "gemini-3"), strings.Contains(model, "gemini-exp"):
		// Assuming recent experimental/v3 models follow the 2M trend of 1.5 Pro/Exp.
		// Subject to change as these are preview models.
		return 2_000_000
	case strings.Contains(model, "pro"):
		return 32_000 // Legacy Gemini 1.0 Pro
	case strings.Contains(model, "flash"):
		return 32_000 // Fallback for older flash-like models if any
	}
	return 0
}

func dayFromTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, timestamp); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	if len(timestamp) >= 10 {
		candidate := timestamp[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func dayFromSession(startTime, lastUpdated string) string {
	if day := dayFromTimestamp(lastUpdated); day != "" {
		return day
	}
	return dayFromTimestamp(startTime)
}

func mapToSortedTimePoints(byDate map[string]float64) []core.TimePoint {
	if len(byDate) == 0 {
		return nil
	}
	keys := make([]string, 0, len(byDate))
	for date := range byDate {
		keys = append(keys, date)
	}
	sort.Strings(keys)

	points := make([]core.TimePoint, 0, len(keys))
	for _, date := range keys {
		points = append(points, core.TimePoint{Date: date, Value: byDate[date]})
	}
	return points
}

func storeSeries(snap *core.UsageSnapshot, key string, values map[string]float64) {
	if len(values) == 0 {
		return
	}
	snap.DailySeries[key] = mapToSortedTimePoints(values)
}

func latestSeriesValue(values map[string]float64) (string, float64) {
	if len(values) == 0 {
		return "", 0
	}
	dates := make([]string, 0, len(values))
	for date := range values {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	last := dates[len(dates)-1]
	return last, values[last]
}

func sumLastNDays(values map[string]float64, days int) float64 {
	if len(values) == 0 || days <= 0 {
		return 0
	}
	lastDate, _ := latestSeriesValue(values)
	if lastDate == "" {
		return 0
	}
	end, err := time.Parse("2006-01-02", lastDate)
	if err != nil {
		return 0
	}
	start := end.AddDate(0, 0, -(days - 1))

	total := 0.0
	for date, value := range values {
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		if !t.Before(start) && !t.After(end) {
			total += value
		}
	}
	return total
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func setPercentMetric(snap *core.UsageSnapshot, key string, value float64, window string) {
	if value < 0 {
		return
	}
	if value > 100 {
		value = 100
	}
	v := value
	limit := 100.0
	remaining := 100 - value
	snap.Metrics[key] = core.Metric{
		Used:      &v,
		Limit:     &limit,
		Remaining: &remaining,
		Unit:      "%",
		Window:    window,
	}
}

func isQuotaLimitMessage(content json.RawMessage) bool {
	text := strings.ToLower(parseMessageContentText(content))
	if text == "" {
		return false
	}
	return strings.Contains(text, "usage limit reached") ||
		strings.Contains(text, "all pro models") ||
		strings.Contains(text, "/stats for usage details")
}

func parseMessageContentText(content json.RawMessage) string {
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return ""
	}

	var asString string
	if content[0] == '"' && json.Unmarshal(content, &asString) == nil {
		return asString
	}

	var asArray []map[string]any
	if content[0] == '[' && json.Unmarshal(content, &asArray) == nil {
		var parts []string
		for _, item := range asArray {
			if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}

	return string(content)
}
