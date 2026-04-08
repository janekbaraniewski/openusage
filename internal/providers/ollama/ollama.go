package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
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
	clock core.Clock
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
		clock: core.SystemClock{},
	}
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Languages", Order: 3, Style: core.DetailSectionStyleLanguages},
			{Name: "MCP Usage", Order: 4, Style: core.DetailSectionStyleMCP},
			{Name: "Spending", Order: 5, Style: core.DetailSectionStyleSpending},
			{Name: "Trends", Order: 6, Style: core.DetailSectionStyleTrends},
			{Name: "Tokens", Order: 7, Style: core.DetailSectionStyleTokens},
			{Name: "Activity", Order: 8, Style: core.DetailSectionStyleActivity},
		},
	}
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}

// HasChanged reports whether Ollama's local data files have been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	if dbPath := resolveDesktopDBPath(acct); dbPath != "" {
		if info, err := os.Stat(dbPath); err == nil && info.ModTime().After(since) {
			return true, nil
		}
	}
	if configPath := resolveServerConfigPath(acct); configPath != "" {
		if info, err := os.Stat(configPath); err == nil && info.ModTime().After(since) {
			return true, nil
		}
	}
	return false, nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	cloudOnly := strings.EqualFold(acct.Auth, string(core.ProviderAuthTypeAPIKey))
	if cloudOnly && authSnap != nil {
		return *authSnap, nil
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)
	hasData := false

	if !cloudOnly {
		localBaseURL := shared.ResolveBaseURL(acct, defaultLocalBaseURL)

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
	}

	finalizeUsageWindows(&snap, p.now())

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

func (p *Provider) fetchServerLogs(acct core.AccountConfig, snap *core.UsageSnapshot) (bool, error) {
	logFiles := resolveServerLogFiles(acct)
	if len(logFiles) == 0 {
		return false, nil
	}

	now := p.now()
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

	snap.DailySeries["requests"] = core.SortedTimePoints(metrics.dailyRequests)
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

type showResponse struct {
	Capabilities []string       `json:"capabilities"`
	Details      modelDetails   `json:"details"`
	ModelInfo    map[string]any `json:"model_info"`
	RemoteModel  string         `json:"remote_model"`
	RemoteHost   string         `json:"remote_host"`
	Template     string         `json:"template"`
	ModifiedAt   string         `json:"modified_at"`
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
