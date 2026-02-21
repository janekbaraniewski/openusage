package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	defaultCodexConfigDir   = ".codex"
	defaultChatGPTBaseURL   = "https://chatgpt.com/backend-api"
	defaultUsageWindowLabel = "all-time"

	maxScannerBufferSize = 8 * 1024 * 1024
	maxHTTPErrorBodySize = 256

	maxBreakdownMetrics = 8
	maxBreakdownRaw     = 6
)

var errLiveUsageAuth = errors.New("live usage auth failed")

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "codex" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenAI Codex CLI",
		Capabilities: []string{"local_sessions", "live_usage_endpoint", "rate_limits", "token_usage", "credits", "by_model", "by_client"},
		DocURL:       "https://github.com/openai/codex",
	}
}

type sessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type eventPayload struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info,omitempty"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
}

type tokenInfo struct {
	TotalTokenUsage    tokenUsage `json:"total_token_usage"`
	LastTokenUsage     tokenUsage `json:"last_token_usage"`
	ModelContextWindow int        `json:"model_context_window"`
}

type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

type rateLimits struct {
	Primary   *rateLimitBucket `json:"primary,omitempty"`
	Secondary *rateLimitBucket `json:"secondary,omitempty"`
	Credits   *creditInfo      `json:"credits,omitempty"`
	PlanType  *string          `json:"plan_type,omitempty"`
}

type rateLimitBucket struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"` // Unix timestamp
}

type creditInfo struct {
	HasCredits bool     `json:"has_credits"`
	Unlimited  bool     `json:"unlimited"`
	Balance    *float64 `json:"balance"`
}

type versionInfo struct {
	LatestVersion string `json:"latest_version"`
	LastCheckedAt string `json:"last_checked_at"`
}

type authFile struct {
	AccountID string     `json:"account_id,omitempty"`
	Tokens    authTokens `json:"tokens"`
}

type authTokens struct {
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id,omitempty"`
}

type usagePayload struct {
	UserID               string                 `json:"user_id,omitempty"`
	AccountID            string                 `json:"account_id,omitempty"`
	Email                string                 `json:"email,omitempty"`
	PlanType             string                 `json:"plan_type,omitempty"`
	RateLimit            *usageLimitDetails     `json:"rate_limit,omitempty"`
	CodeReviewRateLimit  *usageLimitDetails     `json:"code_review_rate_limit,omitempty"`
	AdditionalRateLimits []usageAdditionalLimit `json:"additional_rate_limits,omitempty"`
	Credits              *usageCredits          `json:"credits,omitempty"`
}

type usageLimitDetails struct {
	Allowed         bool             `json:"allowed"`
	LimitReached    bool             `json:"limit_reached"`
	PrimaryWindow   *usageWindowInfo `json:"primary_window,omitempty"`
	SecondaryWindow *usageWindowInfo `json:"secondary_window,omitempty"`
}

type usageWindowInfo struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type usageAdditionalLimit struct {
	LimitName      string             `json:"limit_name,omitempty"`
	MeteredFeature string             `json:"metered_feature,omitempty"`
	RateLimit      *usageLimitDetails `json:"rate_limit,omitempty"`
}

type usageCredits struct {
	HasCredits bool `json:"has_credits"`
	Unlimited  bool `json:"unlimited"`
	Balance    any  `json:"balance"`
}

type sessionMetaPayload struct {
	Source     string `json:"source,omitempty"`
	Originator string `json:"originator,omitempty"`
	Model      string `json:"model,omitempty"`
}

type turnContextPayload struct {
	Model string `json:"model,omitempty"`
}

type usageEntry struct {
	Name string
	Data tokenUsage
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
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
			configDir = filepath.Join(home, defaultCodexConfigDir)
		}
	}

	if configDir == "" {
		snap.Status = core.StatusError
		snap.Message = "Cannot determine Codex config directory"
		return snap, nil
	}

	var hasLocalData bool

	sessionsDir := filepath.Join(configDir, "sessions")
	if acct.ExtraData != nil && acct.ExtraData["sessions_dir"] != "" {
		sessionsDir = acct.ExtraData["sessions_dir"]
	}

	if err := p.readLatestSession(sessionsDir, &snap); err != nil {
		snap.Raw["session_error"] = err.Error()
	} else {
		hasLocalData = true
	}

	p.readDailySessionCounts(sessionsDir, &snap)
	if err := p.readSessionUsageBreakdowns(sessionsDir, &snap); err != nil {
		snap.Raw["split_error"] = err.Error()
	}

	hasLiveData, liveErr := p.fetchLiveUsage(ctx, acct, configDir, &snap)
	if liveErr != nil {
		snap.Raw["quota_api_error"] = liveErr.Error()
	}

	versionFile := filepath.Join(configDir, "version.json")
	if data, err := os.ReadFile(versionFile); err == nil {
		var ver versionInfo
		if json.Unmarshal(data, &ver) == nil && ver.LatestVersion != "" {
			snap.Raw["cli_version"] = ver.LatestVersion
		}
	}

	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" {
			snap.Raw["account_email"] = email
		}
		if planType := acct.ExtraData["plan_type"]; planType != "" {
			snap.Raw["plan_type"] = planType
		}
		if accountID := acct.ExtraData["account_id"]; accountID != "" {
			snap.Raw["account_id"] = accountID
		}
	}

	hasData := hasLocalData || hasLiveData
	if !hasData {
		if errors.Is(liveErr, errLiveUsageAuth) {
			snap.Status = core.StatusAuth
			snap.Message = "Codex auth required — run `codex login`"
		} else {
			snap.Status = core.StatusUnknown
			snap.Message = "No Codex usage data found"
		}
		return snap, nil
	}

	p.applyRateLimitStatus(&snap)

	switch {
	case hasLiveData && hasLocalData:
		snap.Message = "Codex live usage + local session data"
	case hasLiveData:
		snap.Message = "Codex live usage data"
	default:
		snap.Message = "Codex CLI session data"
	}

	return snap, nil
}

func (p *Provider) fetchLiveUsage(ctx context.Context, acct core.AccountConfig, configDir string, snap *core.QuotaSnapshot) (bool, error) {
	authPath := filepath.Join(configDir, "auth.json")
	if acct.ExtraData != nil && acct.ExtraData["auth_file"] != "" {
		authPath = acct.ExtraData["auth_file"]
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		return false, nil
	}

	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return false, nil
	}

	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return false, nil
	}

	baseURL := resolveChatGPTBaseURL(acct, configDir)
	usageURL := usageURLForBase(baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return false, fmt.Errorf("codex: creating live usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	req.Header.Set("Accept", "application/json")

	accountID := firstNonEmpty(auth.Tokens.AccountID, auth.AccountID)
	if accountID == "" && acct.ExtraData != nil {
		accountID = acct.ExtraData["account_id"]
	}
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	if cliVersion := snap.Raw["cli_version"]; cliVersion != "" {
		req.Header.Set("User-Agent", "codex-cli/"+cliVersion)
	} else {
		req.Header.Set("User-Agent", "codex-cli")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("codex: live usage request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, fmt.Errorf("%w: HTTP %d", errLiveUsageAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("codex: live usage HTTP %d: %s", resp.StatusCode, truncateForError(string(body), maxHTTPErrorBodySize))
	}

	var payload usagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("codex: parsing live usage response: %w", err)
	}

	applyUsagePayload(&payload, snap)
	snap.Raw["quota_api"] = "live"
	return true, nil
}

func applyUsagePayload(payload *usagePayload, snap *core.QuotaSnapshot) {
	if payload == nil {
		return
	}

	if payload.Email != "" {
		snap.Raw["account_email"] = payload.Email
	}
	if payload.AccountID != "" {
		snap.Raw["account_id"] = payload.AccountID
	}
	if payload.PlanType != "" {
		snap.Raw["plan_type"] = payload.PlanType
	}

	applyUsageLimitDetails(payload.RateLimit, "rate_limit_primary", "rate_limit_secondary", snap)
	applyUsageLimitDetails(payload.CodeReviewRateLimit, "rate_limit_code_review_primary", "rate_limit_code_review_secondary", snap)

	for _, extra := range payload.AdditionalRateLimits {
		limitID := sanitizeMetricName(firstNonEmpty(extra.MeteredFeature, extra.LimitName))
		if limitID == "" || limitID == "codex" {
			continue
		}

		primaryKey := "rate_limit_" + limitID + "_primary"
		secondaryKey := "rate_limit_" + limitID + "_secondary"
		applyUsageLimitDetails(extra.RateLimit, primaryKey, secondaryKey, snap)
		if extra.LimitName != "" {
			snap.Raw["rate_limit_"+limitID+"_name"] = extra.LimitName
		}
	}

	applyUsageCredits(payload.Credits, snap)
}

func applyUsageCredits(credits *usageCredits, snap *core.QuotaSnapshot) {
	if credits == nil {
		return
	}

	switch {
	case credits.Unlimited:
		snap.Raw["credits"] = "unlimited"
	case credits.HasCredits:
		snap.Raw["credits"] = "available"
		if formatted := formatCreditsBalance(credits.Balance); formatted != "" {
			snap.Raw["credit_balance"] = formatted
		}
	default:
		snap.Raw["credits"] = "none"
	}
}

func formatCreditsBalance(balance any) string {
	switch v := balance.(type) {
	case nil:
		return ""
	case string:
		if strings.TrimSpace(v) == "" {
			return ""
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return fmt.Sprintf("$%.2f", f)
		}
		return v
	case float64:
		return fmt.Sprintf("$%.2f", v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return fmt.Sprintf("$%.2f", f)
		}
	}
	return ""
}

func applyUsageLimitDetails(details *usageLimitDetails, primaryKey, secondaryKey string, snap *core.QuotaSnapshot) {
	if details == nil {
		return
	}
	applyUsageWindowMetric(details.PrimaryWindow, primaryKey, snap)
	applyUsageWindowMetric(details.SecondaryWindow, secondaryKey, snap)
}

func applyUsageWindowMetric(window *usageWindowInfo, key string, snap *core.QuotaSnapshot) {
	if window == nil || key == "" {
		return
	}

	limit := float64(100)
	used := clampPercent(window.UsedPercent)
	remaining := 100 - used
	windowLabel := formatWindow(secondsToMinutes(window.LimitWindowSeconds))

	snap.Metrics[key] = core.Metric{
		Limit:     &limit,
		Used:      &used,
		Remaining: &remaining,
		Unit:      "%",
		Window:    windowLabel,
	}

	if window.ResetAt > 0 {
		snap.Resets[key] = time.Unix(window.ResetAt, 0)
	}
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func secondsToMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func resolveChatGPTBaseURL(acct core.AccountConfig, configDir string) string {
	switch {
	case strings.TrimSpace(acct.BaseURL) != "":
		return normalizeChatGPTBaseURL(acct.BaseURL)
	case acct.ExtraData != nil && strings.TrimSpace(acct.ExtraData["chatgpt_base_url"]) != "":
		return normalizeChatGPTBaseURL(acct.ExtraData["chatgpt_base_url"])
	default:
		if fromConfig := readChatGPTBaseURLFromConfig(configDir); fromConfig != "" {
			return normalizeChatGPTBaseURL(fromConfig)
		}
	}
	return normalizeChatGPTBaseURL(defaultChatGPTBaseURL)
}

func readChatGPTBaseURLFromConfig(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}

	configPath := filepath.Join(configDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		if !strings.HasPrefix(line, "chatgpt_base_url") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		if val != "" {
			return val
		}
	}

	return ""
}

func normalizeChatGPTBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return defaultChatGPTBaseURL
	}
	if (strings.HasPrefix(baseURL, "https://chatgpt.com") || strings.HasPrefix(baseURL, "https://chat.openai.com")) &&
		!strings.Contains(baseURL, "/backend-api") {
		baseURL += "/backend-api"
	}
	return baseURL
}

func usageURLForBase(baseURL string) string {
	if strings.Contains(baseURL, "/backend-api") {
		return baseURL + "/wham/usage"
	}
	return baseURL + "/api/codex/usage"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateForError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func (p *Provider) readLatestSession(sessionsDir string, snap *core.QuotaSnapshot) error {
	latestFile, err := findLatestSessionFile(sessionsDir)
	if err != nil {
		return fmt.Errorf("finding latest session: %w", err)
	}

	snap.Raw["latest_session_file"] = filepath.Base(latestFile)

	lastPayload, err := findLastTokenCount(latestFile)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}

	if lastPayload == nil {
		return fmt.Errorf("no token_count events in latest session")
	}

	if lastPayload.Info != nil {
		info := lastPayload.Info
		total := info.TotalTokenUsage

		inputTokens := float64(total.InputTokens)
		snap.Metrics["session_input_tokens"] = core.Metric{
			Used:   &inputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		outputTokens := float64(total.OutputTokens)
		snap.Metrics["session_output_tokens"] = core.Metric{
			Used:   &outputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		cachedTokens := float64(total.CachedInputTokens)
		snap.Metrics["session_cached_tokens"] = core.Metric{
			Used:   &cachedTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if total.ReasoningOutputTokens > 0 {
			reasoning := float64(total.ReasoningOutputTokens)
			snap.Metrics["session_reasoning_tokens"] = core.Metric{
				Used:   &reasoning,
				Unit:   "tokens",
				Window: "session",
			}
		}

		totalTokens := float64(total.TotalTokens)
		snap.Metrics["session_total_tokens"] = core.Metric{
			Used:   &totalTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if info.ModelContextWindow > 0 {
			ctxWindow := float64(info.ModelContextWindow)
			ctxUsed := float64(total.InputTokens)
			snap.Metrics["context_window"] = core.Metric{
				Limit: &ctxWindow,
				Used:  &ctxUsed,
				Unit:  "tokens",
			}
		}
	}

	if lastPayload.RateLimits != nil {
		rl := lastPayload.RateLimits

		if rl.Primary != nil {
			limit := float64(100)
			used := rl.Primary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Primary.WindowMinutes)
			snap.Metrics["rate_limit_primary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Primary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Primary.ResetsAt, 0)
				snap.Resets["rate_limit_primary"] = resetTime
			}
		}

		if rl.Secondary != nil {
			limit := float64(100)
			used := rl.Secondary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Secondary.WindowMinutes)
			snap.Metrics["rate_limit_secondary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Secondary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Secondary.ResetsAt, 0)
				snap.Resets["rate_limit_secondary"] = resetTime
			}
		}

		if rl.Credits != nil {
			if rl.Credits.Unlimited {
				snap.Raw["credits"] = "unlimited"
			} else if rl.Credits.HasCredits {
				snap.Raw["credits"] = "available"
				if rl.Credits.Balance != nil {
					snap.Raw["credit_balance"] = fmt.Sprintf("$%.2f", *rl.Credits.Balance)
				}
			} else {
				snap.Raw["credits"] = "none"
			}
		}

		if rl.PlanType != nil {
			snap.Raw["plan_type"] = *rl.PlanType
		}
	}

	return nil
}

func (p *Provider) readSessionUsageBreakdowns(sessionsDir string, snap *core.QuotaSnapshot) error {
	modelTotals := make(map[string]tokenUsage)
	clientTotals := make(map[string]tokenUsage)
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	clientSessions := make(map[string]int)

	walkErr := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		defaultDay := dayFromSessionPath(path, sessionsDir)
		sessionClient := "Other"
		currentModel := "unknown"
		var previous tokenUsage
		var hasPrevious bool
		var countedSession bool

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 512*1024)
		scanner.Buffer(buf, maxScannerBufferSize)

		for scanner.Scan() {
			line := scanner.Bytes()
			if !bytes.Contains(line, []byte(`"type":"event_msg"`)) &&
				!bytes.Contains(line, []byte(`"type":"turn_context"`)) &&
				!bytes.Contains(line, []byte(`"type":"session_meta"`)) {
				continue
			}

			var event sessionEvent
			if err := json.Unmarshal(line, &event); err != nil {
				continue
			}

			switch event.Type {
			case "session_meta":
				var meta sessionMetaPayload
				if json.Unmarshal(event.Payload, &meta) == nil {
					sessionClient = classifyClient(meta.Source, meta.Originator)
					if meta.Model != "" {
						currentModel = meta.Model
					}
				}
			case "turn_context":
				var tc turnContextPayload
				if json.Unmarshal(event.Payload, &tc) == nil && strings.TrimSpace(tc.Model) != "" {
					currentModel = tc.Model
				}
			case "event_msg":
				var payload eventPayload
				if json.Unmarshal(event.Payload, &payload) != nil || payload.Type != "token_count" || payload.Info == nil {
					continue
				}

				total := payload.Info.TotalTokenUsage
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

				modelName := normalizeModelName(currentModel)
				clientName := normalizeClientName(sessionClient)
				day := dayFromTimestamp(event.Timestamp)
				if day == "" {
					day = defaultDay
				}

				addUsage(modelTotals, modelName, delta)
				addUsage(clientTotals, clientName, delta)
				addDailyUsage(modelDaily, modelName, day, float64(delta.TotalTokens))
				addDailyUsage(clientDaily, clientName, day, float64(delta.TotalTokens))

				if !countedSession {
					clientSessions[clientName]++
					countedSession = true
				}
			}
		}

		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walking session files: %w", walkErr)
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)

	return nil
}

func emitBreakdownMetrics(prefix string, totals map[string]tokenUsage, daily map[string]map[string]float64, snap *core.QuotaSnapshot) {
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
		if entry.Data.ReasoningOutputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningOutputTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			seriesKey := "tokens_" + prefix + "_" + sanitizeMetricName(entry.Name)
			snap.DailySeries[seriesKey] = mapToSortedTimePoints(byDay)
		}
	}

	rawKey := prefix + "_usage"
	snap.Raw[rawKey] = formatUsageSummary(entries, maxBreakdownRaw)
}

func emitClientSessionMetrics(clientSessions map[string]int, snap *core.QuotaSnapshot) {
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

func setUsageMetric(snap *core.QuotaSnapshot, key string, value float64) {
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
	current.ReasoningOutputTokens += delta.ReasoningOutputTokens
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
		InputTokens:           current.InputTokens - previous.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          current.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningOutputTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func classifyClient(source, originator string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	org := strings.ToLower(strings.TrimSpace(originator))

	switch {
	case strings.Contains(org, "desktop"):
		return "Desktop App"
	case strings.Contains(org, "exec") || src == "exec":
		return "Exec"
	case strings.Contains(org, "cli") || src == "cli":
		return "CLI"
	case src == "vscode" || src == "ide":
		return "IDE"
	case src == "":
		return "Other"
	default:
		return strings.ToUpper(src)
	}
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

func dayFromSessionPath(path, sessionsDir string) string {
	rel, err := filepath.Rel(sessionsDir, path)
	if err != nil {
		return ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}

	candidate := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return ""
	}
	return candidate
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

func (p *Provider) applyRateLimitStatus(snap *core.QuotaSnapshot) {
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError || snap.Status == core.StatusUnknown || snap.Status == core.StatusUnsupported {
		return
	}

	status := core.StatusOK
	for key, metric := range snap.Metrics {
		if !strings.HasPrefix(key, "rate_limit_") || metric.Unit != "%" || metric.Used == nil {
			continue
		}
		used := *metric.Used
		if used >= 100 {
			status = core.StatusLimited
			break
		}
		if used >= 90 {
			status = core.StatusNearLimit
		}
	}
	snap.Status = status
}

func findLatestSessionFile(sessionsDir string) (string, error) {
	var files []string

	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking sessions dir: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}

	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	return files[0], nil
}

func findLastTokenCount(path string) (*eventPayload, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lastPayload *eventPayload

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, maxScannerBufferSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"type":"event_msg"`)) {
			continue
		}

		var event sessionEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Type != "event_msg" {
			continue
		}

		var payload eventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}

		if payload.Type == "token_count" {
			lastPayload = &payload
		}
	}

	return lastPayload, scanner.Err()
}

func (p *Provider) readDailySessionCounts(sessionsDir string, snap *core.QuotaSnapshot) {
	dayCounts := make(map[string]int) // "2025-01-15" → count

	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 {
			dateStr := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
			if _, parseErr := time.Parse("2006-01-02", dateStr); parseErr == nil {
				dayCounts[dateStr]++
			}
		}
		return nil
	})

	if len(dayCounts) == 0 {
		return
	}

	dates := make([]string, 0, len(dayCounts))
	for d := range dayCounts {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	for _, d := range dates {
		snap.DailySeries["sessions"] = append(snap.DailySeries["sessions"], core.TimePoint{
			Date:  d,
			Value: float64(dayCounts[d]),
		})
	}
}

func formatWindow(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		if hours >= 24 {
			days := hours / 24
			leftover := hours % 24
			if leftover == 0 {
				return fmt.Sprintf("%dd", days)
			}
			return fmt.Sprintf("%dd%dh", days, leftover)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remaining)
}
