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
	CurrentTier             *json.RawMessage `json:"currentTier,omitempty"`
	CloudAICompanionProject string           `json:"cloudaicompanionProject,omitempty"`
}

type retrieveUserUsageRequest struct {
	Project string `json:"project"`
}

type retrieveUserUsageResponse struct {
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
	Tokens    *geminiMessageToken `json:"tokens,omitempty"`
	ToolCalls []struct{}          `json:"toolCalls,omitempty"`
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

	quota, err := retrieveUserUsage(ctx, accessToken, projectID)
	if err != nil {
		return fmt.Errorf("retrieveUserUsage: %w", err)
	}

	if len(quota.Buckets) == 0 {
		snap.Raw["quota_api"] = "ok (no buckets returned)"
		return nil
	}

	snap.Raw["quota_api"] = fmt.Sprintf("ok (%d buckets)", len(quota.Buckets))

	worstFraction := 1.0
	for _, bucket := range quota.Buckets {
		if bucket.ModelID == "" || bucket.RemainingFraction == nil {
			continue
		}

		fraction := *bucket.RemainingFraction
		remaining := fraction * 100 // percentage
		limit := float64(100)

		modelKey := sanitizeMetricName(bucket.ModelID)
		if modelKey == "" {
			modelKey = "unknown_model"
		}
		tokenKey := sanitizeMetricName(bucket.TokenType)
		if tokenKey == "" {
			tokenKey = "quota"
		}
		metricKey := modelKey + "_" + tokenKey

		window := "daily"
		if bucket.ResetTime != "" {
			if resetT, err := time.Parse(time.RFC3339, bucket.ResetTime); err == nil {
				dur := time.Until(resetT)
				if dur > 0 {
					window = formatWindow(dur)
				}
			}
		}

		unit := "quota"
		if bucket.TokenType != "" {
			unit = strings.ToLower(strings.TrimSpace(bucket.TokenType))
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
	if worstFraction <= 0 {
		snap.Status = core.StatusLimited
	} else if worstFraction < 0.15 {
		snap.Status = core.StatusNearLimit
	} else {
		snap.Status = core.StatusOK
	}

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
	return loadCodeAssistWithEndpoint(ctx, accessToken, existingProjectID, codeAssistEndpoint)
}

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

func retrieveUserUsage(ctx context.Context, accessToken, projectID string) (*retrieveUserUsageResponse, error) {
	return retrieveUserUsageWithEndpoint(ctx, accessToken, projectID, codeAssistEndpoint)
}

func retrieveUserUsageWithEndpoint(ctx context.Context, accessToken, projectID, baseURL string) (*retrieveUserUsageResponse, error) {
	reqBody := retrieveUserUsageRequest{
		Project: projectID,
	}

	respBody, err := codeAssistPostWithEndpoint(ctx, accessToken, "retrieveUserUsage", reqBody, baseURL)
	if err != nil {
		return nil, err
	}

	var resp retrieveUserUsageResponse
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
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	clientSessions := make(map[string]int)

	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	dailyTokens := make(map[string]float64)

	sessionIDs := make(map[string]bool)
	sessionCount := 0
	totalMessages := 0
	totalTurns := 0
	totalToolCalls := 0

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

		for _, msg := range chat.Messages {
			day := dayFromTimestamp(msg.Timestamp)
			if day == "" {
				day = sessionDay
			}

			if strings.EqualFold(msg.Type, "user") {
				totalMessages++
				if day != "" {
					dailyMessages[day]++
				}
			}

			if msg.Tokens == nil {
				continue
			}

			modelName := normalizeModelName(msg.Model)
			total := msg.Tokens.toUsage()
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
			}

			totalTurns++

			if toolCalls := len(msg.ToolCalls); toolCalls > 0 {
				totalToolCalls += toolCalls
				if day != "" {
					dailyToolCalls[day] += float64(toolCalls)
				}
			}
		}
	}

	if sessionCount == 0 {
		return 0, nil
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)

	storeSeries(snap, "messages", dailyMessages)
	storeSeries(snap, "sessions", dailySessions)
	storeSeries(snap, "tool_calls", dailyToolCalls)
	storeSeries(snap, "tokens_total", dailyTokens)

	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_sessions", float64(sessionCount), "sessions", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_turns", float64(totalTurns), "turns", defaultUsageWindowLabel)
	setUsedMetric(snap, "total_tool_calls", float64(totalToolCalls), "calls", defaultUsageWindowLabel)

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

	setUsedMetric(snap, "7d_messages", sumLastNDays(dailyMessages, 7), "messages", "7d")
	setUsedMetric(snap, "7d_sessions", sumLastNDays(dailySessions, 7), "sessions", "7d")
	setUsedMetric(snap, "7d_tool_calls", sumLastNDays(dailyToolCalls, 7), "calls", "7d")
	setUsedMetric(snap, "7d_tokens", sumLastNDays(dailyTokens, 7), "tokens", "7d")

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
