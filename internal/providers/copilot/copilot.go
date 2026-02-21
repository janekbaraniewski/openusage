package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	copilotAllTimeWindow = "all-time"
	maxCopilotModels     = 8
	maxCopilotClients    = 6
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "copilot",
			Info: core.ProviderInfo{
				Name: "GitHub Copilot",
				Capabilities: []string{
					"quota_tracking", "plan_detection", "chat_quota",
					"completions_quota", "org_billing", "org_metrics",
					"session_tracking", "local_config", "rate_limits",
				},
				DocURL: "https://docs.github.com/en/copilot",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeCLI,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install GitHub CLI and run `gh auth login`.",
					"Ensure Copilot entitlement is enabled for the authenticated account.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

type ghUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Plan  struct {
		Name string `json:"name"`
	} `json:"plan"`
}

type copilotInternalUser struct {
	Login                    string            `json:"login"`
	AccessTypeSKU            string            `json:"access_type_sku"`
	CopilotPlan              string            `json:"copilot_plan"`
	AssignedDate             string            `json:"assigned_date"`
	ChatEnabled              bool              `json:"chat_enabled"`
	MCPEnabled               bool              `json:"is_mcp_enabled"`
	CopilotIgnoreEnabled     bool              `json:"copilotignore_enabled"`
	CodexAgentEnabled        bool              `json:"codex_agent_enabled"`
	RestrictedTelemetry      bool              `json:"restricted_telemetry"`
	CanSignupForLimited      bool              `json:"can_signup_for_limited"`
	LimitedUserSubscribedDay int               `json:"limited_user_subscribed_day"`
	LimitedUserResetDate     string            `json:"limited_user_reset_date"`
	UsageResetDate           string            `json:"quota_reset_date"`
	UsageResetDateUTC        string            `json:"quota_reset_date_utc"`
	AnalyticsTrackingID      string            `json:"analytics_tracking_id"`
	Endpoints                map[string]string `json:"endpoints"`
	OrganizationLoginList    []string          `json:"organization_login_list"`

	LimitedUserUsage *copilotUsageLimits    `json:"limited_user_quotas"`
	MonthlyUsage     *copilotUsageLimits    `json:"monthly_quotas"`
	UsageSnapshots   *copilotUsageSnapshots `json:"quota_snapshots"`

	OrganizationList []copilotOrgEntry `json:"organization_list"`
}

type copilotUsageLimits struct {
	Chat        *int `json:"chat"`
	Completions *int `json:"completions"`
}

type copilotUsageSnapshots struct {
	Chat                *copilotUsageSnapshot `json:"chat"`
	Completions         *copilotUsageSnapshot `json:"completions"`
	PremiumInteractions *copilotUsageSnapshot `json:"premium_interactions"`
}

type copilotUsageSnapshot struct {
	Entitlement      *float64 `json:"entitlement"`
	OverageCount     *float64 `json:"overage_count"`
	OveragePermitted *bool    `json:"overage_permitted"`
	PercentRemaining *float64 `json:"percent_remaining"`
	UsageID          string   `json:"quota_id"`
	UsageRemaining   *float64 `json:"quota_remaining"`
	Remaining        *float64 `json:"remaining"`
	Unlimited        *bool    `json:"unlimited"`
	TimestampUTC     string   `json:"timestamp_utc"`
}

type copilotOrgEntry struct {
	Login              string `json:"login"`
	IsEnterprise       bool   `json:"is_enterprise"`
	CopilotPlan        string `json:"copilot_plan"`
	CopilotSeatManager string `json:"copilot_seat_manager"`
}

type ghRateLimit struct {
	Resources map[string]ghRateLimitResource `json:"resources"`
}

type ghRateLimitResource struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"`
	Used      int   `json:"used"`
}

type orgBilling struct {
	SeatBreakdown struct {
		Total               int `json:"total"`
		AddedThisCycle      int `json:"added_this_cycle"`
		PendingCancellation int `json:"pending_cancellation"`
		PendingInvitation   int `json:"pending_invitation"`
		ActiveThisCycle     int `json:"active_this_cycle"`
		InactiveThisCycle   int `json:"inactive_this_cycle"`
	} `json:"seat_breakdown"`
	PlanType              string `json:"plan_type"`
	SeatManagementSetting string `json:"seat_management_setting"`
	PublicCodeSuggestions string `json:"public_code_suggestions"`
	IDEChat               string `json:"ide_chat"`
	PlatformChat          string `json:"platform_chat"`
	CLI                   string `json:"cli"`
}

type orgMetricsDay struct {
	Date              string          `json:"date"`
	TotalActiveUsers  int             `json:"total_active_users"`
	TotalEngagedUsers int             `json:"total_engaged_users"`
	Completions       *orgCompletions `json:"copilot_ide_code_completions"`
	IDEChat           *orgChat        `json:"copilot_ide_chat"`
	DotcomChat        *orgChat        `json:"copilot_dotcom_chat"`
}

type orgCompletions struct {
	TotalEngagedUsers int               `json:"total_engaged_users"`
	Editors           []orgEditorMetric `json:"editors"`
}

type orgChat struct {
	TotalEngagedUsers int               `json:"total_engaged_users"`
	Editors           []orgEditorMetric `json:"editors"`
}

type orgEditorMetric struct {
	Name   string           `json:"name"`
	Models []orgModelMetric `json:"models"`
}

type orgModelMetric struct {
	Name                string `json:"name"`
	IsCustomModel       bool   `json:"is_custom_model"`
	TotalEngagedUsers   int    `json:"total_engaged_users"`
	TotalSuggestions    int    `json:"total_code_suggestions,omitempty"`
	TotalAcceptances    int    `json:"total_code_acceptances,omitempty"`
	TotalLinesAccepted  int    `json:"total_code_lines_accepted,omitempty"`
	TotalLinesSuggested int    `json:"total_code_lines_suggested,omitempty"`
	TotalChats          int    `json:"total_chats,omitempty"`
	TotalChatCopy       int    `json:"total_chat_copy_events,omitempty"`
	TotalChatInsert     int    `json:"total_chat_insertion_events,omitempty"`
}

type copilotConfig struct {
	Model           string   `json:"model"`
	Banner          string   `json:"banner"`
	ReasoningEffort string   `json:"reasoning_effort"`
	RenderMarkdown  bool     `json:"render_markdown"`
	Experimental    bool     `json:"experimental"`
	AskedSetupTerms []string `json:"asked_setup_terminals"`
}

type sessionEvent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type sessionStartData struct {
	SessionID      string `json:"sessionId"`
	CopilotVersion string `json:"copilotVersion"`
	StartTime      string `json:"startTime"`
	Context        struct {
		CWD        string `json:"cwd"`
		GitRoot    string `json:"gitRoot"`
		Branch     string `json:"branch"`
		Repository string `json:"repository"`
	} `json:"context"`
}

type modelChangeData struct {
	OldModel string `json:"oldModel"`
	NewModel string `json:"newModel"`
}

type sessionInfoData struct {
	InfoType string `json:"infoType"`
	Message  string `json:"message"`
}

type sessionWorkspace struct {
	ID        string `yaml:"id" json:"id"`
	CWD       string `yaml:"cwd" json:"cwd"`
	GitRoot   string `yaml:"git_root" json:"git_root"`
	Repo      string `yaml:"repository" json:"repository"`
	Branch    string `yaml:"branch" json:"branch"`
	Summary   string `yaml:"summary" json:"summary"`
	CreatedAt string `yaml:"created_at" json:"created_at"`
	UpdatedAt string `yaml:"updated_at" json:"updated_at"`
}

type logTokenEntry struct {
	Timestamp time.Time
	Used      int
	Total     int
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	binary := acct.Binary
	if binary == "" {
		binary = "gh"
	}

	if _, err := exec.LookPath(binary); err != nil {
		return core.UsageSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusError,
			Message:    fmt.Sprintf("%s binary not found in PATH", binary),
		}, nil
	}

	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	if vOut, err := runGH(ctx, binary, "copilot", "--version"); err == nil {
		snap.Raw["copilot_version"] = strings.TrimSpace(vOut)
	} else {
		snap.Status = core.StatusError
		snap.Message = "gh copilot extension not available"
		return snap, nil
	}

	authOut, authErr := runGH(ctx, binary, "auth", "status")
	authOutput := authOut
	if authErr != nil {
		authOutput = authOut
	}
	snap.Raw["auth_status"] = strings.TrimSpace(authOutput)

	if authErr != nil {
		snap.Status = core.StatusAuth
		snap.Message = "not authenticated with GitHub"
		return snap, nil
	}

	p.fetchUserInfo(ctx, binary, &snap)

	p.fetchCopilotInternalUser(ctx, binary, &snap)

	p.fetchRateLimits(ctx, binary, &snap)

	p.fetchOrgData(ctx, binary, &snap)

	p.fetchLocalData(acct, &snap)

	p.resolveStatus(&snap, authOutput)

	return snap, nil
}

func (p *Provider) fetchUserInfo(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	userJSON, err := runGH(ctx, binary, "api", "/user")
	if err != nil {
		return
	}
	var user ghUser
	if json.Unmarshal([]byte(userJSON), &user) != nil {
		return
	}
	if user.Login != "" {
		snap.Raw["github_login"] = user.Login
	}
	if user.Name != "" {
		snap.Raw["github_name"] = user.Name
	}
	if user.Plan.Name != "" {
		snap.Raw["github_plan"] = user.Plan.Name
	}
}

func (p *Provider) fetchCopilotInternalUser(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	body, err := runGH(ctx, binary, "api", "/copilot_internal/user")
	if err != nil {
		return
	}
	var cu copilotInternalUser
	if json.Unmarshal([]byte(body), &cu) != nil {
		return
	}
	p.applyCopilotInternalUser(&cu, snap)
}

func (p *Provider) applyCopilotInternalUser(cu *copilotInternalUser, snap *core.UsageSnapshot) {
	if cu == nil {
		return
	}

	snap.Raw["copilot_plan"] = cu.CopilotPlan
	snap.Raw["access_type_sku"] = cu.AccessTypeSKU
	if cu.AssignedDate != "" {
		snap.Raw["assigned_date"] = cu.AssignedDate
	}
	if cu.CodexAgentEnabled {
		snap.Raw["codex_agent_enabled"] = "true"
	}
	if cu.UsageResetDate != "" {
		snap.Raw["quota_reset_date"] = cu.UsageResetDate
	}
	if cu.UsageResetDateUTC != "" {
		snap.Raw["quota_reset_date_utc"] = cu.UsageResetDateUTC
	}

	features := []string{}
	if cu.ChatEnabled {
		features = append(features, "chat")
	}
	if cu.MCPEnabled {
		features = append(features, "mcp")
	}
	if cu.CopilotIgnoreEnabled {
		features = append(features, "copilotignore")
	}
	if len(features) > 0 {
		snap.Raw["features_enabled"] = strings.Join(features, ", ")
	}

	if api, ok := cu.Endpoints["api"]; ok {
		snap.Raw["api_endpoint"] = api
	}

	if len(cu.OrganizationLoginList) > 0 {
		snap.Raw["copilot_orgs"] = strings.Join(cu.OrganizationLoginList, ", ")
	}
	for _, org := range cu.OrganizationList {
		key := fmt.Sprintf("org_%s_plan", org.Login)
		snap.Raw[key] = org.CopilotPlan
		if org.IsEnterprise {
			snap.Raw[fmt.Sprintf("org_%s_enterprise", org.Login)] = "true"
		}
	}

	if !p.applyUsageSnapshotMetrics(cu.UsageSnapshots, snap) {
		p.applyLegacyUsageMetrics(cu, snap)
	}

	for _, candidate := range []string{cu.UsageResetDateUTC, cu.UsageResetDate, cu.LimitedUserResetDate} {
		if t := parseCopilotTime(candidate); !t.IsZero() {
			snap.Resets["quota_reset"] = t
			break
		}
	}
}

func (p *Provider) applyLegacyUsageMetrics(cu *copilotInternalUser, snap *core.UsageSnapshot) {
	if cu.MonthlyUsage != nil && cu.MonthlyUsage.Chat != nil {
		limit := float64(*cu.MonthlyUsage.Chat)
		remaining := float64(0)
		if cu.LimitedUserUsage != nil && cu.LimitedUserUsage.Chat != nil {
			remaining = float64(*cu.LimitedUserUsage.Chat)
		}
		used := limit - remaining
		snap.Metrics["chat_quota"] = core.Metric{
			Limit:     &limit,
			Remaining: &remaining,
			Used:      &used,
			Unit:      "messages",
			Window:    "month",
		}
	}

	if cu.MonthlyUsage != nil && cu.MonthlyUsage.Completions != nil {
		limit := float64(*cu.MonthlyUsage.Completions)
		remaining := float64(0)
		if cu.LimitedUserUsage != nil && cu.LimitedUserUsage.Completions != nil {
			remaining = float64(*cu.LimitedUserUsage.Completions)
		}
		used := limit - remaining
		snap.Metrics["completions_quota"] = core.Metric{
			Limit:     &limit,
			Remaining: &remaining,
			Used:      &used,
			Unit:      "completions",
			Window:    "month",
		}
	}
}

func (p *Provider) applyUsageSnapshotMetrics(snapshots *copilotUsageSnapshots, snap *core.UsageSnapshot) bool {
	if snapshots == nil {
		return false
	}

	applied := false
	if p.applySingleUsageSnapshot("chat_quota", "messages", snapshots.Chat, snap) {
		applied = true
	}
	if p.applySingleUsageSnapshot("completions_quota", "completions", snapshots.Completions, snap) {
		applied = true
	}
	if p.applySingleUsageSnapshot("premium_interactions_quota", "requests", snapshots.PremiumInteractions, snap) {
		applied = true
	}
	return applied
}

func (p *Provider) applySingleUsageSnapshot(key, unit string, quota *copilotUsageSnapshot, snap *core.UsageSnapshot) bool {
	if quota == nil {
		return false
	}

	if quota.UsageID != "" {
		snap.Raw[key+"_id"] = quota.UsageID
	}
	if quota.OveragePermitted != nil {
		snap.Raw[key+"_overage_permitted"] = strconv.FormatBool(*quota.OveragePermitted)
	}
	if quota.Unlimited != nil && *quota.Unlimited {
		snap.Raw[key+"_unlimited"] = "true"
		return false
	}
	if quota.TimestampUTC != "" {
		if t := parseCopilotTime(quota.TimestampUTC); !t.IsZero() {
			snap.Resets[key+"_snapshot"] = t
		}
	}

	remaining := firstNonNilFloat(quota.UsageRemaining, quota.Remaining)
	limit := quota.Entitlement
	pct := clampPercent(firstFloat(quota.PercentRemaining))

	switch {
	case limit != nil && remaining != nil:
		used := *limit - *remaining
		if used < 0 {
			used = 0
		}
		snap.Metrics[key] = core.Metric{
			Limit:     float64Ptr(*limit),
			Remaining: float64Ptr(*remaining),
			Used:      float64Ptr(used),
			Unit:      unit,
			Window:    "month",
		}
		return true
	case pct >= 0:
		limitPct := 100.0
		used := 100 - pct
		snap.Metrics[key] = core.Metric{
			Limit:     &limitPct,
			Remaining: float64Ptr(pct),
			Used:      float64Ptr(used),
			Unit:      "%",
			Window:    "month",
		}
		return true
	case remaining != nil:
		snap.Metrics[key] = core.Metric{
			Used:   float64Ptr(*remaining),
			Unit:   unit,
			Window: "month",
		}
		return true
	default:
		return false
	}
}

func (p *Provider) fetchRateLimits(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	body, err := runGH(ctx, binary, "api", "/rate_limit")
	if err != nil {
		return
	}
	var rl ghRateLimit
	if json.Unmarshal([]byte(body), &rl) != nil {
		return
	}

	for _, resource := range []string{"core", "search", "graphql"} {
		res, ok := rl.Resources[resource]
		if !ok || res.Limit == 0 {
			continue
		}
		limit := float64(res.Limit)
		remaining := float64(res.Remaining)
		used := float64(res.Used)
		if used == 0 && res.Remaining >= 0 && res.Remaining <= res.Limit {
			used = limit - remaining
		}
		key := "gh_" + resource + "_rpm"
		snap.Metrics[key] = core.Metric{
			Limit:     &limit,
			Remaining: &remaining,
			Used:      &used,
			Unit:      "requests",
			Window:    "1h",
		}
		if res.Reset > 0 {
			snap.Resets[key+"_reset"] = time.Unix(res.Reset, 0)
		}
	}
}

func (p *Provider) fetchOrgData(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	orgs := snap.Raw["copilot_orgs"]
	if orgs == "" {
		return
	}

	for _, org := range strings.Split(orgs, ", ") {
		org = strings.TrimSpace(org)
		if org == "" {
			continue
		}
		p.fetchOrgBilling(ctx, binary, org, snap)
		p.fetchOrgMetrics(ctx, binary, org, snap)
	}
}

func (p *Provider) fetchOrgBilling(ctx context.Context, binary, org string, snap *core.UsageSnapshot) {
	body, err := runGH(ctx, binary, "api", fmt.Sprintf("/orgs/%s/copilot/billing", org))
	if err != nil {
		return
	}
	var billing orgBilling
	if json.Unmarshal([]byte(body), &billing) != nil {
		return
	}

	prefix := fmt.Sprintf("org_%s_", org)
	snap.Raw[prefix+"billing_plan"] = billing.PlanType
	snap.Raw[prefix+"seat_mgmt"] = billing.SeatManagementSetting
	snap.Raw[prefix+"ide_chat"] = billing.IDEChat
	snap.Raw[prefix+"platform_chat"] = billing.PlatformChat
	snap.Raw[prefix+"cli"] = billing.CLI
	snap.Raw[prefix+"public_code"] = billing.PublicCodeSuggestions

	if billing.SeatBreakdown.Total > 0 {
		total := float64(billing.SeatBreakdown.Total)
		active := float64(billing.SeatBreakdown.ActiveThisCycle)
		inactive := total - active
		snap.Metrics[prefix+"seats"] = core.Metric{
			Limit:  &total,
			Used:   &active,
			Unit:   "seats",
			Window: "cycle",
		}
		_ = inactive
	}
}

func (p *Provider) fetchOrgMetrics(ctx context.Context, binary, org string, snap *core.UsageSnapshot) {
	body, err := runGH(ctx, binary, "api", fmt.Sprintf("/orgs/%s/copilot/metrics", org))
	if err != nil {
		return
	}
	var days []orgMetricsDay
	if json.Unmarshal([]byte(body), &days) != nil {
		return
	}
	if len(days) == 0 {
		return
	}

	prefix := "org_" + org + "_"
	activeUsers := make([]core.TimePoint, 0, len(days))
	engagedUsers := make([]core.TimePoint, 0, len(days))
	totalSuggestions := make([]core.TimePoint, 0, len(days))
	totalAcceptances := make([]core.TimePoint, 0, len(days))
	totalChats := make([]core.TimePoint, 0, len(days))
	aggSuggestions := 0.0
	aggAcceptances := 0.0
	aggChats := 0.0

	for _, day := range days {
		activeUsers = append(activeUsers, core.TimePoint{Date: day.Date, Value: float64(day.TotalActiveUsers)})
		engagedUsers = append(engagedUsers, core.TimePoint{Date: day.Date, Value: float64(day.TotalEngagedUsers)})

		var daySugg, dayAccept float64
		if day.Completions != nil {
			for _, editor := range day.Completions.Editors {
				for _, model := range editor.Models {
					daySugg += float64(model.TotalSuggestions)
					dayAccept += float64(model.TotalAcceptances)
				}
			}
		}
		totalSuggestions = append(totalSuggestions, core.TimePoint{Date: day.Date, Value: daySugg})
		totalAcceptances = append(totalAcceptances, core.TimePoint{Date: day.Date, Value: dayAccept})
		aggSuggestions += daySugg
		aggAcceptances += dayAccept

		var dayChats float64
		if day.IDEChat != nil {
			for _, editor := range day.IDEChat.Editors {
				for _, model := range editor.Models {
					dayChats += float64(model.TotalChats)
				}
			}
		}
		if day.DotcomChat != nil {
			for _, editor := range day.DotcomChat.Editors {
				for _, model := range editor.Models {
					dayChats += float64(model.TotalChats)
				}
			}
		}
		totalChats = append(totalChats, core.TimePoint{Date: day.Date, Value: dayChats})
		aggChats += dayChats
	}

	snap.DailySeries[prefix+"active_users"] = activeUsers
	snap.DailySeries[prefix+"engaged_users"] = engagedUsers
	snap.DailySeries[prefix+"suggestions"] = totalSuggestions
	snap.DailySeries[prefix+"acceptances"] = totalAcceptances
	snap.DailySeries[prefix+"chats"] = totalChats

	if len(activeUsers) > 0 {
		lastActive := activeUsers[len(activeUsers)-1].Value
		snap.Metrics[prefix+"active_users"] = core.Metric{Used: float64Ptr(lastActive), Unit: "users", Window: "day"}
	}
	if len(engagedUsers) > 0 {
		lastEngaged := engagedUsers[len(engagedUsers)-1].Value
		snap.Metrics[prefix+"engaged_users"] = core.Metric{Used: float64Ptr(lastEngaged), Unit: "users", Window: "day"}
	}
	if aggSuggestions > 0 {
		snap.Metrics[prefix+"suggestions"] = core.Metric{Used: float64Ptr(aggSuggestions), Unit: "suggestions", Window: "series"}
	}
	if aggAcceptances > 0 {
		snap.Metrics[prefix+"acceptances"] = core.Metric{Used: float64Ptr(aggAcceptances), Unit: "acceptances", Window: "series"}
	}
	if aggChats > 0 {
		snap.Metrics[prefix+"chats"] = core.Metric{Used: float64Ptr(aggChats), Unit: "chats", Window: "series"}
	}
}

func (p *Provider) fetchLocalData(acct core.AccountConfig, snap *core.UsageSnapshot) {
	if acct.ExtraData != nil {
		if dir := strings.TrimSpace(acct.ExtraData["config_dir"]); dir != "" {
			p.readConfig(dir, snap)
			logData := p.readLogs(dir, snap)
			p.readSessions(dir, snap, logData)
			return
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	copilotDir := filepath.Join(home, ".copilot")

	p.readConfig(copilotDir, snap)

	logData := p.readLogs(copilotDir, snap)

	p.readSessions(copilotDir, snap, logData)
}

func (p *Provider) readConfig(copilotDir string, snap *core.UsageSnapshot) {
	data, err := os.ReadFile(filepath.Join(copilotDir, "config.json"))
	if err != nil {
		return
	}
	var cfg copilotConfig
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	if cfg.Model != "" {
		snap.Raw["preferred_model"] = cfg.Model
	}
	if cfg.ReasoningEffort != "" {
		snap.Raw["reasoning_effort"] = cfg.ReasoningEffort
	}
	if cfg.Experimental {
		snap.Raw["experimental"] = "enabled"
	}
}

type logSummary struct {
	DefaultModel  string
	SessionTokens map[string]logTokenEntry // sessionID → last CompactionProcessor entry
	SessionBurn   map[string]float64       // sessionID → cumulative positive token deltas
}

func (p *Provider) readLogs(copilotDir string, snap *core.UsageSnapshot) logSummary {
	ls := logSummary{
		SessionTokens: make(map[string]logTokenEntry),
		SessionBurn:   make(map[string]float64),
	}
	logDir := filepath.Join(copilotDir, "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return ls
	}

	var allTokenEntries []logTokenEntry

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logDir, entry.Name()))
		if err != nil {
			continue
		}

		var currentSessionID string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)

			if strings.Contains(line, "Workspace initialized:") {
				if idx := strings.Index(line, "Workspace initialized:"); idx >= 0 {
					rest := strings.TrimSpace(line[idx+len("Workspace initialized:"):])
					if spIdx := strings.Index(rest, " "); spIdx > 0 {
						currentSessionID = rest[:spIdx]
					} else if rest != "" {
						currentSessionID = rest
					}
				}
			}

			if strings.Contains(line, "Using default model:") {
				if idx := strings.Index(line, "Using default model:"); idx >= 0 {
					m := strings.TrimSpace(line[idx+len("Using default model:"):])
					if m != "" {
						ls.DefaultModel = m
					}
				}
			}

			if strings.Contains(line, "CompactionProcessor: Utilization") {
				te := parseCompactionLine(line)
				if te.Total > 0 {
					allTokenEntries = append(allTokenEntries, te)
					if currentSessionID != "" {
						if prev, ok := ls.SessionTokens[currentSessionID]; ok && prev.Total > 0 {
							delta := te.Used - prev.Used
							if delta > 0 {
								ls.SessionBurn[currentSessionID] += float64(delta)
							}
						} else if te.Used > 0 {
							ls.SessionBurn[currentSessionID] += float64(te.Used)
						}
						ls.SessionTokens[currentSessionID] = te
					}
				}
			}
		}
	}

	if ls.DefaultModel != "" {
		snap.Raw["default_model"] = ls.DefaultModel
	}

	if len(allTokenEntries) > 0 {
		last := allTokenEntries[len(allTokenEntries)-1]
		snap.Raw["context_window_tokens"] = fmt.Sprintf("%d/%d", last.Used, last.Total)
		pct := float64(last.Used) / float64(last.Total) * 100
		snap.Raw["context_window_pct"] = fmt.Sprintf("%.1f%%", pct)
		used := float64(last.Used)
		limit := float64(last.Total)
		snap.Metrics["context_window"] = core.Metric{
			Limit:     &limit,
			Used:      &used,
			Remaining: float64Ptr(limit - used),
			Unit:      "tokens",
			Window:    "session",
		}
	}

	return ls
}

type assistantMsgData struct {
	Content      string          `json:"content"`
	ReasoningTxt string          `json:"reasoningText"`
	ToolRequests json.RawMessage `json:"toolRequests"`
}

func (p *Provider) readSessions(copilotDir string, snap *core.UsageSnapshot, logs logSummary) {
	sessionDir := filepath.Join(copilotDir, "session-state")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return
	}

	snap.Raw["total_sessions"] = fmt.Sprintf("%d", len(entries))

	type sessionInfo struct {
		id             string
		createdAt      time.Time
		updatedAt      time.Time
		cwd            string
		repo           string
		branch         string
		client         string
		summary        string
		messages       int
		turns          int
		model          string
		responseChars  int
		reasoningChars int
		toolCalls      int
		tokenUsed      int
		tokenTotal     int
		tokenBurn      float64
	}

	var sessions []sessionInfo
	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	dailyTokens := make(map[string]float64)
	modelMessages := make(map[string]int)
	modelTurns := make(map[string]int)
	modelSessions := make(map[string]int)
	modelResponseChars := make(map[string]int)
	modelReasoningChars := make(map[string]int)
	modelToolCalls := make(map[string]int)
	dailyModelMessages := make(map[string]map[string]float64)
	dailyModelTokens := make(map[string]map[string]float64)
	modelInputTokens := make(map[string]float64)
	toolUsageCounts := make(map[string]int)
	clientLabels := make(map[string]string)
	clientTokens := make(map[string]float64)
	clientSessions := make(map[string]int)
	clientMessages := make(map[string]int)
	dailyClientTokens := make(map[string]map[string]float64)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		si := sessionInfo{id: entry.Name()}
		sessPath := filepath.Join(sessionDir, entry.Name())

		if wsData, err := os.ReadFile(filepath.Join(sessPath, "workspace.yaml")); err == nil {
			ws := parseSimpleYAML(string(wsData))
			si.cwd = ws["cwd"]
			si.repo = ws["repository"]
			si.branch = ws["branch"]
			si.summary = ws["summary"]
			si.createdAt = flexParseTime(ws["created_at"])
			si.updatedAt = flexParseTime(ws["updated_at"])
		}

		if te, ok := logs.SessionTokens[si.id]; ok {
			si.tokenUsed = te.Used
			si.tokenTotal = te.Total
		}
		if burn, ok := logs.SessionBurn[si.id]; ok {
			si.tokenBurn = burn
		}

		if evtData, err := os.ReadFile(filepath.Join(sessPath, "events.jsonl")); err == nil {
			currentModel := logs.DefaultModel
			lines := strings.Split(string(evtData), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var evt sessionEvent
				if json.Unmarshal([]byte(line), &evt) != nil {
					continue
				}

				switch evt.Type {
				case "session.start":
					var start sessionStartData
					if json.Unmarshal(evt.Data, &start) == nil {
						if si.cwd == "" {
							si.cwd = start.Context.CWD
						}
						if si.repo == "" {
							si.repo = start.Context.Repository
						}
						if si.branch == "" {
							si.branch = start.Context.Branch
						}
						if si.createdAt.IsZero() {
							si.createdAt = flexParseTime(start.StartTime)
						}
					}

				case "session.model_change":
					var mc modelChangeData
					if json.Unmarshal(evt.Data, &mc) == nil && mc.NewModel != "" {
						currentModel = mc.NewModel
					}

				case "session.info":
					var info sessionInfoData
					if json.Unmarshal(evt.Data, &info) == nil && info.InfoType == "model" {
						if m := extractModelFromInfoMsg(info.Message); m != "" {
							currentModel = m
						}
					}

				case "user.message":
					si.messages++
					day := parseDayFromTimestamp(evt.Timestamp)
					if day != "" {
						dailyMessages[day]++
					}
					if currentModel != "" {
						modelMessages[currentModel]++
						if day != "" {
							if dailyModelMessages[currentModel] == nil {
								dailyModelMessages[currentModel] = make(map[string]float64)
							}
							dailyModelMessages[currentModel][day]++
						}
					}

				case "assistant.turn_start":
					si.turns++
					if currentModel != "" {
						modelTurns[currentModel]++
					}

				case "assistant.message":
					var msg assistantMsgData
					if json.Unmarshal(evt.Data, &msg) == nil {
						si.responseChars += len(msg.Content)
						si.reasoningChars += len(msg.ReasoningTxt)
						if currentModel != "" {
							modelResponseChars[currentModel] += len(msg.Content)
							modelReasoningChars[currentModel] += len(msg.ReasoningTxt)
						}
						var tools []json.RawMessage
						if json.Unmarshal(msg.ToolRequests, &tools) == nil && len(tools) > 0 {
							si.toolCalls += len(tools)
							if currentModel != "" {
								modelToolCalls[currentModel] += len(tools)
							}
							for _, toolReq := range tools {
								toolName := extractCopilotToolName(toolReq)
								if toolName == "" {
									toolName = "unknown"
								}
								toolUsageCounts[toolName]++
							}
							day := parseDayFromTimestamp(evt.Timestamp)
							if day != "" {
								dailyToolCalls[day] += float64(len(tools))
							}
						}
					}
				}
			}
			si.model = currentModel
		}

		day := dayForSession(si.createdAt, si.updatedAt)
		if si.model != "" {
			modelSessions[si.model]++
		}
		if day != "" {
			dailySessions[day]++
		}

		clientLabel := normalizeCopilotClient(si.repo, si.cwd)
		clientKey := sanitizeMetricName(clientLabel)
		if clientKey == "" {
			clientKey = "cli"
		}
		si.client = clientLabel
		if _, ok := clientLabels[clientKey]; !ok {
			clientLabels[clientKey] = clientLabel
		}
		clientSessions[clientKey]++
		clientMessages[clientKey] += si.messages

		sessionTokens := float64(si.tokenUsed)
		if si.tokenBurn > 0 {
			sessionTokens = si.tokenBurn
		}
		if sessionTokens > 0 {
			clientTokens[clientKey] += sessionTokens
			if day != "" {
				dailyTokens[day] += sessionTokens
				if dailyClientTokens[clientKey] == nil {
					dailyClientTokens[clientKey] = make(map[string]float64)
				}
				dailyClientTokens[clientKey][day] += sessionTokens
			}
			if si.model != "" {
				modelInputTokens[si.model] += sessionTokens
				if day != "" {
					if dailyModelTokens[si.model] == nil {
						dailyModelTokens[si.model] = make(map[string]float64)
					}
					dailyModelTokens[si.model][day] += sessionTokens
				}
			}
		}
		sessions = append(sessions, si)
	}

	storeSeries(snap, "messages", dailyMessages)
	storeSeries(snap, "sessions", dailySessions)
	storeSeries(snap, "tool_calls", dailyToolCalls)
	storeSeries(snap, "tokens_total", dailyTokens)
	storeSeries(snap, "cli_messages", dailyMessages)
	storeSeries(snap, "cli_sessions", dailySessions)
	storeSeries(snap, "cli_tool_calls", dailyToolCalls)
	for model, dayCounts := range dailyModelMessages {
		safe := sanitizeMetricName(model)
		storeSeries(snap, "cli_messages_"+safe, dayCounts)
	}
	for model, dayCounts := range dailyModelTokens {
		safe := sanitizeMetricName(model)
		storeSeries(snap, "tokens_"+safe, dayCounts)
		storeSeries(snap, "cli_tokens_"+safe, dayCounts)
	}

	setRawStr(snap, "model_usage", formatModelMap(modelMessages, "msgs"))
	setRawStr(snap, "model_turns", formatModelMap(modelTurns, "turns"))
	setRawStr(snap, "model_sessions", formatModelMapPlain(modelSessions))
	setRawStr(snap, "model_response_chars", formatModelMap(modelResponseChars, "chars"))
	setRawStr(snap, "model_reasoning_chars", formatModelMap(modelReasoningChars, "chars"))
	setRawStr(snap, "model_tool_calls", formatModelMap(modelToolCalls, "calls"))

	sort.Slice(sessions, func(i, j int) bool {
		ti := sessions[i].updatedAt
		if ti.IsZero() {
			ti = sessions[i].createdAt
		}
		tj := sessions[j].updatedAt
		if tj.IsZero() {
			tj = sessions[j].createdAt
		}
		return ti.After(tj)
	})

	var totalMessages, totalTurns, totalResponse, totalReasoning, totalTools int
	totalTokens := 0.0
	for _, s := range sessions {
		totalMessages += s.messages
		totalTurns += s.turns
		totalResponse += s.responseChars
		totalReasoning += s.reasoningChars
		totalTools += s.toolCalls
		tokens := float64(s.tokenUsed)
		if s.tokenBurn > 0 {
			tokens = s.tokenBurn
		}
		totalTokens += tokens
	}
	setRawInt(snap, "total_cli_messages", totalMessages)
	setRawInt(snap, "total_cli_turns", totalTurns)
	setRawInt(snap, "total_response_chars", totalResponse)
	setRawInt(snap, "total_reasoning_chars", totalReasoning)
	setRawInt(snap, "total_tool_calls", totalTools)

	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", copilotAllTimeWindow)
	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "total_turns", float64(totalTurns), "turns", copilotAllTimeWindow)
	setUsedMetric(snap, "total_tool_calls", float64(totalTools), "calls", copilotAllTimeWindow)
	setUsedMetric(snap, "total_response_chars", float64(totalResponse), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "total_reasoning_chars", float64(totalReasoning), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "total_conversations", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_messages", float64(totalMessages), "messages", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_turns", float64(totalTurns), "turns", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_sessions", float64(len(sessions)), "sessions", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_tool_calls", float64(totalTools), "calls", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_response_chars", float64(totalResponse), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_reasoning_chars", float64(totalReasoning), "chars", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_input_tokens", totalTokens, "tokens", copilotAllTimeWindow)
	setUsedMetric(snap, "cli_total_tokens", totalTokens, "tokens", copilotAllTimeWindow)

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

	topModels := topModelNames(modelInputTokens, modelMessages, maxCopilotModels)
	for _, model := range topModels {
		prefix := "model_" + sanitizeMetricName(model)
		rec := core.ModelUsageRecord{
			RawModelID: model,
			RawSource:  "json",
			Window:     copilotAllTimeWindow,
		}
		setUsedMetric(snap, prefix+"_input_tokens", modelInputTokens[model], "tokens", copilotAllTimeWindow)
		if modelInputTokens[model] > 0 {
			rec.InputTokens = core.Float64Ptr(modelInputTokens[model])
			rec.TotalTokens = core.Float64Ptr(modelInputTokens[model])
		}
		setUsedMetric(snap, prefix+"_messages", float64(modelMessages[model]), "messages", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_turns", float64(modelTurns[model]), "turns", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_sessions", float64(modelSessions[model]), "sessions", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_tool_calls", float64(modelToolCalls[model]), "calls", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_response_chars", float64(modelResponseChars[model]), "chars", copilotAllTimeWindow)
		setUsedMetric(snap, prefix+"_reasoning_chars", float64(modelReasoningChars[model]), "chars", copilotAllTimeWindow)
		core.AppendModelUsageRecord(snap, rec)
	}

	topClients := topCopilotClientNames(clientTokens, clientSessions, clientMessages, maxCopilotClients)
	for _, client := range topClients {
		clientPrefix := "client_" + client
		setUsedMetric(snap, clientPrefix+"_total_tokens", clientTokens[client], "tokens", copilotAllTimeWindow)
		setUsedMetric(snap, clientPrefix+"_input_tokens", clientTokens[client], "tokens", copilotAllTimeWindow)
		setUsedMetric(snap, clientPrefix+"_sessions", float64(clientSessions[client]), "sessions", copilotAllTimeWindow)
		if byDay := dailyClientTokens[client]; len(byDay) > 0 {
			storeSeries(snap, "tokens_client_"+client, byDay)
		}
	}
	setRawStr(snap, "client_usage", formatCopilotClientUsage(topClients, clientLabels, clientTokens, clientSessions))
	setRawStr(snap, "tool_usage", formatModelMap(toolUsageCounts, "calls"))
	for toolName, count := range toolUsageCounts {
		if count <= 0 {
			continue
		}
		setUsedMetric(snap, "tool_"+sanitizeMetricName(toolName), float64(count), "calls", copilotAllTimeWindow)
	}

	if len(sessions) > 0 {
		r := sessions[0]
		if r.client != "" {
			snap.Raw["last_session_client"] = r.client
		}
		snap.Raw["last_session_repo"] = r.repo
		snap.Raw["last_session_branch"] = r.branch
		if r.summary != "" {
			snap.Raw["last_session_summary"] = r.summary
		}
		if !r.updatedAt.IsZero() {
			snap.Raw["last_session_time"] = r.updatedAt.Format(time.RFC3339)
		}
		if r.model != "" {
			snap.Raw["last_session_model"] = r.model
		}
		sessionTokens := float64(r.tokenUsed)
		if r.tokenBurn > 0 {
			sessionTokens = r.tokenBurn
		}
		if sessionTokens > 0 {
			snap.Raw["last_session_tokens"] = fmt.Sprintf("%.0f/%d", sessionTokens, r.tokenTotal)
			setUsedMetric(snap, "session_input_tokens", sessionTokens, "tokens", "session")
			setUsedMetric(snap, "session_total_tokens", sessionTokens, "tokens", "session")
			if r.tokenTotal > 0 {
				limit := float64(r.tokenTotal)
				snap.Metrics["context_window"] = core.Metric{
					Limit:     &limit,
					Used:      float64Ptr(sessionTokens),
					Remaining: float64Ptr(maxFloat(limit-sessionTokens, 0)),
					Unit:      "tokens",
					Window:    "session",
				}
			}
		}
	}
}

func parseCompactionLine(line string) logTokenEntry {
	var entry logTokenEntry

	if len(line) >= 24 {
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", line[:24]); err == nil {
			entry.Timestamp = t
		}
	}

	parenStart := strings.Index(line, "(")
	parenEnd := strings.Index(line, " tokens)")
	if parenStart >= 0 && parenEnd > parenStart {
		inner := line[parenStart+1 : parenEnd]
		parts := strings.Split(inner, "/")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &entry.Used)
			fmt.Sscanf(parts[1], "%d", &entry.Total)
		}
	}

	return entry
}

func (p *Provider) resolveStatus(snap *core.UsageSnapshot, authOutput string) {
	lower := strings.ToLower(authOutput)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit") {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited"
		return
	}

	for key, m := range snap.Metrics {
		pct := m.Percent()
		isUsageMetric := key == "chat_quota" || key == "completions_quota" || key == "premium_interactions_quota"
		if pct >= 0 && pct < 5 && isUsageMetric {
			snap.Status = core.StatusLimited
			snap.Message = usageStatusMessage(snap)
			return
		}
		if pct >= 0 && pct < 20 && isUsageMetric {
			snap.Status = core.StatusNearLimit
			snap.Message = usageStatusMessage(snap)
			return
		}
	}

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = usageStatusMessage(snap)
	}
}

func usageStatusMessage(snap *core.UsageSnapshot) string {
	parts := []string{}

	login := snap.Raw["github_login"]
	if login != "" {
		parts = append(parts, fmt.Sprintf("Copilot (%s)", login))
	} else {
		parts = append(parts, "Copilot")
	}

	sku := snap.Raw["access_type_sku"]
	plan := snap.Raw["copilot_plan"]
	if sku != "" {
		parts = append(parts, skuLabel(sku))
	} else if plan != "" {
		parts = append(parts, plan)
	}

	return strings.Join(parts, " · ")
}

func skuLabel(sku string) string {
	switch {
	case strings.Contains(sku, "free"):
		return "Free"
	case strings.Contains(sku, "pro_plus") || strings.Contains(sku, "pro+"):
		return "Pro+"
	case strings.Contains(sku, "pro"):
		return "Pro"
	case strings.Contains(sku, "business"):
		return "Business"
	case strings.Contains(sku, "enterprise"):
		return "Enterprise"
	default:
		return sku
	}
}

func runGH(ctx context.Context, binary string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}

func parseSimpleYAML(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		result[key] = val
	}
	return result
}

func mapToSeries(m map[string]float64) []core.TimePoint {
	pts := make([]core.TimePoint, 0, len(m))
	for date, val := range m {
		pts = append(pts, core.TimePoint{Date: date, Value: val})
	}
	sort.Slice(pts, func(i, j int) bool {
		return pts[i].Date < pts[j].Date
	})
	return pts
}

func storeSeries(snap *core.UsageSnapshot, key string, m map[string]float64) {
	if len(m) > 0 {
		snap.DailySeries[key] = mapToSeries(m)
	}
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

func dayForSession(createdAt, updatedAt time.Time) string {
	if !updatedAt.IsZero() {
		return updatedAt.Format("2006-01-02")
	}
	if !createdAt.IsZero() {
		return createdAt.Format("2006-01-02")
	}
	return ""
}

func latestSeriesValue(m map[string]float64) (string, float64) {
	if len(m) == 0 {
		return "", 0
	}
	dates := make([]string, 0, len(m))
	for d := range m {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	last := dates[len(dates)-1]
	return last, m[last]
}

func sumLastNDays(m map[string]float64, days int) float64 {
	if len(m) == 0 || days <= 0 {
		return 0
	}
	date, _ := latestSeriesValue(m)
	if date == "" {
		return 0
	}
	end, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	start := end.AddDate(0, 0, -(days - 1))
	sum := 0.0
	for d, v := range m {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if !t.Before(start) && !t.After(end) {
			sum += v
		}
	}
	return sum
}

func topModelNames(tokenMap map[string]float64, messageMap map[string]int, limit int) []string {
	type row struct {
		model    string
		tokens   float64
		messages int
	}

	seen := make(map[string]bool)
	var rows []row
	for model, tokens := range tokenMap {
		seen[model] = true
		rows = append(rows, row{model: model, tokens: tokens, messages: messageMap[model]})
	}
	for model, messages := range messageMap {
		if seen[model] {
			continue
		}
		rows = append(rows, row{model: model, messages: messages})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tokens == rows[j].tokens {
			if rows[i].messages == rows[j].messages {
				return rows[i].model < rows[j].model
			}
			return rows[i].messages > rows[j].messages
		}
		return rows[i].tokens > rows[j].tokens
	})

	if limit <= 0 || len(rows) <= limit {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.model)
		}
		return out
	}

	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, rows[i].model)
	}
	return out
}

func topCopilotClientNames(tokenMap map[string]float64, sessionMap, messageMap map[string]int, limit int) []string {
	type row struct {
		client   string
		tokens   float64
		sessions int
		messages int
	}

	seen := make(map[string]bool)
	var rows []row
	for client, tokens := range tokenMap {
		seen[client] = true
		rows = append(rows, row{
			client:   client,
			tokens:   tokens,
			sessions: sessionMap[client],
			messages: messageMap[client],
		})
	}
	for client, sessions := range sessionMap {
		if seen[client] {
			continue
		}
		seen[client] = true
		rows = append(rows, row{
			client:   client,
			sessions: sessions,
			messages: messageMap[client],
		})
	}
	for client, messages := range messageMap {
		if seen[client] {
			continue
		}
		rows = append(rows, row{
			client:   client,
			messages: messages,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tokens == rows[j].tokens {
			if rows[i].sessions == rows[j].sessions {
				if rows[i].messages == rows[j].messages {
					return rows[i].client < rows[j].client
				}
				return rows[i].messages > rows[j].messages
			}
			return rows[i].sessions > rows[j].sessions
		}
		return rows[i].tokens > rows[j].tokens
	})

	if limit <= 0 || len(rows) <= limit {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.client)
		}
		return out
	}

	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, rows[i].client)
	}
	return out
}

func normalizeCopilotClient(repo, cwd string) string {
	repo = strings.TrimSpace(repo)
	if repo != "" && repo != "." {
		return repo
	}

	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		base := strings.TrimSpace(filepath.Base(cwd))
		if base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}

	return "cli"
}

func formatCopilotClientUsage(clients []string, labels map[string]string, tokens map[string]float64, sessions map[string]int) string {
	if len(clients) == 0 {
		return ""
	}

	parts := make([]string, 0, len(clients))
	for _, client := range clients {
		label := labels[client]
		if label == "" {
			label = client
		}

		value := tokens[client]
		sessionCount := sessions[client]

		item := fmt.Sprintf("%s %s tok", label, formatCopilotTokenCount(value))
		if sessionCount > 0 {
			item += fmt.Sprintf(" · %d sess", sessionCount)
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, ", ")
}

func formatCopilotTokenCount(value float64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", value/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", value/1_000)
	default:
		return fmt.Sprintf("%.0f", value)
	}
}

func parseDayFromTimestamp(ts string) string {
	t := flexParseTime(ts)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func flexParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseCopilotTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	if t := flexParseTime(s); !t.IsZero() {
		return t
	}

	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		switch {
		case n > 1_000_000_000_000:
			return time.UnixMilli(n)
		case n > 1_000_000_000:
			return time.Unix(n, 0)
		}
	}
	return time.Time{}
}

func extractModelFromInfoMsg(msg string) string {
	idx := strings.Index(msg, ": ")
	if idx < 0 {
		return ""
	}
	m := strings.TrimSpace(msg[idx+2:])
	if pIdx := strings.Index(m, " ("); pIdx >= 0 {
		m = m[:pIdx]
	}
	return m
}

func extractCopilotToolName(raw json.RawMessage) string {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ""
	}

	var tool struct {
		Name     string `json:"name"`
		ToolName string `json:"toolName"`
		Tool     string `json:"tool"`
	}
	if err := json.Unmarshal(raw, &tool); err != nil {
		return ""
	}

	candidates := []string{tool.Name, tool.ToolName, tool.Tool}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func formatModelMap(m map[string]int, unit string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for model, count := range m {
		parts = append(parts, fmt.Sprintf("%s: %d %s", model, count, unit))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func formatModelMapPlain(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for model, count := range m {
		parts = append(parts, fmt.Sprintf("%s: %d", model, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func setRawInt(snap *core.UsageSnapshot, key string, v int) {
	if v > 0 {
		snap.Raw[key] = fmt.Sprintf("%d", v)
	}
}

func setRawStr(snap *core.UsageSnapshot, key, v string) {
	if v != "" {
		snap.Raw[key] = v
	}
}

func float64Ptr(v float64) *float64 { return &v }

func firstNonNilFloat(values ...*float64) *float64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstFloat(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return -1
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
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
