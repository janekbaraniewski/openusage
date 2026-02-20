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
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "copilot" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name: "GitHub Copilot",
		Capabilities: []string{
			"quota_tracking", "plan_detection", "chat_quota",
			"completions_quota", "org_billing", "org_metrics",
			"session_tracking", "local_config", "rate_limits",
		},
		DocURL: "https://docs.github.com/en/copilot",
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
	RestrictedTelemetry      bool              `json:"restricted_telemetry"`
	CanSignupForLimited      bool              `json:"can_signup_for_limited"`
	LimitedUserSubscribedDay int               `json:"limited_user_subscribed_day"`
	LimitedUserResetDate     string            `json:"limited_user_reset_date"`
	AnalyticsTrackingID      string            `json:"analytics_tracking_id"`
	Endpoints                map[string]string `json:"endpoints"`
	OrganizationLoginList    []string          `json:"organization_login_list"`

	LimitedUserQuotas *copilotQuotas `json:"limited_user_quotas"`
	MonthlyQuotas     *copilotQuotas `json:"monthly_quotas"`

	OrganizationList []copilotOrgEntry `json:"organization_list"`
}

type copilotQuotas struct {
	Chat        *int `json:"chat"`
	Completions *int `json:"completions"`
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

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	binary := acct.Binary
	if binary == "" {
		binary = "gh"
	}

	if _, err := exec.LookPath(binary); err != nil {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusError,
			Message:    fmt.Sprintf("%s binary not found in PATH", binary),
		}, nil
	}

	snap := core.QuotaSnapshot{
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

	p.fetchLocalData(&snap)

	p.resolveStatus(&snap, authOutput)

	return snap, nil
}

func (p *Provider) fetchUserInfo(ctx context.Context, binary string, snap *core.QuotaSnapshot) {
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

func (p *Provider) fetchCopilotInternalUser(ctx context.Context, binary string, snap *core.QuotaSnapshot) {
	body, err := runGH(ctx, binary, "api", "/copilot_internal/user")
	if err != nil {
		return
	}
	var cu copilotInternalUser
	if json.Unmarshal([]byte(body), &cu) != nil {
		return
	}

	snap.Raw["copilot_plan"] = cu.CopilotPlan
	snap.Raw["access_type_sku"] = cu.AccessTypeSKU
	if cu.AssignedDate != "" {
		snap.Raw["assigned_date"] = cu.AssignedDate
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

	if cu.MonthlyQuotas != nil && cu.MonthlyQuotas.Chat != nil {
		limit := float64(*cu.MonthlyQuotas.Chat)
		var remaining float64
		if cu.LimitedUserQuotas != nil && cu.LimitedUserQuotas.Chat != nil {
			remaining = float64(*cu.LimitedUserQuotas.Chat)
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

	if cu.MonthlyQuotas != nil && cu.MonthlyQuotas.Completions != nil {
		limit := float64(*cu.MonthlyQuotas.Completions)
		var remaining float64
		if cu.LimitedUserQuotas != nil && cu.LimitedUserQuotas.Completions != nil {
			remaining = float64(*cu.LimitedUserQuotas.Completions)
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

	if cu.LimitedUserResetDate != "" {
		if t, err := time.Parse("2006-01-02", cu.LimitedUserResetDate); err == nil {
			snap.Resets["quota_reset"] = t
		}
	}
}

func (p *Provider) fetchRateLimits(ctx context.Context, binary string, snap *core.QuotaSnapshot) {
	body, err := runGH(ctx, binary, "api", "/rate_limit")
	if err != nil {
		return
	}
	var rl ghRateLimit
	if json.Unmarshal([]byte(body), &rl) != nil {
		return
	}

	rateMetrics := map[string]string{
		"core":    "Gh API RPM",
		"search":  "Gh Search RPM",
		"graphql": "Gh GraphQL RPM",
	}

	for resource, label := range rateMetrics {
		res, ok := rl.Resources[resource]
		if !ok || res.Limit == 0 {
			continue
		}
		limit := float64(res.Limit)
		remaining := float64(res.Remaining)
		used := float64(res.Used)
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
		_ = label
	}
}

func (p *Provider) fetchOrgData(ctx context.Context, binary string, snap *core.QuotaSnapshot) {
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

func (p *Provider) fetchOrgBilling(ctx context.Context, binary, org string, snap *core.QuotaSnapshot) {
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

func (p *Provider) fetchOrgMetrics(ctx context.Context, binary, org string, snap *core.QuotaSnapshot) {
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
	}

	snap.DailySeries[prefix+"active_users"] = activeUsers
	snap.DailySeries[prefix+"engaged_users"] = engagedUsers
	snap.DailySeries[prefix+"suggestions"] = totalSuggestions
	snap.DailySeries[prefix+"acceptances"] = totalAcceptances
	snap.DailySeries[prefix+"chats"] = totalChats
}

func (p *Provider) fetchLocalData(snap *core.QuotaSnapshot) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	copilotDir := filepath.Join(home, ".copilot")

	p.readConfig(copilotDir, snap)

	logData := p.readLogs(copilotDir, snap)

	p.readSessions(copilotDir, snap, logData)
}

func (p *Provider) readConfig(copilotDir string, snap *core.QuotaSnapshot) {
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
}

func (p *Provider) readLogs(copilotDir string, snap *core.QuotaSnapshot) logSummary {
	ls := logSummary{SessionTokens: make(map[string]logTokenEntry)}
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
	}

	return ls
}

type assistantMsgData struct {
	Content      string          `json:"content"`
	ReasoningTxt string          `json:"reasoningText"`
	ToolRequests json.RawMessage `json:"toolRequests"`
}

func (p *Provider) readSessions(copilotDir string, snap *core.QuotaSnapshot, logs logSummary) {
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
		repo           string
		branch         string
		summary        string
		messages       int
		turns          int
		model          string
		responseChars  int
		reasoningChars int
		toolCalls      int
		tokenUsed      int
		tokenTotal     int
	}

	var sessions []sessionInfo
	dailyMessages := make(map[string]float64)
	dailySessions := make(map[string]float64)
	dailyToolCalls := make(map[string]float64)
	modelMessages := make(map[string]int)
	modelTurns := make(map[string]int)
	modelSessions := make(map[string]int)
	modelResponseChars := make(map[string]int)
	modelReasoningChars := make(map[string]int)
	modelToolCalls := make(map[string]int)
	dailyModelMessages := make(map[string]map[string]float64)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		si := sessionInfo{id: entry.Name()}
		sessPath := filepath.Join(sessionDir, entry.Name())

		if wsData, err := os.ReadFile(filepath.Join(sessPath, "workspace.yaml")); err == nil {
			ws := parseSimpleYAML(string(wsData))
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

		if si.model != "" {
			modelSessions[si.model]++
		}
		if !si.createdAt.IsZero() {
			dailySessions[si.createdAt.Format("2006-01-02")]++
		}
		sessions = append(sessions, si)
	}

	storeSeries(snap, "cli_messages", dailyMessages)
	storeSeries(snap, "cli_sessions", dailySessions)
	storeSeries(snap, "cli_tool_calls", dailyToolCalls)
	for model, dayCounts := range dailyModelMessages {
		storeSeries(snap, "cli_messages_"+model, dayCounts)
	}

	setRawStr(snap, "model_usage", formatModelMap(modelMessages, "msgs"))
	setRawStr(snap, "model_turns", formatModelMap(modelTurns, "turns"))
	setRawStr(snap, "model_sessions", formatModelMapPlain(modelSessions))
	setRawStr(snap, "model_response_chars", formatModelMap(modelResponseChars, "chars"))
	setRawStr(snap, "model_reasoning_chars", formatModelMap(modelReasoningChars, "chars"))
	setRawStr(snap, "model_tool_calls", formatModelMap(modelToolCalls, "calls"))

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].updatedAt.After(sessions[j].updatedAt)
	})

	var totalMessages, totalTurns, totalResponse, totalReasoning, totalTools int
	for _, s := range sessions {
		totalMessages += s.messages
		totalTurns += s.turns
		totalResponse += s.responseChars
		totalReasoning += s.reasoningChars
		totalTools += s.toolCalls
	}
	setRawInt(snap, "total_cli_messages", totalMessages)
	setRawInt(snap, "total_cli_turns", totalTurns)
	setRawInt(snap, "total_response_chars", totalResponse)
	setRawInt(snap, "total_reasoning_chars", totalReasoning)
	setRawInt(snap, "total_tool_calls", totalTools)

	if len(sessions) > 0 {
		r := sessions[0]
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
		if r.tokenUsed > 0 {
			snap.Raw["last_session_tokens"] = fmt.Sprintf("%d/%d", r.tokenUsed, r.tokenTotal)
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

func (p *Provider) resolveStatus(snap *core.QuotaSnapshot, authOutput string) {
	lower := strings.ToLower(authOutput)
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limit") {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited"
		return
	}

	for key, m := range snap.Metrics {
		pct := m.Percent()
		if pct >= 0 && pct < 5 && (key == "chat_quota" || key == "completions_quota") {
			snap.Status = core.StatusLimited
			snap.Message = quotaStatusMessage(snap)
			return
		}
		if pct >= 0 && pct < 20 && (key == "chat_quota" || key == "completions_quota") {
			snap.Status = core.StatusNearLimit
			snap.Message = quotaStatusMessage(snap)
			return
		}
	}

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = quotaStatusMessage(snap)
	}
}

func quotaStatusMessage(snap *core.QuotaSnapshot) string {
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

func storeSeries(snap *core.QuotaSnapshot, key string, m map[string]float64) {
	if len(m) > 0 {
		snap.DailySeries[key] = mapToSeries(m)
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
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
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

func setRawInt(snap *core.QuotaSnapshot, key string, v int) {
	if v > 0 {
		snap.Raw[key] = fmt.Sprintf("%d", v)
	}
}

func setRawStr(snap *core.QuotaSnapshot, key, v string) {
	if v != "" {
		snap.Raw[key] = v
	}
}
