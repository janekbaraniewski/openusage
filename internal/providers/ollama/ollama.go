package ollama

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
	defaultLocalBaseURL = "http://127.0.0.1:11434"
	defaultCloudBaseURL = "https://ollama.com"
)

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "ollama",
			Info: core.ProviderInfo{
				Name:         "Ollama",
				Capabilities: []string{"local_api", "local_sqlite", "local_logs", "cloud_api", "per_model_breakdown"},
				DocURL:       "https://docs.ollama.com/api",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OLLAMA_API_KEY",
				DefaultAccountID: "ollama",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Ollama and keep local server running on http://127.0.0.1:11434.",
					"Optionally set OLLAMA_API_KEY for direct cloud account metadata.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	cloudOnly := strings.EqualFold(acct.Auth, string(core.ProviderAuthTypeAPIKey))
	hasData := false

	if !cloudOnly {
		localBaseURL := acct.BaseURL
		if localBaseURL == "" {
			localBaseURL = defaultLocalBaseURL
		}

		localOK, err := p.fetchLocalAPI(ctx, localBaseURL, &snap)
		if err != nil {
			snap.SetDiagnostic("local_api_error", err.Error())
		}
		hasData = hasData || localOK

		dbOK, err := p.fetchDesktopDB(ctx, acct, &snap)
		if err != nil {
			snap.SetDiagnostic("desktop_db_error", err.Error())
		}
		hasData = hasData || dbOK

		logOK, err := p.fetchServerLogs(acct, &snap)
		if err != nil {
			snap.SetDiagnostic("server_logs_error", err.Error())
		}
		hasData = hasData || logOK

		if err := p.fetchServerConfig(acct, &snap); err != nil {
			snap.SetDiagnostic("server_config_error", err.Error())
		}
	}

	apiKey := acct.ResolveAPIKey()
	if apiKey != "" {
		cloudHasData, authFailed, limited, err := p.fetchCloudAPI(ctx, acct, apiKey, &snap)
		if err != nil {
			if cloudOnly && !hasData {
				return core.UsageSnapshot{}, err
			}
			snap.SetDiagnostic("cloud_api_error", err.Error())
		}
		hasData = hasData || cloudHasData

		if limited {
			if !hasData || cloudOnly {
				snap.Status = core.StatusLimited
				snap.Message = "rate limited by Ollama cloud API (HTTP 429)"
				return snap, nil
			}
			snap.SetDiagnostic("cloud_rate_limited", "HTTP 429")
		}
		if authFailed {
			if !hasData || cloudOnly {
				snap.Status = core.StatusAuth
				snap.Message = "Ollama cloud auth failed (check OLLAMA_API_KEY)"
				return snap, nil
			}
			snap.SetDiagnostic("cloud_auth_failed", "check OLLAMA_API_KEY")
		}
	} else if cloudOnly {
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("env var %s not set", acct.APIKeyEnv)
		return snap, nil
	}

	switch {
	case hasData:
		snap.Status = core.StatusOK
		snap.Message = buildStatusMessage(snap)
	case cloudOnly:
		snap.Status = core.StatusAuth
		snap.Message = "cloud account configured but no API key found"
	default:
		snap.Status = core.StatusUnknown
		snap.Message = "No Ollama data found (local API, DB, logs, or cloud API)"
	}

	return snap, nil
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"messages_today", "requests_today", "models_total"} {
		metric, ok := snap.Metrics[key]
		if !ok || metric.Remaining == nil {
			continue
		}
		switch key {
		case "messages_today":
			parts = append(parts, fmt.Sprintf("%.0f msgs today", *metric.Remaining))
		case "requests_today":
			parts = append(parts, fmt.Sprintf("%.0f req today", *metric.Remaining))
		case "models_total":
			parts = append(parts, fmt.Sprintf("%.0f models", *metric.Remaining))
		}
	}
	if len(parts) == 0 {
		return "OK"
	}
	return strings.Join(parts, ", ")
}

func (p *Provider) fetchLocalAPI(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var hasData bool

	versionOK, err := p.fetchLocalVersion(ctx, baseURL, snap)
	if err != nil {
		return false, err
	}
	hasData = hasData || versionOK

	tagsOK, err := p.fetchLocalTags(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || tagsOK

	psOK, err := p.fetchLocalPS(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || psOK

	return hasData, nil
}

func (p *Provider) fetchLocalVersion(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp versionResponse
	code, headers, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/version", "", &resp)
	if err != nil {
		return false, fmt.Errorf("ollama: local version request failed: %w", err)
	}
	for k, v := range parsers.RedactHeaders(headers) {
		if strings.EqualFold(k, "X-Request-Id") || strings.EqualFold(k, "X-Build-Time") || strings.EqualFold(k, "X-Build-Commit") {
			snap.Raw["local_version_"+normalizeHeaderKey(k)] = v
		}
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("ollama: local version endpoint returned HTTP %d", code)
	}
	if resp.Version != "" {
		snap.SetAttribute("cli_version", resp.Version)
		return true, nil
	}
	return false, nil
}

func (p *Provider) fetchLocalTags(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp tagsResponse
	code, headers, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/tags", "", &resp)
	if err != nil {
		return false, fmt.Errorf("ollama: local tags request failed: %w", err)
	}
	for k, v := range parsers.RedactHeaders(headers) {
		if strings.EqualFold(k, "X-Request-Id") {
			snap.Raw["local_tags_"+normalizeHeaderKey(k)] = v
		}
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("ollama: local tags endpoint returned HTTP %d", code)
	}

	totalModels := float64(len(resp.Models))
	setValueMetric(snap, "models_total", totalModels, "models", "current")

	var localCount, cloudCount int
	var localBytes, cloudBytes int64
	for _, model := range resp.Models {
		if isCloudModel(model) {
			cloudCount++
			if model.Size > 0 {
				cloudBytes += model.Size
			}
			continue
		}

		localCount++
		if model.Size > 0 {
			localBytes += model.Size
		}
	}

	setValueMetric(snap, "models_local", float64(localCount), "models", "current")
	setValueMetric(snap, "models_cloud", float64(cloudCount), "models", "current")
	setValueMetric(snap, "model_storage_bytes", float64(localBytes), "bytes", "current")
	setValueMetric(snap, "cloud_model_stub_bytes", float64(cloudBytes), "bytes", "current")

	if len(resp.Models) > 0 {
		snap.Raw["models_top"] = summarizeModels(resp.Models, 6)
	}

	return true, nil
}

func (p *Provider) fetchLocalPS(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp processResponse
	code, _, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/ps", "", &resp)
	if err != nil {
		return false, fmt.Errorf("ollama: local process list request failed: %w", err)
	}
	if code != http.StatusOK {
		return false, fmt.Errorf("ollama: local process list endpoint returned HTTP %d", code)
	}

	setValueMetric(snap, "loaded_models", float64(len(resp.Models)), "models", "current")

	var loadedBytes int64
	var loadedVRAM int64
	maxContext := 0
	for _, m := range resp.Models {
		loadedBytes += m.Size
		loadedVRAM += m.SizeVRAM
		if m.ContextLength > maxContext {
			maxContext = m.ContextLength
		}
	}

	setValueMetric(snap, "loaded_model_bytes", float64(loadedBytes), "bytes", "current")
	setValueMetric(snap, "loaded_vram_bytes", float64(loadedVRAM), "bytes", "current")
	if maxContext > 0 {
		setValueMetric(snap, "context_window", float64(maxContext), "tokens", "current")
	}

	if len(resp.Models) > 0 {
		loadedNames := make([]string, 0, len(resp.Models))
		for _, m := range resp.Models {
			if m.Name == "" {
				continue
			}
			loadedNames = append(loadedNames, m.Name)
		}
		if len(loadedNames) > 0 {
			snap.Raw["loaded_models"] = strings.Join(loadedNames, ", ")
		}
	}

	return true, nil
}

func (p *Provider) fetchDesktopDB(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) (bool, error) {
	dbPath := resolveDesktopDBPath(acct)
	if dbPath == "" || !fileExists(dbPath) {
		return false, nil
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return false, fmt.Errorf("ollama: opening desktop db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return false, fmt.Errorf("ollama: pinging desktop db: %w", err)
	}

	snap.Raw["desktop_db_path"] = dbPath

	setCountMetric := func(key string, count int64, unit, window string) {
		setValueMetric(snap, key, float64(count), unit, window)
	}

	totalChats, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats`)
	if err == nil {
		setCountMetric("total_conversations", totalChats, "chats", "all-time")
	}

	totalMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages`)
	if err == nil {
		setCountMetric("total_messages", totalMessages, "messages", "all-time")
	}

	totalUserMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user'`)
	if err == nil {
		setCountMetric("total_user_messages", totalUserMessages, "messages", "all-time")
	}

	totalAssistantMessages, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'assistant'`)
	if err == nil {
		setCountMetric("total_assistant_messages", totalAssistantMessages, "messages", "all-time")
	}

	totalToolCalls, err := queryCount(ctx, db, `SELECT COUNT(*) FROM tool_calls`)
	if err == nil {
		setCountMetric("total_tool_calls", totalToolCalls, "calls", "all-time")
	}

	totalAttachments, err := queryCount(ctx, db, `SELECT COUNT(*) FROM attachments`)
	if err == nil {
		setCountMetric("total_attachments", totalAttachments, "attachments", "all-time")
	}

	sessionsToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE date(created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("sessions_today", sessionsToday, "sessions", "today")
	}

	messagesToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE date(created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("messages_today", messagesToday, "messages", "today")
	}

	userMessagesToday, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND date(created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("requests_today", userMessagesToday, "requests", "today")
	}

	toolCallsToday, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE date(m.created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("tool_calls_today", toolCallsToday, "calls", "today")
	}

	attachmentsToday, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM attachments a
		JOIN messages m ON a.message_id = m.id
		WHERE date(m.created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("attachments_today", attachmentsToday, "attachments", "today")
	}

	if err := populateModelUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_model_usage_error", err.Error())
	}

	if err := populateDailySeriesFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_daily_series_error", err.Error())
	}

	if err := populateSettingsFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_settings_error", err.Error())
	}

	if err := populateCachedUserFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_user_error", err.Error())
	}

	return true, nil
}

func (p *Provider) fetchServerLogs(acct core.AccountConfig, snap *core.UsageSnapshot) (bool, error) {
	logFiles := resolveServerLogFiles(acct)
	if len(logFiles) == 0 {
		return false, nil
	}

	now := time.Now()
	start24h := now.Add(-24 * time.Hour)
	start7d := now.Add(-7 * 24 * time.Hour)
	today := now.Format("2006-01-02")

	metrics := logMetrics{
		dailyRequests: make(map[string]float64),
	}

	for _, file := range logFiles {
		if err := parseLogFile(file, func(event ginLogEvent) {
			if !isInferencePath(event.Path) {
				return
			}

			dateKey := event.Timestamp.Format("2006-01-02")
			metrics.dailyRequests[dateKey]++

			if event.Timestamp.After(start7d) {
				metrics.requests7d++
			}
			if event.Timestamp.After(start24h) {
				metrics.recentRequests++
			}
			if dateKey == today {
				metrics.requestsToday++
				metrics.latencyTotal += event.Duration
				metrics.latencyCount++
				switch {
				case event.Status >= 500:
					metrics.errors5xxToday++
				case event.Status >= 400:
					metrics.errors4xxToday++
				}
				switch event.Path {
				case "/api/chat", "/v1/chat/completions", "/v1/responses":
					metrics.chatRequestsToday++
				case "/api/generate", "/v1/completions":
					metrics.generateRequestsToday++
				}
			}
		}); err != nil {
			return false, fmt.Errorf("ollama: parsing log %s: %w", file, err)
		}
	}

	if len(metrics.dailyRequests) == 0 {
		return false, nil
	}

	setValueMetric(snap, "requests_today", float64(metrics.requestsToday), "requests", "today")
	setValueMetric(snap, "recent_requests", float64(metrics.recentRequests), "requests", "24h")
	setValueMetric(snap, "requests_7d", float64(metrics.requests7d), "requests", "7d")
	setValueMetric(snap, "chat_requests_today", float64(metrics.chatRequestsToday), "requests", "today")
	setValueMetric(snap, "generate_requests_today", float64(metrics.generateRequestsToday), "requests", "today")
	setValueMetric(snap, "http_4xx_today", float64(metrics.errors4xxToday), "responses", "today")
	setValueMetric(snap, "http_5xx_today", float64(metrics.errors5xxToday), "responses", "today")
	if metrics.latencyCount > 0 {
		avgMs := float64(metrics.latencyTotal.Microseconds()) / 1000 / float64(metrics.latencyCount)
		setValueMetric(snap, "avg_latency_ms_today", avgMs, "ms", "today")
	}

	snap.DailySeries["requests"] = mapToSortedTimePoints(metrics.dailyRequests)
	return true, nil
}

func (p *Provider) fetchServerConfig(acct core.AccountConfig, snap *core.UsageSnapshot) error {
	path := resolveServerConfigPath(acct)
	if path == "" || !fileExists(path) {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ollama: reading server config: %w", err)
	}

	var cfg struct {
		DisableOllamaCloud bool `json:"disable_ollama_cloud"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("ollama: parsing server config: %w", err)
	}

	snap.SetAttribute("cloud_disabled", strconv.FormatBool(cfg.DisableOllamaCloud))
	snap.Raw["server_config_path"] = path
	return nil
}

func (p *Provider) fetchCloudAPI(ctx context.Context, acct core.AccountConfig, apiKey string, snap *core.UsageSnapshot) (hasData, authFailed, limited bool, err error) {
	cloudBaseURL := resolveCloudBaseURL(acct)

	var me cloudUserResponse
	status, headers, reqErr := doJSONRequest(ctx, http.MethodPost, cloudBaseURL+"/api/me", apiKey, &me)
	if reqErr != nil {
		return false, false, false, fmt.Errorf("ollama: cloud account request failed: %w", reqErr)
	}

	for k, v := range parsers.RedactHeaders(headers, "authorization") {
		if strings.EqualFold(k, "X-Request-Id") {
			snap.Raw["cloud_me_"+normalizeHeaderKey(k)] = v
		}
	}

	switch status {
	case http.StatusOK:
		snap.SetAttribute("auth_type", "api_key")
		if me.ID != "" {
			snap.SetAttribute("account_id", me.ID)
		}
		if me.Email != "" {
			snap.SetAttribute("account_email", me.Email)
		}
		if me.Name != "" {
			snap.SetAttribute("account_name", me.Name)
		}
		if me.Plan != "" {
			snap.SetAttribute("plan_name", me.Plan)
		}
		if me.SubscriptionPeriodStart.Time != "" && me.SubscriptionPeriodStart.Valid {
			snap.SetAttribute("billing_cycle_start", me.SubscriptionPeriodStart.Time)
		}
		if me.SubscriptionPeriodEnd.Time != "" && me.SubscriptionPeriodEnd.Valid {
			snap.SetAttribute("billing_cycle_end", me.SubscriptionPeriodEnd.Time)
		}
		hasData = true
	case http.StatusUnauthorized, http.StatusForbidden:
		authFailed = true
	case http.StatusTooManyRequests:
		limited = true
	default:
		snap.SetDiagnostic("cloud_me_status", fmt.Sprintf("HTTP %d", status))
	}

	var tags tagsResponse
	tagsStatus, _, tagsErr := doJSONRequest(ctx, http.MethodGet, cloudBaseURL+"/api/tags", apiKey, &tags)
	if tagsErr != nil {
		if !hasData {
			return hasData, authFailed, limited, fmt.Errorf("ollama: cloud tags request failed: %w", tagsErr)
		}
		snap.SetDiagnostic("cloud_tags_error", tagsErr.Error())
		return hasData, authFailed, limited, nil
	}

	switch tagsStatus {
	case http.StatusOK:
		setValueMetric(snap, "cloud_catalog_models", float64(len(tags.Models)), "models", "current")
		hasData = true
	case http.StatusUnauthorized, http.StatusForbidden:
		authFailed = true
	case http.StatusTooManyRequests:
		limited = true
	default:
		snap.SetDiagnostic("cloud_tags_status", fmt.Sprintf("HTTP %d", tagsStatus))
	}

	return hasData, authFailed, limited, nil
}

func resolveCloudBaseURL(acct core.AccountConfig) string {
	if acct.ExtraData != nil {
		if v := strings.TrimSpace(acct.ExtraData["cloud_base_url"]); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	if strings.HasPrefix(strings.ToLower(acct.BaseURL), "https://") && strings.Contains(strings.ToLower(acct.BaseURL), "ollama.com") {
		return strings.TrimRight(acct.BaseURL, "/")
	}
	return defaultCloudBaseURL
}

func resolveDesktopDBPath(acct core.AccountConfig) string {
	if acct.ExtraData != nil {
		for _, key := range []string{"db_path", "app_db"} {
			if v := strings.TrimSpace(acct.ExtraData[key]); v != "" {
				return v
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Ollama", "db.sqlite")
	case "linux":
		candidates := []string{
			filepath.Join(home, ".local", "share", "Ollama", "db.sqlite"),
			filepath.Join(home, ".config", "Ollama", "db.sqlite"),
		}
		for _, c := range candidates {
			if fileExists(c) {
				return c
			}
		}
		return candidates[0]
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData != "" {
			return filepath.Join(appData, "Ollama", "db.sqlite")
		}
		return filepath.Join(home, "AppData", "Roaming", "Ollama", "db.sqlite")
	default:
		return filepath.Join(home, ".ollama", "db.sqlite")
	}
}

func resolveServerConfigPath(acct core.AccountConfig) string {
	if acct.ExtraData != nil {
		if v := strings.TrimSpace(acct.ExtraData["server_config"]); v != "" {
			return v
		}
		if configDir := strings.TrimSpace(acct.ExtraData["config_dir"]); configDir != "" {
			return filepath.Join(configDir, "server.json")
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ollama", "server.json")
}

func resolveServerLogFiles(acct core.AccountConfig) []string {
	logDir := ""
	if acct.ExtraData != nil {
		logDir = strings.TrimSpace(acct.ExtraData["logs_dir"])
		if logDir == "" {
			if configDir := strings.TrimSpace(acct.ExtraData["config_dir"]); configDir != "" {
				logDir = filepath.Join(configDir, "logs")
			}
		}
	}
	if logDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		logDir = filepath.Join(home, ".ollama", "logs")
	}

	pattern := filepath.Join(logDir, "server*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(files)
	return files
}

func queryCount(ctx context.Context, db *sql.DB, query string) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func populateSettingsFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	var selectedModel sql.NullString
	var contextLength sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT selected_model, context_length FROM settings LIMIT 1`).Scan(&selectedModel, &contextLength)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if selectedModel.Valid && strings.TrimSpace(selectedModel.String) != "" {
		snap.SetAttribute("selected_model", selectedModel.String)
	}
	if contextLength.Valid && contextLength.Int64 > 0 {
		setValueMetric(snap, "configured_context_length", float64(contextLength.Int64), "tokens", "current")
	}
	return nil
}

func populateCachedUserFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	var name sql.NullString
	var email sql.NullString
	var plan sql.NullString
	var cachedAt sql.NullString

	err := db.QueryRowContext(ctx, `SELECT name, email, plan, cached_at FROM users ORDER BY cached_at DESC LIMIT 1`).Scan(&name, &email, &plan, &cachedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if name.Valid && strings.TrimSpace(name.String) != "" {
		snap.SetAttribute("account_name", name.String)
	}
	if email.Valid && strings.TrimSpace(email.String) != "" {
		snap.SetAttribute("account_email", email.String)
	}
	if plan.Valid && strings.TrimSpace(plan.String) != "" {
		snap.SetAttribute("plan_name", plan.String)
	}
	if cachedAt.Valid && strings.TrimSpace(cachedAt.String) != "" {
		snap.SetAttribute("account_cached_at", cachedAt.String)
	}
	return nil
}

func populateModelUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	rows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*) FROM messages WHERE model_name IS NOT NULL AND trim(model_name) != '' GROUP BY model_name ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var top []string
	for rows.Next() {
		var model string
		var count float64
		if err := rows.Scan(&model, &count); err != nil {
			return err
		}
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}

		metricKey := "model_" + sanitizeMetricPart(model) + "_requests"
		setValueMetric(snap, metricKey, count, "requests", "all-time")

		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "sqlite",
			Window:     "all-time",
			Requests:   core.Float64Ptr(count),
		}
		rec.SetDimension("provider", "ollama")
		core.AppendModelUsageRecord(snap, rec)

		if len(top) < 6 {
			top = append(top, fmt.Sprintf("%s=%.0f", model, count))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(top) > 0 {
		snap.Raw["models_usage_top"] = strings.Join(top, ", ")
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(created_at), model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY date(created_at), model_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perModelDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var date string
		var model string
		var count float64
		if err := perDayRows.Scan(&date, &model, &count); err != nil {
			return err
		}
		model = strings.TrimSpace(model)
		date = strings.TrimSpace(date)
		if model == "" || date == "" {
			continue
		}
		if perModelDaily[model] == nil {
			perModelDaily[model] = make(map[string]float64)
		}
		perModelDaily[model][date] = count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for model, byDate := range perModelDaily {
		seriesKey := "requests_model_" + sanitizeMetricPart(model)
		snap.DailySeries[seriesKey] = mapToSortedTimePoints(byDate)
	}

	return nil
}

func populateDailySeriesFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	dailyQueries := []struct {
		key   string
		query string
	}{
		{"messages", `SELECT date(created_at), COUNT(*) FROM messages GROUP BY date(created_at)`},
		{"sessions", `SELECT date(created_at), COUNT(*) FROM chats GROUP BY date(created_at)`},
		{"tool_calls", `SELECT date(m.created_at), COUNT(*)
			FROM tool_calls tc
			JOIN messages m ON tc.message_id = m.id
			GROUP BY date(m.created_at)`},
		{"requests_user", `SELECT date(created_at), COUNT(*) FROM messages WHERE role = 'user' GROUP BY date(created_at)`},
	}

	for _, dq := range dailyQueries {
		rows, err := db.QueryContext(ctx, dq.query)
		if err != nil {
			continue
		}

		byDate := make(map[string]float64)
		for rows.Next() {
			var date string
			var count float64
			if err := rows.Scan(&date, &count); err != nil {
				rows.Close()
				return err
			}
			if strings.TrimSpace(date) == "" {
				continue
			}
			byDate[date] = count
		}
		rows.Close()
		if len(byDate) > 0 {
			snap.DailySeries[dq.key] = mapToSortedTimePoints(byDate)
		}
	}

	return nil
}

func parseLogFile(path string, onEvent func(ginLogEvent)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxLogLine = 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLogLine)

	for scanner.Scan() {
		line := scanner.Text()
		event, ok := parseGINLogLine(line)
		if !ok {
			continue
		}
		onEvent(event)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func parseGINLogLine(line string) (ginLogEvent, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[GIN]") {
		return ginLogEvent{}, false
	}

	parts := strings.Split(line, "|")
	if len(parts) < 5 {
		return ginLogEvent{}, false
	}

	left := strings.TrimSpace(strings.TrimPrefix(parts[0], "[GIN]"))
	leftParts := strings.Split(left, " - ")
	if len(leftParts) != 2 {
		return ginLogEvent{}, false
	}

	timestamp, err := time.ParseInLocation("2006/01/02 15:04:05", strings.TrimSpace(leftParts[0])+" "+strings.TrimSpace(leftParts[1]), time.Local)
	if err != nil {
		return ginLogEvent{}, false
	}

	status, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return ginLogEvent{}, false
	}

	durationText := strings.TrimSpace(parts[2])
	durationText = strings.ReplaceAll(durationText, "µ", "u")
	duration, err := time.ParseDuration(durationText)
	if err != nil {
		return ginLogEvent{}, false
	}

	methodPath := strings.TrimSpace(parts[4])
	methodPathParts := strings.Fields(methodPath)
	if len(methodPathParts) < 2 {
		return ginLogEvent{}, false
	}

	method := strings.TrimSpace(methodPathParts[0])
	path := strings.Trim(strings.TrimSpace(methodPathParts[1]), `"`)
	if method == "" || path == "" {
		return ginLogEvent{}, false
	}

	return ginLogEvent{
		Timestamp: timestamp,
		Status:    status,
		Duration:  duration,
		Method:    method,
		Path:      path,
	}, true
}

func isInferencePath(path string) bool {
	switch path {
	case "/api/chat", "/api/generate", "/api/embed", "/api/embeddings",
		"/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/embeddings":
		return true
	default:
		return false
	}
}

func doJSONRequest(ctx context.Context, method, url, apiKey string, out any) (int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, err
	}
	if len(body) == 0 {
		return resp.StatusCode, resp.Header, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return resp.StatusCode, resp.Header, err
	}
	return resp.StatusCode, resp.Header, nil
}

func mapToSortedTimePoints(values map[string]float64) []core.TimePoint {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	series := make([]core.TimePoint, 0, len(keys))
	for _, key := range keys {
		series = append(series, core.TimePoint{Date: key, Value: values[key]})
	}
	return series
}

func sanitizeMetricPart(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = nonAlnumRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "unknown"
	}
	return s
}

func setValueMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	snap.Metrics[key] = core.Metric{
		Remaining: core.Float64Ptr(value),
		Unit:      unit,
		Window:    window,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func summarizeModels(models []tagModel, limit int) string {
	if len(models) == 0 || limit <= 0 {
		return ""
	}
	out := make([]string, 0, limit)
	for i := 0; i < len(models) && i < limit; i++ {
		name := strings.TrimSpace(models[i].Name)
		if name == "" {
			name = strings.TrimSpace(models[i].Model)
		}
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}

func normalizeHeaderKey(k string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(k)), "-", "_")
}

func isCloudModel(model tagModel) bool {
	name := strings.ToLower(strings.TrimSpace(model.Name))
	mdl := strings.ToLower(strings.TrimSpace(model.Model))
	if strings.HasSuffix(name, ":cloud") || strings.HasSuffix(mdl, ":cloud") {
		return true
	}
	if strings.TrimSpace(model.RemoteHost) != "" || strings.TrimSpace(model.RemoteModel) != "" {
		return true
	}
	return false
}

type versionResponse struct {
	Version string `json:"version"`
}

type modelDetails struct {
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

type tagModel struct {
	Name        string       `json:"name"`
	Model       string       `json:"model"`
	RemoteModel string       `json:"remote_model"`
	RemoteHost  string       `json:"remote_host"`
	ModifiedAt  string       `json:"modified_at"`
	Size        int64        `json:"size"`
	Digest      string       `json:"digest"`
	Details     modelDetails `json:"details"`
}

type tagsResponse struct {
	Models []tagModel `json:"models"`
}

type processModel struct {
	Name          string       `json:"name"`
	Model         string       `json:"model"`
	Size          int64        `json:"size"`
	SizeVRAM      int64        `json:"size_vram"`
	ContextLength int          `json:"context_length"`
	ExpiresAt     string       `json:"expires_at"`
	Digest        string       `json:"digest"`
	Details       modelDetails `json:"details"`
}

type processResponse struct {
	Models []processModel `json:"models"`
}

type cloudUserResponse struct {
	ID                      string          `json:"id"`
	Email                   string          `json:"email"`
	Name                    string          `json:"name"`
	Plan                    string          `json:"plan"`
	SubscriptionPeriodStart cloudNullTime   `json:"subscriptionperiodstart"`
	SubscriptionPeriodEnd   cloudNullTime   `json:"subscriptionperiodend"`
	RawPeriodStart          json.RawMessage `json:"SubscriptionPeriodStart"`
	RawPeriodEnd            json.RawMessage `json:"SubscriptionPeriodEnd"`
}

type cloudNullTime struct {
	Time  string `json:"Time"`
	Valid bool   `json:"Valid"`
}

type ginLogEvent struct {
	Timestamp time.Time
	Status    int
	Duration  time.Duration
	Method    string
	Path      string
}

type logMetrics struct {
	dailyRequests map[string]float64

	requestsToday  int
	recentRequests int
	requests7d     int

	chatRequestsToday     int
	generateRequestsToday int
	errors4xxToday        int
	errors5xxToday        int
	latencyTotal          time.Duration
	latencyCount          int
}
