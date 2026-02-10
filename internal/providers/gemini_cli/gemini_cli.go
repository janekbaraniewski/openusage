// Package gemini_cli implements a QuotaProvider that reads Google Gemini CLI's
// local configuration and OAuth data, then queries the internal CodeAssist API
// for real quota information (remaining fraction per model, reset times).
//
// Data sources (at ~/.gemini/):
//   - oauth_creds.json — OAuth access/refresh tokens, expiry
//   - google_accounts.json — active Google account email
//   - settings.json — auth type, feature flags, tool settings
//   - installation_id — unique CLI installation ID
//   - antigravity/conversations/ — protobuf session files (counted only)
//
// API flow (reverse-engineered from Gemini CLI source):
//  1. Refresh the OAuth access token using the stored refresh_token
//  2. POST loadCodeAssist → get the cloudaicompanionProject (project ID)
//  3. POST retrieveUserQuota with the project ID → get quota buckets
//     Each bucket has: modelId, remainingFraction, resetTime, remainingAmount, tokenType
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
	"strings"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

const (
	// OAuth2 client credentials from Gemini CLI (public/installed-app — safe to embed).
	oauthClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	oauthClientSecret = "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"
	tokenEndpoint     = "https://oauth2.googleapis.com/token"

	// CodeAssist API
	codeAssistEndpoint   = "https://cloudcode-pa.googleapis.com"
	codeAssistAPIVersion = "v1internal"
)

// ── Pricing reference ───────────────────────────────────────────────────────
// Gemini CLI uses the free CodeAssist tier by default. For Google One AI
// Premium or Google Workspace subscribers, higher quotas are available.
//
// Subscription tiers:
//   - Free tier: rate-limited (varies by model, ~60 RPM for flash, 5 RPM for pro)
//   - Google One AI Premium ($19.99/mo): higher rate limits, 2TB storage, etc.
//   - Google Workspace: enterprise quotas via admin console
//
// When using Gemini CLI, there is no per-token billing — it's included in
// the subscription. The quota system uses "remaining fraction" per model.
const pricingInfo = "Free tier: rate-limited per model · " +
	"Google AI Premium: $19.99/mo (higher quotas, 2TB storage) · " +
	"No per-token billing — usage is quota-based"

// Provider reads Gemini CLI's local data and fetches quotas from CodeAssist API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "gemini_cli" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Gemini CLI",
		Capabilities: []string{"local_config", "oauth_status", "conversation_count", "quota_api"},
		DocURL:       "https://github.com/google-gemini/gemini-cli",
	}
}

// ---------------------------------------------------------------------------
// Local file types
// ---------------------------------------------------------------------------

// oauthCreds mirrors ~/.gemini/oauth_creds.json.
type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	ExpiryDate   int64  `json:"expiry_date"` // Unix millis
	RefreshToken string `json:"refresh_token"`
}

// googleAccounts mirrors ~/.gemini/google_accounts.json.
type googleAccounts struct {
	Active string   `json:"active"`
	Old    []string `json:"old"`
}

// geminiSettings mirrors ~/.gemini/settings.json.
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

// ---------------------------------------------------------------------------
// CodeAssist API types
// ---------------------------------------------------------------------------

// tokenRefreshResponse is the response from Google's OAuth2 token endpoint.
type tokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// loadCodeAssistRequest is the request body for the loadCodeAssist endpoint.
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

// loadCodeAssistResponse contains the project ID we need.
type loadCodeAssistResponse struct {
	CurrentTier             *json.RawMessage `json:"currentTier,omitempty"`
	CloudAICompanionProject string           `json:"cloudaicompanionProject,omitempty"`
}

// retrieveUserQuotaRequest is the request for retrieveUserQuota.
type retrieveUserQuotaRequest struct {
	Project string `json:"project"`
}

// retrieveUserQuotaResponse contains quota buckets.
type retrieveUserQuotaResponse struct {
	Buckets []bucketInfo `json:"buckets,omitempty"`
}

// bucketInfo represents a single quota bucket for a model.
type bucketInfo struct {
	RemainingAmount   string   `json:"remainingAmount,omitempty"`
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         string   `json:"resetTime,omitempty"` // ISO-8601
	TokenType         string   `json:"tokenType,omitempty"`
	ModelID           string   `json:"modelId,omitempty"`
}

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	// Determine config directory
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

	// 1. Read OAuth credentials for auth status & token refresh
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
					// Token expired but we have a refresh token — we'll refresh it later
					snap.Raw["oauth_status"] = "expired (will refresh)"
				} else {
					// Token expired and no refresh token — need re-auth
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

	// 2. Read account info
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

	// 3. Read settings
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

	// 4. Read installation ID
	idFile := filepath.Join(configDir, "installation_id")
	if data, err := os.ReadFile(idFile); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			snap.Raw["installation_id"] = id
		}
	}

	// 5. Count conversations (antigravity dir)
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

	// 6. Get CLI version
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

	// 7. Add account metadata from detection
	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" && snap.Raw["account_email"] == "" {
			snap.Raw["account_email"] = email
		}
	}

	// 8. Fetch real quota data from CodeAssist API
	if creds.RefreshToken != "" {
		if err := p.fetchQuotaFromAPI(ctx, &snap, creds, acct); err != nil {
			log.Printf("[gemini_cli] quota API error: %v", err)
			snap.Raw["quota_api_error"] = err.Error()
			// Don't override status if we have local data — quota API failure is non-fatal
		}
	} else {
		snap.Raw["quota_api"] = "skipped (no refresh token)"
	}

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Gemini CLI data found"
		return snap, nil
	}

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingInfo

	if snap.Message == "" {
		if email := snap.Raw["account_email"]; email != "" {
			snap.Message = fmt.Sprintf("Gemini CLI (%s)", email)
		} else {
			snap.Message = "Gemini CLI local data"
		}
	}

	return snap, nil
}

// ---------------------------------------------------------------------------
// CodeAssist API integration
// ---------------------------------------------------------------------------

// fetchQuotaFromAPI refreshes the OAuth token, discovers the project ID,
// and retrieves per-model quota buckets from the CodeAssist API.
func (p *Provider) fetchQuotaFromAPI(ctx context.Context, snap *core.QuotaSnapshot, creds oauthCreds, acct core.AccountConfig) error {
	// Step 1: Refresh the access token
	accessToken, err := refreshAccessToken(ctx, creds.RefreshToken)
	if err != nil {
		snap.Status = core.StatusAuth
		snap.Message = "OAuth token refresh failed — run `gemini` to re-authenticate"
		return fmt.Errorf("token refresh: %w", err)
	}
	snap.Raw["oauth_status"] = "valid (refreshed)"

	// Step 2: Get the project ID
	projectID := ""
	// Check env vars first (same precedence as Gemini CLI)
	if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		projectID = v
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT_ID"); v != "" {
		projectID = v
	}
	// Check if detection stashed a project ID
	if projectID == "" && acct.ExtraData != nil {
		projectID = acct.ExtraData["project_id"]
	}

	// If still empty, call loadCodeAssist to discover it
	if projectID == "" {
		projectID, err = loadCodeAssist(ctx, accessToken, "")
		if err != nil {
			return fmt.Errorf("loadCodeAssist: %w", err)
		}
	}

	if projectID == "" {
		return fmt.Errorf("could not determine project ID")
	}
	snap.Raw["project_id"] = projectID

	// Step 3: Retrieve user quota
	quota, err := retrieveUserQuota(ctx, accessToken, projectID)
	if err != nil {
		return fmt.Errorf("retrieveUserQuota: %w", err)
	}

	if len(quota.Buckets) == 0 {
		snap.Raw["quota_api"] = "ok (no buckets returned)"
		return nil
	}

	snap.Raw["quota_api"] = fmt.Sprintf("ok (%d buckets)", len(quota.Buckets))

	// Step 4: Map buckets to Metrics
	worstFraction := 1.0
	for _, bucket := range quota.Buckets {
		if bucket.ModelID == "" || bucket.RemainingFraction == nil {
			continue
		}

		fraction := *bucket.RemainingFraction
		remaining := fraction * 100 // percentage
		limit := float64(100)

		metricKey := bucket.ModelID
		if bucket.TokenType != "" {
			metricKey = fmt.Sprintf("%s_%s", bucket.ModelID, bucket.TokenType)
		}

		window := "daily"
		if bucket.ResetTime != "" {
			if resetT, err := time.Parse(time.RFC3339, bucket.ResetTime); err == nil {
				snap.Resets[metricKey] = resetT

				// Compute human-readable window from reset time
				dur := time.Until(resetT)
				if dur > 0 {
					window = formatWindow(dur)
				}
			}
		}

		unit := "quota"
		if bucket.TokenType != "" {
			unit = bucket.TokenType
		}

		snap.Metrics[metricKey] = core.Metric{
			Limit:     &limit,
			Remaining: &remaining,
			Unit:      unit,
			Window:    window,
		}

		if fraction < worstFraction {
			worstFraction = fraction
		}
	}

	// Update overall status based on worst bucket
	if worstFraction <= 0 {
		snap.Status = core.StatusLimited
	} else if worstFraction < 0.15 {
		snap.Status = core.StatusNearLimit
	} else {
		snap.Status = core.StatusOK
	}

	return nil
}

// refreshAccessToken uses the refresh_token to obtain a fresh access_token.
func refreshAccessToken(ctx context.Context, refreshToken string) (string, error) {
	return refreshAccessTokenWithEndpoint(ctx, refreshToken, tokenEndpoint)
}

// refreshAccessTokenWithEndpoint is the testable variant that accepts a custom token endpoint.
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

// loadCodeAssist calls the loadCodeAssist endpoint to discover the cloudaicompanionProject.
func loadCodeAssist(ctx context.Context, accessToken, existingProjectID string) (string, error) {
	return loadCodeAssistWithEndpoint(ctx, accessToken, existingProjectID, codeAssistEndpoint)
}

// loadCodeAssistWithEndpoint is the testable variant that accepts a custom base URL.
func loadCodeAssistWithEndpoint(ctx context.Context, accessToken, existingProjectID, baseURL string) (string, error) {
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
		return "", err
	}

	var resp loadCodeAssistResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("parse loadCodeAssist response: %w", err)
	}

	return resp.CloudAICompanionProject, nil
}

// retrieveUserQuota calls the retrieveUserQuota endpoint.
func retrieveUserQuota(ctx context.Context, accessToken, projectID string) (*retrieveUserQuotaResponse, error) {
	return retrieveUserQuotaWithEndpoint(ctx, accessToken, projectID, codeAssistEndpoint)
}

// retrieveUserQuotaWithEndpoint is the testable variant that accepts a custom base URL.
func retrieveUserQuotaWithEndpoint(ctx context.Context, accessToken, projectID, baseURL string) (*retrieveUserQuotaResponse, error) {
	reqBody := retrieveUserQuotaRequest{
		Project: projectID,
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "retrieveUserQuota", reqBody, baseURL)
	if err != nil {
		return nil, err
	}

	var resp retrieveUserQuotaResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parse retrieveUserQuota response: %w", err)
	}

	return &resp, nil
}

// codeAssistPost makes an authenticated POST request to the CodeAssist API.
func codeAssistPost(ctx context.Context, accessToken, method string, body interface{}) ([]byte, error) {
	return codeAssistPostWithEndpoint(ctx, accessToken, method, body, codeAssistEndpoint)
}

// codeAssistPostWithEndpoint is the testable variant that accepts a custom base URL.
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// formatWindow converts a duration to a human-readable quota window string.
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

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
