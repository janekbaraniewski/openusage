package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const (
	providerID           = "cursor"
	defaultAccountID     = "cursor"
	defaultUsageWindow   = "session"
	quotaNearLimitRatio  = 0.15
	statusFilePathEnvVar = "OPENUSAGE_CURSOR_STATUS_FILE"
)

// Provider exposes Cursor usage from the local status line plus DashboardService plan buckets.
type Provider struct {
	providerbase.Base
	clock  core.Clock
	client *Client
}

// New returns the Cursor provider.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: providerID,
			Info: core.ProviderInfo{
				Name:         "Cursor IDE",
				Capabilities: []string{"local_config", "statusline", "token_usage", "quota", "by_model", "by_workspace"},
				DocURL:       "https://www.cursor.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: defaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				DocsURL: "https://www.cursor.com/",
				Quickstart: []string{
					"Install and run Cursor CLI so ~/.cursor exists.",
					"Run `openusage integrations install cursor` to connect the status line.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

// WithClient injects a DashboardService client (tests / custom base URL).
func (p *Provider) WithClient(c *Client) *Provider {
	p.client = c
	return p
}

// DetailWidget keeps the provider's detail view focused on the generic usage and token sections.
func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}

// DefaultStatusFilePath returns the path written by the installed Cursor status-line command.
func DefaultStatusFilePath() string {
	if path := strings.TrimSpace(os.Getenv(statusFilePathEnvVar)); path != "" {
		return path
	}
	stateDir, err := telemetry.DefaultStateDir()
	if err != nil || strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, "cursor-status.json")
}

func statusFilePath(acct core.AccountConfig) string {
	return strings.TrimSpace(acct.Path("status_file", DefaultStatusFilePath()))
}

// Fetch projects the latest captured status-line document into a snapshot.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	path := statusFilePath(acct)
	if path != "" {
		snap.Raw["status_file"] = path
	}

	if err := ctx.Err(); err != nil {
		return snap, err
	}
	if path == "" {
		p.applyLivePlanUsage(ctx, acct, &snap)
		if _, ok := snap.Metrics["plan_percent_used"]; ok {
			return snap, nil
		}
		snap.Status = core.StatusAuth
		snap.Message = "Cursor status-line path is unavailable"
		return snap, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Check if optional CSV export is configured
			csvExportPath := strings.TrimSpace(acct.Path("csv_export", ""))
			if csvExportPath == "" && strings.HasSuffix(strings.ToLower(strings.TrimSpace(acct.Binary)), ".csv") {
				csvExportPath = strings.TrimSpace(acct.Binary)
			}
			if csvExportPath != "" && fileExists(csvExportPath) {
				if records, version, csvErr := parseCursorCSVFile(csvExportPath); csvErr == nil {
					applyCursorCSVToSnapshot(records, &snap)
					if version > 0 {
						snap.Raw["csv_schema_version"] = fmt.Sprintf("v%d", version)
					}
					p.applyLivePlanUsage(ctx, acct, &snap)
					return snap, nil
				}
			}

			// Check if container/account has a cli-config.json
			configDir := strings.TrimSpace(acct.Path("config_dir", ""))
			if configDir == "" {
				if home, hErr := os.UserHomeDir(); hErr == nil {
					boxName := strings.TrimPrefix(acct.ID, "cursor-")
					if boxName != acct.ID && boxName != "" {
						for _, cDir := range []string{".agent-containers", ".cursor-containers"} {
							candidate := filepath.Join(home, cDir, boxName, ".cursor")
							if fileExists(filepath.Join(candidate, "cli-config.json")) {
								configDir = candidate
								break
							}
						}
					}
					if configDir == "" && path == DefaultStatusFilePath() {
						configDir = filepath.Join(home, ".cursor")
					}
				}
			}

			if configDir != "" {
				cfgFile := filepath.Join(configDir, "cli-config.json")
				if cfgData, cErr := os.ReadFile(cfgFile); cErr == nil {
					var cfg struct {
						Model struct {
							DisplayName string `json:"displayName"`
							ModelID     string `json:"modelId"`
						} `json:"model"`
						AuthInfo struct {
							Email       string `json:"email"`
							DisplayName string `json:"displayName"`
							UserID      any    `json:"userId"`
							AuthID      string `json:"authId"`
						} `json:"authInfo"`
					}
					if json.Unmarshal(cfgData, &cfg) == nil && (cfg.AuthInfo.Email != "" || cfg.AuthInfo.AuthID != "") {
						snap.Status = core.StatusOK
						snap.Timestamp = time.Now().UTC()
						if info, sErr := os.Stat(cfgFile); sErr == nil {
							snap.Timestamp = info.ModTime().UTC()
						}
						if cfg.AuthInfo.Email != "" {
							snap.SetAttribute("email", cfg.AuthInfo.Email)
							snap.SetAttribute("account_email", cfg.AuthInfo.Email)
						}
						if cfg.AuthInfo.DisplayName != "" {
							snap.SetAttribute("username", cfg.AuthInfo.DisplayName)
						}
						mName := cfg.Model.DisplayName
						if mName == "" {
							mName = cfg.Model.ModelID
						}
						if mName != "" {
							snap.SetAttribute("model", mName)
						}
						p.applyLivePlanUsage(ctx, acct, &snap)
						return snap, nil
					}
				}
			}

			snap.Status = core.StatusAuth
			snap.Message = "No Cursor status-line data yet"
			snap.SetDiagnostic("setup", "Run `openusage integrations install cursor`, then start cursor")
			p.applyLivePlanUsage(ctx, acct, &snap)
			if _, ok := snap.Metrics["plan_percent_used"]; ok {
				snap.Status = core.StatusOK
				snap.Message = ""
			}
			return snap, nil
		}
		return snap, fmt.Errorf("cursor: read status file: %w", err)
	}

	payload, err := parseStatusLinePayload(data)
	if err != nil {
		snap.Status = core.StatusError
		snap.Message = "Cursor status-line data is malformed"
		snap.SetDiagnostic("parse_error", err.Error())
		return snap, nil
	}

	projectSnapshot(&snap, payload)
	p.applyLivePlanUsage(ctx, acct, &snap)
	return snap, nil
}

func (p *Provider) applyLivePlanUsage(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	if p == nil || snap == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	token, auth := resolveCursorAccessToken(acct)
	if token == "" {
		return
	}
	client := p.client
	if client == nil {
		client = NewClient(acct.BaseURL, nil)
	}
	live, err := client.fetchLivePlanUsage(ctx, token)
	if err != nil {
		snap.SetDiagnostic("plan_api_error", err.Error())
		return
	}
	applyLivePlanToSnapshot(snap, live, auth)
}

func applyLivePlanToSnapshot(snap *core.UsageSnapshot, live livePlanUsage, auth LocalAuthInfo) {
	if snap == nil {
		return
	}
	snap.EnsureMaps()
	setPct := func(key string, used *float64) {
		if used == nil {
			return
		}
		pct := clamp(*used, 0, 100)
		metric := core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(pct),
			Remaining: core.Float64Ptr(100 - pct),
			Unit:      "%",
			Window:    "monthly",
		}
		snap.Metrics[key] = metric
		if !live.ResetAt.IsZero() {
			snap.Resets[key] = live.ResetAt
			snap.Resets[key+"_reset"] = live.ResetAt
		}
	}
	setPct("plan_percent_used", live.Included)
	setPct("plan_auto_percent_used", live.Auto)
	setPct("plan_api_percent_used", live.API)
	if m, ok := snap.Metrics["plan_percent_used"]; ok {
		snap.Metrics["cursor_plan_usage"] = m
		if !live.ResetAt.IsZero() {
			snap.Resets["cursor_plan_usage"] = live.ResetAt
			snap.Resets["quota"] = live.ResetAt
			snap.Resets["billing_cycle_end"] = live.ResetAt
		}
	}
	if planBucketHitMonthlyLimit(live) && snap.Status != core.StatusError {
		snap.Status = core.StatusLimited
		snap.Message = "Monthly usage has been hit"
	}
	if live.PlanName != "" {
		snap.SetAttribute("plan_tier", live.PlanName)
	} else if auth.MembershipType != "" {
		snap.SetAttribute("plan_tier", auth.MembershipType)
	}
	if live.Ondemand != "" {
		snap.SetAttribute("ondemand", live.Ondemand)
	}
	if auth.Email != "" && snap.Attributes["account_email"] == "" {
		snap.SetAttribute("account_email", auth.Email)
		snap.Raw["account_email"] = auth.Email
	}
	snap.Raw["plan_source"] = "dashboard_api"
	if snap.Status == "" || snap.Status == core.StatusAuth {
		snap.Status = core.StatusOK
	}
}

func planBucketHitMonthlyLimit(live livePlanUsage) bool {
	hit := func(v *float64) bool {
		return v != nil && *v >= 100
	}
	return hit(live.Auto) || hit(live.API)
}

// EnrichSnapshots overlays live Included/Auto/API plan buckets onto daemon
// snapshots. make run reads the telemetry daemon, which often still only has
// status-line context metrics; this fills plan usage in the TUI process.
func (p *Provider) EnrichSnapshots(ctx context.Context, accounts []core.AccountConfig, snaps map[string]core.UsageSnapshot) {
	if p == nil || len(snaps) == 0 {
		return
	}
	byID := make(map[string]core.AccountConfig, len(accounts))
	for _, acct := range accounts {
		if id := strings.TrimSpace(acct.ID); id != "" {
			byID[id] = acct
		}
	}
	for id, snap := range snaps {
		if snap.ProviderID != "cursor" {
			continue
		}
		acct, ok := byID[id]
		if !ok {
			acct = core.AccountConfig{ID: id, Provider: "cursor"}
		}
		p.applyLivePlanUsage(ctx, acct, &snap)
		snaps[id] = snap
	}
}

// HasChanged reports whether the Cursor status-line state file has been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	path := statusFilePath(acct)
	if path == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.ModTime().After(since), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func projectSnapshot(snap *core.UsageSnapshot, payload statusLinePayload) {
	if snap == nil {
		return
	}

	snap.Timestamp = payloadReceivedAt(payload)
	snap.Status = statusFromQuota(payload)

	modelID := strings.TrimSpace(payload.Model.ID)
	modelName := strings.TrimSpace(payload.Model.DisplayName)
	if modelName == "" {
		modelName = modelID
	}
	if modelID != "" {
		snap.SetAttribute("model_id", modelID)
	}
	if modelName != "" {
		snap.SetAttribute("model", modelName)
	}
	if payload.Model.ParamSummary != "" {
		snap.SetAttribute("model_param", payload.Model.ParamSummary)
	}
	if workspace := statusWorkspace(payload); workspace != "" {
		snap.SetAttribute("workspace", workspace)
	}
	if payload.SessionID != "" {
		snap.SetAttribute("session_id", payload.SessionID)
	}
	if payload.SessionName != "" {
		snap.SetAttribute("session_name", payload.SessionName)
	}
	if payload.Version != "" {
		snap.SetAttribute("cli_version", payload.Version)
	}
	if payload.PlanTier != "" {
		snap.SetAttribute("plan_tier", payload.PlanTier)
	}

	email := payload.Email
	if email == "" && payload.AuthInfo != nil {
		email = payload.AuthInfo.Email
	}
	if email != "" {
		snap.SetAttribute("account_email", email)
		snap.Raw["account_email"] = email
	}
	if payload.Worktree != nil && payload.Worktree.Name != "" {
		snap.SetAttribute("worktree", payload.Worktree.Name)
	}
	if payload.AgentState != "" {
		snap.Raw["agent_state"] = payload.AgentState
	}

	projectContextMetrics(snap, payload.ContextWindow)
	projectCurrentUsageMetrics(snap, payload.ContextWindow.CurrentUsage)
	projectQuotaMetrics(snap, payload)

	if q, ok := snap.Metrics["quota"]; ok {
		snap.Metrics["cursor_plan_usage"] = q
		snap.Metrics["plan_percent_used"] = q
	}
	if cw, ok := snap.Metrics["context_window"]; ok {
		snap.Metrics["context_window_percent"] = cw
	}
	if tt, ok := snap.Metrics["total_tokens"]; ok {
		snap.Metrics["tokens_used"] = tt
	}

	if total := cumulativeTotalTokens(payload.ContextWindow); total > 0 {
		snap.Metrics["total_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(total)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
		if modelName != "" {
			record := core.ModelUsageRecord{
				RawModelID:  modelName,
				RawSource:   "statusline",
				Window:      defaultUsageWindow,
				InputTokens: core.Float64Ptr(float64(payload.ContextWindow.TotalInputTokens)),
				TotalTokens: core.Float64Ptr(float64(total)),
			}
			if payload.ContextWindow.TotalOutputTokens != nil {
				record.OutputTokens = core.Float64Ptr(float64(*payload.ContextWindow.TotalOutputTokens))
			}
			record.SetDimension("workspace", statusWorkspace(payload))
			snap.AppendModelUsage(record)
		}
	}

	if snap.Message == "" {
		if modelName != "" {
			snap.Message = fmt.Sprintf("Cursor CLI (%s)", modelName)
		} else {
			snap.Message = "Cursor CLI status line"
		}
	}
}

func projectContextMetrics(snap *core.UsageSnapshot, contextWindow statusLineContextWindow) {
	used, remaining, hasPercent := contextPercentages(contextWindow)
	if hasPercent {
		snap.Metrics["context_window"] = core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(used),
			Remaining: core.Float64Ptr(remaining),
			Unit:      "%",
			Window:    defaultUsageWindow,
		}
	}

	if contextWindow.TotalInputTokens > 0 {
		snap.Metrics["total_input_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(contextWindow.TotalInputTokens)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
	}
	if contextWindow.TotalOutputTokens != nil && *contextWindow.TotalOutputTokens > 0 {
		snap.Metrics["total_output_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(*contextWindow.TotalOutputTokens)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
	}
	if !hasPercent && contextWindow.ContextWindowSize != nil && *contextWindow.ContextWindowSize > 0 {
		usedTokens := cumulativeTotalTokens(contextWindow)
		if usedTokens > 0 {
			snap.Metrics["context_window"] = core.Metric{
				Limit:  core.Float64Ptr(float64(*contextWindow.ContextWindowSize)),
				Used:   core.Float64Ptr(float64(usedTokens)),
				Unit:   "tokens",
				Window: defaultUsageWindow,
			}
		}
	}
}

func projectCurrentUsageMetrics(snap *core.UsageSnapshot, usage *statusLineCurrentUsage) {
	if usage == nil {
		return
	}
	setTokenMetric := func(key string, value int64) {
		if value <= 0 {
			return
		}
		snap.Metrics[key] = core.Metric{Used: core.Float64Ptr(float64(value)), Unit: "tokens", Window: "current"}
	}
	setTokenMetric("current_input_tokens", usage.InputTokens)
	setTokenMetric("current_output_tokens", usage.OutputTokens)
	setTokenMetric("current_cache_read_tokens", usage.CacheReadTokens)
	setTokenMetric("current_cache_write_tokens", usage.CacheWriteTokensValue())
	if total := usage.TotalTokens(); total > 0 {
		setTokenMetric("current_tokens", total)
	}
}

func projectQuotaMetrics(snap *core.UsageSnapshot, payload statusLinePayload) {
	if len(payload.Quota) == 0 {
		return
	}
	keys := make([]string, 0, len(payload.Quota))
	for key := range payload.Quota {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	now := time.Now().UTC()
	receivedAt := payloadReceivedAt(payload)

	worst := 1.0
	worstName := ""
	found := false
	for _, name := range keys {
		quota := payload.Quota[name]
		if quota.RemainingFraction == nil {
			continue
		}
		remaining := clamp(*quota.RemainingFraction, 0, 1)
		cleanName := sanitizeMetricName(name)

		window := "quota"
		var period time.Duration
		if strings.Contains(cleanName, "5h") {
			window = "5h"
			period = 5 * time.Hour
		} else if strings.Contains(cleanName, "weekly") || strings.Contains(cleanName, "7d") {
			window = "7d"
			period = 7 * 24 * time.Hour
		}

		reset := quotaResetTime(quota, receivedAt)
		if !reset.IsZero() && period > 0 && reset.Before(now) {
			for reset.Before(now) {
				reset = reset.Add(period)
			}
			remaining = 1.0
		}

		remainingPercent := remaining * 100

		metric := core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(100 - remainingPercent),
			Remaining: core.Float64Ptr(remainingPercent),
			Unit:      "%",
			Window:    window,
		}

		key := "quota_" + cleanName
		snap.Metrics[key] = metric

		if !reset.IsZero() {
			snap.Resets[key] = reset
			snap.Resets[key+"_reset"] = reset
		}

		if !quota.Disabled {
			if !found || remaining < worst {
				worst = remaining
				worstName = name
				found = true
			}
		}
	}

	if !found {
		return
	}

	remainingPercent := worst * 100
	snap.Metrics["quota"] = core.Metric{
		Limit:     core.Float64Ptr(100),
		Used:      core.Float64Ptr(100 - remainingPercent),
		Remaining: core.Float64Ptr(remainingPercent),
		Unit:      "%",
		Window:    "quota",
	}
	if quota, ok := payload.Quota[worstName]; ok {
		if reset := quotaResetTime(quota, receivedAt); !reset.IsZero() {
			snap.Resets["quota"] = reset
			snap.Resets["quota_reset"] = reset
		}
	}
}

func statusFromQuota(payload statusLinePayload) core.Status {
	// First check context window usage
	if payload.ContextWindow.UsedPercentage != nil {
		used := *payload.ContextWindow.UsedPercentage
		if used >= 100 {
			return core.StatusLimited
		}
		if used >= 85 {
			return core.StatusNearLimit
		}
	}

	worst, ok := worstQuotaFraction(payload)
	if !ok {
		return core.StatusOK
	}
	if worst <= 0 {
		return core.StatusLimited
	}
	if worst < quotaNearLimitRatio {
		return core.StatusNearLimit
	}
	return core.StatusOK
}

func worstQuotaFraction(payload statusLinePayload) (float64, bool) {
	worst := 1.0
	found := false
	for _, quota := range payload.Quota {
		if quota.RemainingFraction == nil || quota.Disabled {
			continue
		}
		fraction := clamp(*quota.RemainingFraction, 0, 1)
		if !found || fraction < worst {
			worst = fraction
			found = true
		}
	}
	return worst, found
}

func contextPercentages(contextWindow statusLineContextWindow) (float64, float64, bool) {
	if contextWindow.UsedPercentage == nil && contextWindow.RemainingPercentage == nil {
		return 0, 0, false
	}
	used := 0.0
	remaining := 0.0
	if contextWindow.UsedPercentage != nil {
		used = clamp(*contextWindow.UsedPercentage, 0, 100)
	}
	if contextWindow.RemainingPercentage != nil {
		remaining = clamp(*contextWindow.RemainingPercentage, 0, 100)
	}
	if contextWindow.UsedPercentage == nil {
		used = 100 - remaining
	}
	if contextWindow.RemainingPercentage == nil {
		remaining = 100 - used
	}
	return used, remaining, true
}

func cumulativeTotalTokens(contextWindow statusLineContextWindow) int64 {
	out := int64(0)
	if contextWindow.TotalOutputTokens != nil {
		out = *contextWindow.TotalOutputTokens
	}
	return contextWindow.TotalInputTokens + out
}

func statusWorkspace(payload statusLinePayload) string {
	if current := strings.TrimSpace(payload.Workspace.CurrentDir); current != "" {
		return current
	}
	if project := strings.TrimSpace(payload.Workspace.ProjectDir); project != "" {
		return project
	}
	return strings.TrimSpace(payload.CWD)
}

func payloadReceivedAt(payload statusLinePayload) time.Time {
	if !payload.ReceivedAt.IsZero() {
		return payload.ReceivedAt.UTC()
	}
	return time.Now().UTC()
}

func quotaResetTime(quota statusLineQuota, receivedAt time.Time) time.Time {
	if reset := strings.TrimSpace(quota.ResetTime); reset != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, reset); err == nil {
			return parsed.UTC()
		}
	}
	if quota.ResetInSeconds != nil && *quota.ResetInSeconds > 0 {
		return receivedAt.Add(time.Duration(*quota.ResetInSeconds) * time.Second)
	}
	return time.Time{}
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func sanitizeMetricName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := b.String()
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	return strings.Trim(res, "_")
}
