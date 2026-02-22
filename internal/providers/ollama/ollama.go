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
	"net/url"
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
var settingsUsageRe = regexp.MustCompile(`(?is)(Session usage|Weekly usage)\s*</span>\s*<span[^>]*>\s*([0-9]+(?:\.[0-9]+)?)%\s*used\s*</span>`)
var settingsResetRe = regexp.MustCompile(`(?is)(Session usage|Weekly usage).*?data-time="([^"]+)"`)

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

	finalizeUsageWindows(&snap)

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
	parts := make([]string, 0, 5)
	for _, key := range []string{"messages_today", "requests_today", "requests_5h", "requests_1d", "models_total"} {
		metric, ok := snap.Metrics[key]
		if !ok || metric.Remaining == nil {
			continue
		}
		switch key {
		case "messages_today":
			parts = append(parts, fmt.Sprintf("%.0f msgs today", *metric.Remaining))
		case "requests_today":
			parts = append(parts, fmt.Sprintf("%.0f req today", *metric.Remaining))
		case "requests_5h":
			parts = append(parts, fmt.Sprintf("%.0f req 5h", *metric.Remaining))
		case "requests_1d":
			parts = append(parts, fmt.Sprintf("%.0f req 1d", *metric.Remaining))
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

	statusOK, err := p.fetchLocalStatus(ctx, baseURL, snap)
	if err != nil {
		return false, err
	}
	hasData = hasData || statusOK

	versionOK, err := p.fetchLocalVersion(ctx, baseURL, snap)
	if err != nil {
		return false, err
	}
	hasData = hasData || versionOK

	meOK, err := p.fetchLocalMe(ctx, baseURL, snap)
	if err != nil {
		return hasData, err
	}
	hasData = hasData || meOK

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

func (p *Provider) fetchLocalStatus(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp map[string]any
	code, _, err := doJSONRequest(ctx, http.MethodGet, baseURL+"/api/status", "", &resp)
	if err != nil {
		return false, nil
	}
	if code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
		return false, nil
	}
	if code != http.StatusOK {
		return false, nil
	}

	cloud := anyMapCaseInsensitive(resp, "cloud")
	if len(cloud) == 0 {
		return false, nil
	}

	var hasData bool
	if disabled, ok := anyBoolCaseInsensitive(cloud, "disabled"); ok {
		snap.SetAttribute("cloud_disabled", strconv.FormatBool(disabled))
		hasData = true
	}
	if source := anyStringCaseInsensitive(cloud, "source"); source != "" {
		snap.SetAttribute("cloud_source", source)
		hasData = true
	}
	return hasData, nil
}

func (p *Provider) fetchLocalMe(ctx context.Context, baseURL string, snap *core.UsageSnapshot) (bool, error) {
	var resp map[string]any
	code, _, err := doJSONRequest(ctx, http.MethodPost, baseURL+"/api/me", "", &resp)
	if err != nil {
		return false, nil
	}

	switch code {
	case http.StatusOK:
		return applyCloudUserPayload(resp, snap), nil
	case http.StatusUnauthorized, http.StatusForbidden:
		if signinURL := anyStringCaseInsensitive(resp, "signin_url", "sign_in_url"); signinURL != "" {
			snap.SetAttribute("signin_url", signinURL)
			return true, nil
		}
		return false, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false, nil
	default:
		snap.SetDiagnostic("local_me_status", fmt.Sprintf("HTTP %d", code))
		return false, nil
	}
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
			name := normalizeModelName(m.Name)
			if name == "" {
				continue
			}
			loadedNames = append(loadedNames, name)
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

	sessions5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("sessions_5h", sessions5h, "sessions", "5h")
	}

	sessions1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM chats WHERE datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("sessions_1d", sessions1d, "sessions", "1d")
	}

	messages5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("messages_5h", messages5h, "messages", "5h")
	}

	messages1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("messages_1d", messages1d, "messages", "1d")
	}

	requests5h, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND datetime(created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("requests_5h", requests5h, "requests", "5h")
	}

	requests1d, err := queryCount(ctx, db, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND datetime(created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("requests_1d", requests1d, "requests", "1d")
	}

	toolCallsToday, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE date(m.created_at, 'localtime') = date('now', 'localtime')`)
	if err == nil {
		setCountMetric("tool_calls_today", toolCallsToday, "calls", "today")
	}

	toolCalls5h, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE datetime(m.created_at) >= datetime('now', '-5 hours')`)
	if err == nil {
		setCountMetric("tool_calls_5h", toolCalls5h, "calls", "5h")
	}

	toolCalls1d, err := queryCount(ctx, db, `SELECT COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE datetime(m.created_at) >= datetime('now', '-24 hours')`)
	if err == nil {
		setCountMetric("tool_calls_1d", toolCalls1d, "calls", "1d")
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

	if err := populateEstimatedTokenUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_token_estimate_error", err.Error())
	}

	if err := populateSourceUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_source_usage_error", err.Error())
	}

	if err := populateToolUsageFromDB(ctx, db, snap); err != nil {
		snap.SetDiagnostic("desktop_tool_usage_error", err.Error())
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
	start5h := now.Add(-5 * time.Hour)
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
			if event.Timestamp.After(start5h) {
				metrics.requests5h++
				metrics.latencyTotal5h += event.Duration
				metrics.latencyCount5h++
				switch {
				case event.Status >= 500:
					metrics.errors5xx5h++
				case event.Status >= 400:
					metrics.errors4xx5h++
				}
				switch event.Path {
				case "/api/chat", "/v1/chat/completions", "/v1/responses", "/v1/messages":
					metrics.chatRequests5h++
				case "/api/generate", "/v1/completions":
					metrics.generateRequests5h++
				}
			}
			if event.Timestamp.After(start24h) {
				metrics.recentRequests++
				metrics.requests1d++
				metrics.latencyTotal1d += event.Duration
				metrics.latencyCount1d++
				switch {
				case event.Status >= 500:
					metrics.errors5xx1d++
				case event.Status >= 400:
					metrics.errors4xx1d++
				}
				switch event.Path {
				case "/api/chat", "/v1/chat/completions", "/v1/responses", "/v1/messages":
					metrics.chatRequests1d++
				case "/api/generate", "/v1/completions":
					metrics.generateRequests1d++
				}
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
				case "/api/chat", "/v1/chat/completions", "/v1/responses", "/v1/messages":
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
	setValueMetric(snap, "requests_5h", float64(metrics.requests5h), "requests", "5h")
	setValueMetric(snap, "requests_1d", float64(metrics.requests1d), "requests", "1d")
	setValueMetric(snap, "recent_requests", float64(metrics.recentRequests), "requests", "24h")
	setValueMetric(snap, "requests_7d", float64(metrics.requests7d), "requests", "7d")
	setValueMetric(snap, "chat_requests_5h", float64(metrics.chatRequests5h), "requests", "5h")
	setValueMetric(snap, "generate_requests_5h", float64(metrics.generateRequests5h), "requests", "5h")
	setValueMetric(snap, "chat_requests_1d", float64(metrics.chatRequests1d), "requests", "1d")
	setValueMetric(snap, "generate_requests_1d", float64(metrics.generateRequests1d), "requests", "1d")
	setValueMetric(snap, "chat_requests_today", float64(metrics.chatRequestsToday), "requests", "today")
	setValueMetric(snap, "generate_requests_today", float64(metrics.generateRequestsToday), "requests", "today")
	setValueMetric(snap, "http_4xx_5h", float64(metrics.errors4xx5h), "responses", "5h")
	setValueMetric(snap, "http_5xx_5h", float64(metrics.errors5xx5h), "responses", "5h")
	setValueMetric(snap, "http_4xx_1d", float64(metrics.errors4xx1d), "responses", "1d")
	setValueMetric(snap, "http_5xx_1d", float64(metrics.errors5xx1d), "responses", "1d")
	setValueMetric(snap, "http_4xx_today", float64(metrics.errors4xxToday), "responses", "today")
	setValueMetric(snap, "http_5xx_today", float64(metrics.errors5xxToday), "responses", "today")
	if metrics.latencyCount5h > 0 {
		avgMs := float64(metrics.latencyTotal5h.Microseconds()) / 1000 / float64(metrics.latencyCount5h)
		setValueMetric(snap, "avg_latency_ms_5h", avgMs, "ms", "5h")
	}
	if metrics.latencyCount1d > 0 {
		avgMs := float64(metrics.latencyTotal1d.Microseconds()) / 1000 / float64(metrics.latencyCount1d)
		setValueMetric(snap, "avg_latency_ms_1d", avgMs, "ms", "1d")
	}
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

	var me map[string]any
	status, headers, reqErr := doJSONRequest(ctx, http.MethodPost, cloudEndpointURL(cloudBaseURL, "/api/me"), apiKey, &me)
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
		if applyCloudUserPayload(me, snap) {
			hasData = true
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		authFailed = true
	case http.StatusTooManyRequests:
		limited = true
	default:
		snap.SetDiagnostic("cloud_me_status", fmt.Sprintf("HTTP %d", status))
	}

	var tags tagsResponse
	tagsStatus, _, tagsErr := doJSONRequest(ctx, http.MethodGet, cloudEndpointURL(cloudBaseURL, "/api/tags"), apiKey, &tags)
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

	if _, ok := snap.Metrics["usage_five_hour"]; !ok {
		if parsed, parseErr := fetchCloudUsageFromSettingsPage(ctx, cloudBaseURL, apiKey, acct, snap); parseErr != nil {
			snap.SetDiagnostic("cloud_usage_settings_error", parseErr.Error())
		} else if parsed {
			hasData = true
		}
	}

	return hasData, authFailed, limited, nil
}

func applyCloudUserPayload(payload map[string]any, snap *core.UsageSnapshot) bool {
	if len(payload) == 0 {
		return false
	}

	var hasData bool

	if id := anyStringCaseInsensitive(payload, "id", "ID"); id != "" {
		snap.SetAttribute("account_id", id)
		hasData = true
	}
	if email := anyStringCaseInsensitive(payload, "email", "Email"); email != "" {
		snap.SetAttribute("account_email", email)
		hasData = true
	}
	if name := anyStringCaseInsensitive(payload, "name", "Name"); name != "" {
		snap.SetAttribute("account_name", name)
		hasData = true
	}
	if plan := anyStringCaseInsensitive(payload, "plan", "Plan"); plan != "" {
		snap.SetAttribute("plan_name", plan)
		hasData = true
	}

	if customerID := anyNullStringCaseInsensitive(payload, "customerid", "customer_id", "CustomerID"); customerID != "" {
		snap.SetAttribute("customer_id", customerID)
	}
	if subscriptionID := anyNullStringCaseInsensitive(payload, "subscriptionid", "subscription_id", "SubscriptionID"); subscriptionID != "" {
		snap.SetAttribute("subscription_id", subscriptionID)
	}
	if workOSUserID := anyNullStringCaseInsensitive(payload, "workosuserid", "workos_user_id", "WorkOSUserID"); workOSUserID != "" {
		snap.SetAttribute("workos_user_id", workOSUserID)
	}

	if billingStart, ok := anyNullTimeCaseInsensitive(payload, "subscriptionperiodstart", "subscription_period_start", "SubscriptionPeriodStart"); ok {
		snap.SetAttribute("billing_cycle_start", billingStart.Format(time.RFC3339))
	}
	if billingEnd, ok := anyNullTimeCaseInsensitive(payload, "subscriptionperiodend", "subscription_period_end", "SubscriptionPeriodEnd"); ok {
		snap.SetAttribute("billing_cycle_end", billingEnd.Format(time.RFC3339))
	}

	if extractCloudUsageWindows(payload, snap) {
		hasData = true
	}

	return hasData
}

func extractCloudUsageWindows(payload map[string]any, snap *core.UsageSnapshot) bool {
	var found bool

	sessionKeys := []string{
		"session_usage", "sessionusage", "usage_5h", "usagefivehour", "five_hour_usage", "fivehourusage",
	}
	if metric, resetAt, ok := findUsageWindow(payload, sessionKeys, "5h"); ok {
		snap.Metrics["usage_five_hour"] = metric
		if !resetAt.IsZero() {
			snap.Resets["usage_five_hour"] = resetAt
			snap.SetAttribute("block_end", resetAt.Format(time.RFC3339))
			if metric.Window == "5h" {
				start := resetAt.Add(-5 * time.Hour)
				snap.SetAttribute("block_start", start.Format(time.RFC3339))
			}
		}
		found = true
	}

	dayKeys := []string{
		"weekly_usage", "weeklyusage", "usage_1d", "usageoneday", "one_day_usage", "daily_usage", "dailyusage",
	}
	if metric, resetAt, ok := findUsageWindow(payload, dayKeys, "1d"); ok {
		snap.Metrics["usage_weekly"] = core.Metric{
			Limit:     metric.Limit,
			Remaining: metric.Remaining,
			Used:      metric.Used,
			Unit:      metric.Unit,
			Window:    "1w",
		}
		// Backward-compatible alias for existing widgets/config.
		snap.Metrics["usage_one_day"] = metric
		if !resetAt.IsZero() {
			snap.Resets["usage_weekly"] = resetAt
			snap.Resets["usage_one_day"] = resetAt
		}
		found = true
	}

	return found
}

func findUsageWindow(payload map[string]any, keys []string, fallbackWindow string) (core.Metric, time.Time, bool) {
	sources := []map[string]any{
		payload,
		anyMapCaseInsensitive(payload, "usage"),
		anyMapCaseInsensitive(payload, "cloud_usage"),
		anyMapCaseInsensitive(payload, "quota"),
	}

	for _, src := range sources {
		if len(src) == 0 {
			continue
		}
		for _, key := range keys {
			v, ok := anyValueCaseInsensitive(src, key)
			if !ok {
				continue
			}
			if metric, resetAt, ok := parseUsageWindowValue(v, fallbackWindow); ok {
				return metric, resetAt, true
			}
		}
	}

	return core.Metric{}, time.Time{}, false
}

func parseUsageWindowValue(v any, fallbackWindow string) (core.Metric, time.Time, bool) {
	if pct, ok := anyFloat(v); ok {
		return core.Metric{
			Used:   core.Float64Ptr(pct),
			Unit:   "%",
			Window: fallbackWindow,
		}, time.Time{}, true
	}

	switch raw := v.(type) {
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return core.Metric{
				Used:   core.Float64Ptr(f),
				Unit:   "%",
				Window: fallbackWindow,
			}, time.Time{}, true
		}
	case map[string]any:
		var metric core.Metric
		metric.Window = fallbackWindow
		metric.Unit = anyStringCaseInsensitive(raw, "unit")
		if metric.Unit == "" {
			metric.Unit = "%"
		}

		if window := anyStringCaseInsensitive(raw, "window"); window != "" {
			metric.Window = strings.TrimSpace(window)
		}

		if used, ok := anyFloatCaseInsensitive(raw, "used", "usage", "value"); ok {
			metric.Used = core.Float64Ptr(used)
		}
		if limit, ok := anyFloatCaseInsensitive(raw, "limit", "max"); ok {
			metric.Limit = core.Float64Ptr(limit)
		}
		if remaining, ok := anyFloatCaseInsensitive(raw, "remaining", "left"); ok {
			metric.Remaining = core.Float64Ptr(remaining)
		}
		if pct, ok := anyFloatCaseInsensitive(raw, "percent", "pct", "used_percent", "usage_percent"); ok {
			metric.Unit = "%"
			metric.Used = core.Float64Ptr(pct)
			metric.Limit = nil
			metric.Remaining = nil
		}

		var resetAt time.Time
		if resetRaw := anyStringCaseInsensitive(raw, "reset_at", "resets_at", "reset_time", "reset"); resetRaw != "" {
			if t, ok := parseAnyTime(resetRaw); ok {
				resetAt = t
			}
		}
		if resetAt.IsZero() {
			if seconds, ok := anyFloatCaseInsensitive(raw, "reset_in", "reset_in_seconds", "resets_in", "seconds_to_reset"); ok && seconds > 0 {
				resetAt = time.Now().Add(time.Duration(seconds * float64(time.Second)))
			}
		}

		if metric.Used != nil || metric.Limit != nil || metric.Remaining != nil {
			return metric, resetAt, true
		}
	}

	return core.Metric{}, time.Time{}, false
}

func finalizeUsageWindows(snap *core.UsageSnapshot) {
	now := time.Now().In(time.Local)
	blockStart, blockEnd := currentFiveHourBlock(now)

	// Keep usage windows strictly real-data-driven.
	// If usage_five_hour exists but reset is missing, infer the current 5h block boundary.
	if _, ok := snap.Metrics["usage_five_hour"]; ok {
		if _, ok := snap.Resets["usage_five_hour"]; !ok {
			snap.Resets["usage_five_hour"] = blockEnd
		}
		if _, ok := snap.Attributes["block_start"]; !ok {
			snap.SetAttribute("block_start", blockStart.Format(time.RFC3339))
		}
		if _, ok := snap.Attributes["block_end"]; !ok {
			snap.SetAttribute("block_end", blockEnd.Format(time.RFC3339))
		}
	}
}

func currentFiveHourBlock(now time.Time) (time.Time, time.Time) {
	startHour := (now.Hour() / 5) * 5
	start := time.Date(now.Year(), now.Month(), now.Day(), startHour, 0, 0, 0, now.Location())
	end := start.Add(5 * time.Hour)
	return start, end
}

func resolveCloudBaseURL(acct core.AccountConfig) string {
	normalize := func(raw string) string {
		raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
		if raw == "" {
			return ""
		}
		u, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		switch strings.TrimSpace(strings.ToLower(u.Path)) {
		case "", "/":
			u.Path = ""
		case "/api", "/api/v1":
			u.Path = ""
		}
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}

	if acct.ExtraData != nil {
		if v := strings.TrimSpace(acct.ExtraData["cloud_base_url"]); v != "" {
			return normalize(v)
		}
	}
	if strings.HasPrefix(strings.ToLower(acct.BaseURL), "https://") && strings.Contains(strings.ToLower(acct.BaseURL), "ollama.com") {
		return normalize(acct.BaseURL)
	}
	return normalize(defaultCloudBaseURL)
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

func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	table = strings.TrimSpace(table)
	column = strings.TrimSpace(column)
	if table == "" || column == "" {
		return false, nil
	}
	safeTable := strings.ReplaceAll(table, "'", "''")
	query := fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, safeTable)
	var count int
	if err := db.QueryRowContext(ctx, query, column).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
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
		var rawModel string
		var count float64
		if err := rows.Scan(&rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
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

	todayRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
			AND date(created_at, 'localtime') = date('now', 'localtime')
		GROUP BY model_name`)
	if err == nil {
		defer todayRows.Close()
		for todayRows.Next() {
			var rawModel string
			var count float64
			if err := todayRows.Scan(&rawModel, &count); err != nil {
				return err
			}
			model := normalizeModelName(rawModel)
			if model == "" {
				continue
			}

			metricKey := "model_" + sanitizeMetricPart(model) + "_requests_today"
			setValueMetric(snap, metricKey, count, "requests", "today")

			rec := core.ModelUsageRecord{
				RawModelID: model,
				RawSource:  "sqlite",
				Window:     "today",
				Requests:   core.Float64Ptr(count),
			}
			rec.SetDimension("provider", "ollama")
			core.AppendModelUsageRecord(snap, rec)
		}
		if err := todayRows.Err(); err != nil {
			return err
		}
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
		var rawModel string
		var count float64
		if err := perDayRows.Scan(&date, &rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
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
		usageSeriesKey := "usage_model_" + sanitizeMetricPart(model)
		snap.DailySeries[usageSeriesKey] = mapToSortedTimePoints(byDate)
	}

	return nil
}

func populateEstimatedTokenUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	hasThinking, err := tableHasColumn(ctx, db, "messages", "thinking")
	if err != nil {
		return err
	}

	thinkingExpr := `''`
	if hasThinking {
		thinkingExpr = `COALESCE(thinking, '')`
	}

	query := fmt.Sprintf(`SELECT chat_id, id, role, model_name, COALESCE(content, ''), %s, COALESCE(created_at, '')
		FROM messages
		ORDER BY chat_id, datetime(created_at), id`, thinkingExpr)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	type tokenAgg struct {
		input    float64
		output   float64
		requests float64
	}
	ensureAgg := func(m map[string]*tokenAgg, key string) *tokenAgg {
		if m[key] == nil {
			m[key] = &tokenAgg{}
		}
		return m[key]
	}
	ensureDaily := func(m map[string]map[string]float64, key string) map[string]float64 {
		if m[key] == nil {
			m[key] = make(map[string]float64)
		}
		return m[key]
	}

	modelAgg := make(map[string]*tokenAgg)
	sourceAgg := make(map[string]*tokenAgg)
	dailyTokens := make(map[string]float64)
	dailyRequests := make(map[string]float64)
	modelDailyTokens := make(map[string]map[string]float64)
	sourceDailyTokens := make(map[string]map[string]float64)
	sourceDailyRequests := make(map[string]map[string]float64)
	sessionsBySource := make(map[string]float64)

	now := time.Now().In(time.Local)
	start5h := now.Add(-5 * time.Hour)
	start1d := now.Add(-24 * time.Hour)
	start7d := now.Add(-7 * 24 * time.Hour)

	var tokens5h float64
	var tokens1d float64
	var tokens7d float64
	var tokensToday float64

	currentChat := ""
	pendingInputChars := 0
	chatSources := make(map[string]bool)
	flushChat := func() {
		for source := range chatSources {
			sessionsBySource[source]++
		}
		clear(chatSources)
		pendingInputChars = 0
	}

	for rows.Next() {
		var chatID string
		var id int64
		var role sql.NullString
		var modelName sql.NullString
		var content sql.NullString
		var thinking sql.NullString
		var createdAt sql.NullString

		if err := rows.Scan(&chatID, &id, &role, &modelName, &content, &thinking, &createdAt); err != nil {
			return err
		}

		if currentChat == "" {
			currentChat = chatID
		}
		if chatID != currentChat {
			flushChat()
			currentChat = chatID
		}

		roleVal := strings.ToLower(strings.TrimSpace(role.String))
		contentLen := len(content.String)
		thinkingLen := len(thinking.String)

		ts := time.Time{}
		if createdAt.Valid && strings.TrimSpace(createdAt.String) != "" {
			if parsed, ok := parseAnyTime(createdAt.String); ok {
				ts = parsed.In(time.Local)
			}
		}
		day := ""
		if !ts.IsZero() {
			day = ts.Format("2006-01-02")
		} else if createdAt.Valid && len(createdAt.String) >= 10 {
			day = createdAt.String[:10]
		}

		if roleVal == "user" {
			pendingInputChars += contentLen + thinkingLen
			continue
		}
		if roleVal != "assistant" {
			continue
		}

		model := strings.TrimSpace(modelName.String)
		model = normalizeModelName(model)
		if model == "" {
			continue
		}
		modelKey := sanitizeMetricPart(model)
		source := sourceFromModelName(model)
		sourceKey := sanitizeMetricPart(source)

		inputTokens := estimateTokensFromChars(pendingInputChars)
		outputTokens := estimateTokensFromChars(contentLen + thinkingLen)
		totalTokens := inputTokens + outputTokens
		pendingInputChars = 0

		modelTotals := ensureAgg(modelAgg, model)
		modelTotals.input += inputTokens
		modelTotals.output += outputTokens
		modelTotals.requests++

		sourceTotals := ensureAgg(sourceAgg, sourceKey)
		sourceTotals.input += inputTokens
		sourceTotals.output += outputTokens
		sourceTotals.requests++
		chatSources[sourceKey] = true

		if day != "" {
			dailyTokens[day] += totalTokens
			dailyRequests[day]++
			ensureDaily(modelDailyTokens, modelKey)[day] += totalTokens
			ensureDaily(sourceDailyTokens, sourceKey)[day] += totalTokens
			ensureDaily(sourceDailyRequests, sourceKey)[day]++
			if day == now.Format("2006-01-02") {
				tokensToday += totalTokens
			}
		}

		if !ts.IsZero() {
			if ts.After(start5h) {
				tokens5h += totalTokens
			}
			if ts.After(start1d) {
				tokens1d += totalTokens
			}
			if ts.After(start7d) {
				tokens7d += totalTokens
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if currentChat != "" {
		flushChat()
	}

	type modelTotal struct {
		name string
		tok  float64
	}
	var topModels []modelTotal
	for model, totals := range modelAgg {
		modelKey := sanitizeMetricPart(model)
		setValueMetric(snap, "model_"+modelKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "model_"+modelKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "model_"+modelKey+"_total_tokens", totals.input+totals.output, "tokens", "all-time")

		rec := core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "sqlite_estimate",
			Window:       "all-time",
			InputTokens:  core.Float64Ptr(totals.input),
			OutputTokens: core.Float64Ptr(totals.output),
			TotalTokens:  core.Float64Ptr(totals.input + totals.output),
			Requests:     core.Float64Ptr(totals.requests),
		}
		rec.SetDimension("provider", "ollama")
		rec.SetDimension("estimation", "chars_div_4")
		core.AppendModelUsageRecord(snap, rec)

		topModels = append(topModels, modelTotal{name: model, tok: totals.input + totals.output})
	}
	sort.Slice(topModels, func(i, j int) bool {
		if topModels[i].tok == topModels[j].tok {
			return topModels[i].name < topModels[j].name
		}
		return topModels[i].tok > topModels[j].tok
	})
	if len(topModels) > 0 {
		top := make([]string, 0, minInt(len(topModels), 6))
		for i := 0; i < len(topModels) && i < 6; i++ {
			top = append(top, fmt.Sprintf("%s=%.0f", topModels[i].name, topModels[i].tok))
		}
		snap.Raw["model_tokens_estimated_top"] = strings.Join(top, ", ")
	}

	for sourceKey, totals := range sourceAgg {
		totalTokens := totals.input + totals.output
		setValueMetric(snap, "client_"+sourceKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_total_tokens", totalTokens, "tokens", "all-time")
		setValueMetric(snap, "client_"+sourceKey+"_requests", totals.requests, "requests", "all-time")
		if sessions := sessionsBySource[sourceKey]; sessions > 0 {
			setValueMetric(snap, "client_"+sourceKey+"_sessions", sessions, "sessions", "all-time")
		}

		setValueMetric(snap, "provider_"+sourceKey+"_input_tokens", totals.input, "tokens", "all-time")
		setValueMetric(snap, "provider_"+sourceKey+"_output_tokens", totals.output, "tokens", "all-time")
		setValueMetric(snap, "provider_"+sourceKey+"_requests", totals.requests, "requests", "all-time")
	}

	for sourceKey, byDay := range sourceDailyTokens {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["tokens_client_"+sourceKey] = mapToSortedTimePoints(byDay)
	}
	for sourceKey, byDay := range sourceDailyRequests {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_client_"+sourceKey] = mapToSortedTimePoints(byDay)
	}
	for modelKey, byDay := range modelDailyTokens {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["tokens_model_"+modelKey] = mapToSortedTimePoints(byDay)
	}
	if len(dailyTokens) > 0 {
		snap.DailySeries["analytics_tokens"] = mapToSortedTimePoints(dailyTokens)
	}
	if len(dailyRequests) > 0 {
		snap.DailySeries["analytics_requests"] = mapToSortedTimePoints(dailyRequests)
	}

	if tokensToday > 0 {
		setValueMetric(snap, "tokens_today", tokensToday, "tokens", "today")
	}
	if tokens5h > 0 {
		setValueMetric(snap, "tokens_5h", tokens5h, "tokens", "5h")
	}
	if tokens1d > 0 {
		setValueMetric(snap, "tokens_1d", tokens1d, "tokens", "1d")
	}
	if tokens7d > 0 {
		setValueMetric(snap, "7d_tokens", tokens7d, "tokens", "7d")
	}

	snap.SetAttribute("token_estimation", "chars_div_4")
	return nil
}

func estimateTokensFromChars(chars int) float64 {
	if chars <= 0 {
		return 0
	}
	return float64((chars + 3) / 4)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func populateSourceUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	allTimeRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY model_name`)
	if err != nil {
		return err
	}
	defer allTimeRows.Close()

	allTimeBySource := make(map[string]float64)
	for allTimeRows.Next() {
		var rawModel string
		var count float64
		if err := allTimeRows.Scan(&rawModel, &count); err != nil {
			return err
		}
		model := normalizeModelName(rawModel)
		source := sourceFromModelName(model)
		allTimeBySource[source] += count
	}
	if err := allTimeRows.Err(); err != nil {
		return err
	}

	for source, count := range allTimeBySource {
		if count <= 0 {
			continue
		}
		sourceKey := sanitizeMetricPart(source)
		setValueMetric(snap, "source_"+sourceKey+"_requests", count, "requests", "all-time")
	}

	todayRows, err := db.QueryContext(ctx, `SELECT model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
			AND date(created_at, 'localtime') = date('now', 'localtime')
		GROUP BY model_name`)
	if err == nil {
		defer todayRows.Close()
		todayBySource := make(map[string]float64)
		for todayRows.Next() {
			var rawModel string
			var count float64
			if err := todayRows.Scan(&rawModel, &count); err != nil {
				return err
			}
			model := normalizeModelName(rawModel)
			source := sourceFromModelName(model)
			todayBySource[source] += count
		}
		if err := todayRows.Err(); err != nil {
			return err
		}

		for source, count := range todayBySource {
			if count <= 0 {
				continue
			}
			sourceKey := sanitizeMetricPart(source)
			setValueMetric(snap, "source_"+sourceKey+"_requests_today", count, "requests", "today")
		}
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(created_at), model_name, COUNT(*)
		FROM messages
		WHERE model_name IS NOT NULL AND trim(model_name) != ''
		GROUP BY date(created_at), model_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perSourceDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var day string
		var rawModel string
		var count float64
		if err := perDayRows.Scan(&day, &rawModel, &count); err != nil {
			return err
		}
		day = strings.TrimSpace(day)
		if day == "" {
			continue
		}
		model := normalizeModelName(rawModel)
		source := sourceFromModelName(model)
		sourceKey := sanitizeMetricPart(source)
		if perSourceDaily[sourceKey] == nil {
			perSourceDaily[sourceKey] = make(map[string]float64)
		}
		perSourceDaily[sourceKey][day] += count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for sourceKey, byDay := range perSourceDaily {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_source_"+sourceKey] = mapToSortedTimePoints(byDay)
	}

	return nil
}

func populateToolUsageFromDB(ctx context.Context, db *sql.DB, snap *core.UsageSnapshot) error {
	hasFunctionName, err := tableHasColumn(ctx, db, "tool_calls", "function_name")
	if err != nil || !hasFunctionName {
		return nil
	}

	rows, err := db.QueryContext(ctx, `SELECT function_name, COUNT(*)
		FROM tool_calls
		WHERE trim(function_name) != ''
		GROUP BY function_name
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var top []string
	for rows.Next() {
		var toolName string
		var count float64
		if err := rows.Scan(&toolName, &count); err != nil {
			return err
		}
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}

		setValueMetric(snap, "tool_"+sanitizeMetricPart(toolName), count, "calls", "all-time")
		if len(top) < 6 {
			top = append(top, fmt.Sprintf("%s=%.0f", toolName, count))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(top) > 0 {
		snap.Raw["tool_usage"] = strings.Join(top, ", ")
	}

	perDayRows, err := db.QueryContext(ctx, `SELECT date(m.created_at), tc.function_name, COUNT(*)
		FROM tool_calls tc
		JOIN messages m ON tc.message_id = m.id
		WHERE trim(tc.function_name) != ''
		GROUP BY date(m.created_at), tc.function_name`)
	if err != nil {
		return nil
	}
	defer perDayRows.Close()

	perToolDaily := make(map[string]map[string]float64)
	for perDayRows.Next() {
		var day string
		var toolName string
		var count float64
		if err := perDayRows.Scan(&day, &toolName, &count); err != nil {
			return err
		}
		day = strings.TrimSpace(day)
		toolKey := sanitizeMetricPart(toolName)
		if day == "" || toolKey == "" {
			continue
		}
		if perToolDaily[toolKey] == nil {
			perToolDaily[toolKey] = make(map[string]float64)
		}
		perToolDaily[toolKey][day] += count
	}
	if err := perDayRows.Err(); err != nil {
		return err
	}

	for toolKey, byDay := range perToolDaily {
		if len(byDay) == 0 {
			continue
		}
		snap.DailySeries["usage_tool_"+toolKey] = mapToSortedTimePoints(byDay)
	}

	return nil
}

func sourceFromModelName(model string) string {
	normalized := normalizeModelName(model)
	if normalized == "" {
		return "unknown"
	}
	if strings.HasSuffix(normalized, ":cloud") || strings.Contains(normalized, "-cloud") {
		return "cloud"
	}
	return "local"
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
			points := mapToSortedTimePoints(byDate)
			snap.DailySeries[dq.key] = points
			if dq.key == "requests_user" {
				if _, exists := snap.DailySeries["requests"]; !exists {
					snap.DailySeries["requests"] = points
				}
			}
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
		"/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/embeddings", "/v1/messages":
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

func normalizeModelName(input string) string {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return ""
	}
	s = strings.Trim(strings.TrimPrefix(s, "models/"), "/")
	if strings.HasPrefix(s, "ollama.com/") {
		s = strings.TrimPrefix(s, "ollama.com/")
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, ":latest"))
	return s
}

func cloudEndpointURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultCloudBaseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func resolveCloudSessionCookie(acct core.AccountConfig) string {
	if acct.ExtraData != nil {
		for _, key := range []string{"cloud_session_cookie", "session_cookie", "cookie"} {
			if v := strings.TrimSpace(acct.ExtraData[key]); v != "" {
				return v
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("OLLAMA_SESSION_COOKIE")); v != "" {
		return v
	}
	return ""
}

func fetchCloudUsageFromSettingsPage(ctx context.Context, cloudBaseURL, apiKey string, acct core.AccountConfig, snap *core.UsageSnapshot) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudEndpointURL(cloudBaseURL, "/settings"), nil)
	if err != nil {
		return false, fmt.Errorf("ollama: creating settings request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if cookie := resolveCloudSessionCookie(acct); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("ollama: cloud settings request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama: cloud settings endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("ollama: reading cloud settings response: %w", err)
	}

	pcts := make(map[string]float64)
	for _, m := range settingsUsageRe.FindAllStringSubmatch(string(body), -1) {
		if len(m) < 3 {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(m[1]))
		v, convErr := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
		if convErr != nil {
			continue
		}
		pcts[label] = v
	}

	resets := make(map[string]time.Time)
	for _, m := range settingsResetRe.FindAllStringSubmatch(string(body), -1) {
		if len(m) < 3 {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(m[1]))
		t, ok := parseAnyTime(strings.TrimSpace(m[2]))
		if !ok {
			continue
		}
		resets[label] = t
	}

	found := false
	if v, ok := pcts["session usage"]; ok {
		snap.Metrics["usage_five_hour"] = core.Metric{
			Used:   core.Float64Ptr(v),
			Unit:   "%",
			Window: "5h",
		}
		if t, ok := resets["session usage"]; ok {
			snap.Resets["usage_five_hour"] = t
			snap.SetAttribute("block_end", t.Format(time.RFC3339))
			snap.SetAttribute("block_start", t.Add(-5*time.Hour).Format(time.RFC3339))
		}
		found = true
	}
	if v, ok := pcts["weekly usage"]; ok {
		weekly := core.Metric{
			Used:   core.Float64Ptr(v),
			Unit:   "%",
			Window: "1w",
		}
		snap.Metrics["usage_weekly"] = weekly
		// Backward-compatible alias for existing widgets/config.
		snap.Metrics["usage_one_day"] = core.Metric{
			Used:   core.Float64Ptr(v),
			Unit:   "%",
			Window: "1d",
		}
		if t, ok := resets["weekly usage"]; ok {
			snap.Resets["usage_weekly"] = t
			snap.Resets["usage_one_day"] = t
		}
		found = true
	}

	return found, nil
}

func setValueMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	snap.Metrics[key] = core.Metric{
		Used:      core.Float64Ptr(value),
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
		name := normalizeModelName(models[i].Name)
		if name == "" {
			name = normalizeModelName(models[i].Model)
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

func anyValueCaseInsensitive(m map[string]any, keys ...string) (any, bool) {
	if len(m) == 0 {
		return nil, false
	}
	want := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		norm := normalizeLookupKey(key)
		if norm == "" {
			continue
		}
		want[norm] = struct{}{}
	}
	for k, v := range m {
		if _, ok := want[normalizeLookupKey(k)]; ok {
			return v, true
		}
	}
	return nil, false
}

func anyStringCaseInsensitive(m map[string]any, keys ...string) string {
	v, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case fmt.Stringer:
		return strings.TrimSpace(val.String())
	default:
		return ""
	}
}

func anyMapCaseInsensitive(m map[string]any, keys ...string) map[string]any {
	v, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return nil
	}
	out, _ := v.(map[string]any)
	return out
}

func anyBoolCaseInsensitive(m map[string]any, keys ...string) (bool, bool) {
	v, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return false, false
	}
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err == nil {
			return b, true
		}
	}
	return false, false
}

func anyFloatCaseInsensitive(m map[string]any, keys ...string) (float64, bool) {
	v, ok := anyValueCaseInsensitive(m, keys...)
	if !ok {
		return 0, false
	}
	return anyFloat(v)
}

func anyFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		if err == nil {
			return f, true
		}
	case string:
		s := strings.TrimSpace(strings.TrimSuffix(val, "%"))
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func anyNullStringCaseInsensitive(m map[string]any, keys ...string) string {
	raw := anyMapCaseInsensitive(m, keys...)
	if len(raw) == 0 {
		return ""
	}
	valid, ok := anyBoolCaseInsensitive(raw, "valid")
	if ok && !valid {
		return ""
	}
	return anyStringCaseInsensitive(raw, "string", "value")
}

func anyNullTimeCaseInsensitive(m map[string]any, keys ...string) (time.Time, bool) {
	raw := anyMapCaseInsensitive(m, keys...)
	if len(raw) == 0 {
		return time.Time{}, false
	}
	valid, ok := anyBoolCaseInsensitive(raw, "valid")
	if ok && !valid {
		return time.Time{}, false
	}
	timeRaw := anyStringCaseInsensitive(raw, "time", "value")
	if timeRaw == "" {
		return time.Time{}, false
	}
	return parseAnyTime(timeRaw)
}

func normalizeLookupKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	return s
}

func parseAnyTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

type ginLogEvent struct {
	Timestamp time.Time
	Status    int
	Duration  time.Duration
	Method    string
	Path      string
}

type logMetrics struct {
	dailyRequests map[string]float64

	requests5h     int
	requests1d     int
	requestsToday  int
	recentRequests int
	requests7d     int

	chatRequests5h        int
	generateRequests5h    int
	errors4xx5h           int
	errors5xx5h           int
	latencyTotal5h        time.Duration
	latencyCount5h        int
	chatRequests1d        int
	generateRequests1d    int
	errors4xx1d           int
	errors5xx1d           int
	latencyTotal1d        time.Duration
	latencyCount1d        int
	chatRequestsToday     int
	generateRequestsToday int
	errors4xxToday        int
	errors5xxToday        int
	latencyTotal          time.Duration
	latencyCount          int
}
