package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	copilotAllTimeWindow = "all-time"
	maxCopilotModels     = 8
	maxCopilotClients    = 6
)

// Default TTLs for the tiered cache. Binary paths and versions change rarely,
// so they use long TTLs.  The full snapshot uses a shorter TTL to keep data
// reasonably fresh while eliminating most subprocess spawns.
const (
	ttlBinaryResolution = 1 * time.Hour
	ttlVersion          = 1 * time.Hour
	ttlAuthStatus       = 5 * time.Minute
	ttlSnapshot         = 2 * time.Minute
)

// copilotAPICache holds cached results from CLI subprocess calls and API
// responses.  All fields are protected by Provider.cacheMu.
type copilotAPICache struct {
	// Binary resolution (1 hour TTL)
	ghBinary         string
	copilotBinary    string
	binaryResolvedAt time.Time

	// Version detection (1 hour TTL)
	version          string
	versionSource    string
	versionFetchedAt time.Time

	// Auth status (5 min TTL)
	authOK        bool
	authOutput    string
	authFetchedAt time.Time

	// Full snapshot cache for quick return (2 min TTL)
	lastSnap   core.UsageSnapshot
	lastSnapAt time.Time
}

type Provider struct {
	providerbase.Base

	cacheMu  sync.Mutex
	apiCache *copilotAPICache
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

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(true)
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
	SelectedModel  string `json:"selectedModel"`
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

// HasChanged reports whether Copilot's local log/session files have been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	configDir := acct.Hint("config_dir", "")
	if configDir == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			configDir = filepath.Join(home, ".copilot")
		}
	}
	if configDir == "" {
		return true, nil
	}
	return shared.AnyPathModifiedAfter([]string{
		filepath.Join(configDir, "logs"),
		filepath.Join(configDir, "session-state"),
		filepath.Join(configDir, "config.json"),
	}, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	// Fast path: return cached snapshot if still fresh and successful.
	p.cacheMu.Lock()
	if p.apiCache != nil && time.Since(p.apiCache.lastSnapAt) < ttlSnapshot && p.apiCache.lastSnap.Status == core.StatusOK {
		snap := p.apiCache.lastSnap
		p.cacheMu.Unlock()
		return snap, nil
	}
	p.cacheMu.Unlock()

	ghBinary, copilotBinary := p.resolveAndCacheBinaries(acct)
	if ghBinary == "" && copilotBinary == "" {
		return core.UsageSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusError,
			Message:    "neither gh nor copilot binary found in PATH",
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

	version, versionSource, err := p.detectAndCacheVersion(ctx, ghBinary, copilotBinary)
	if err != nil {
		snap.Status = core.StatusError
		snap.Message = "copilot command not available"
		if errMsg := strings.TrimSpace(err.Error()); errMsg != "" {
			snap.Raw["copilot_version_error"] = errMsg
		}
		return snap, nil
	}
	snap.Raw["copilot_version"] = version
	snap.Raw["copilot_version_source"] = versionSource

	authOutput := ""
	if ghBinary != "" {
		authOut, authOK := p.checkAndCacheAuth(ctx, ghBinary)
		authOutput = authOut
		snap.Raw["auth_status"] = strings.TrimSpace(authOutput)

		if !authOK {
			snap.Status = core.StatusAuth
			snap.Message = "not authenticated with GitHub"
			return snap, nil
		}

		p.fetchUserInfo(ctx, ghBinary, &snap)

		p.fetchCopilotInternalUser(ctx, ghBinary, &snap)

		p.fetchRateLimits(ctx, ghBinary, &snap)

		p.fetchOrgData(ctx, ghBinary, &snap)
	} else {
		snap.Raw["auth_status"] = "gh CLI unavailable; skipped GitHub API checks"
	}

	p.fetchLocalData(acct, &snap)

	p.resolveStatus(&snap, authOutput)

	// Cache successful snapshots for quick return on subsequent polls.
	if snap.Status == core.StatusOK {
		p.cacheMu.Lock()
		if p.apiCache == nil {
			p.apiCache = &copilotAPICache{}
		}
		p.apiCache.lastSnap = snap
		p.apiCache.lastSnapAt = time.Now()
		p.cacheMu.Unlock()
	}

	return snap, nil
}

// resolveAndCacheBinaries returns cached binary paths if the TTL has not expired,
// otherwise resolves them fresh and caches the result.
func (p *Provider) resolveAndCacheBinaries(acct core.AccountConfig) (string, string) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	if p.apiCache != nil && !p.apiCache.binaryResolvedAt.IsZero() && time.Since(p.apiCache.binaryResolvedAt) < ttlBinaryResolution {
		return p.apiCache.ghBinary, p.apiCache.copilotBinary
	}

	configuredBinary := strings.TrimSpace(acct.Binary)
	if configuredBinary == "" {
		configuredBinary = "gh"
	}
	gh, copilot := resolveCopilotBinaries(configuredBinary, acct)

	if p.apiCache == nil {
		p.apiCache = &copilotAPICache{}
	}
	p.apiCache.ghBinary = gh
	p.apiCache.copilotBinary = copilot
	p.apiCache.binaryResolvedAt = time.Now()
	return gh, copilot
}

// detectAndCacheVersion returns cached version info if the TTL has not expired,
// otherwise runs the version command and caches the result.
func (p *Provider) detectAndCacheVersion(ctx context.Context, ghBinary, copilotBinary string) (string, string, error) {
	p.cacheMu.Lock()
	if p.apiCache != nil && p.apiCache.version != "" && time.Since(p.apiCache.versionFetchedAt) < ttlVersion {
		v, src := p.apiCache.version, p.apiCache.versionSource
		p.cacheMu.Unlock()
		return v, src, nil
	}
	p.cacheMu.Unlock()

	version, source, err := detectCopilotVersion(ctx, ghBinary, copilotBinary)
	if err != nil {
		return "", "", err
	}

	p.cacheMu.Lock()
	if p.apiCache == nil {
		p.apiCache = &copilotAPICache{}
	}
	p.apiCache.version = version
	p.apiCache.versionSource = source
	p.apiCache.versionFetchedAt = time.Now()
	p.cacheMu.Unlock()

	return version, source, nil
}

// checkAndCacheAuth returns cached auth status if the TTL has not expired,
// otherwise runs `gh auth status` and caches the result.
func (p *Provider) checkAndCacheAuth(ctx context.Context, ghBinary string) (string, bool) {
	p.cacheMu.Lock()
	if p.apiCache != nil && !p.apiCache.authFetchedAt.IsZero() && time.Since(p.apiCache.authFetchedAt) < ttlAuthStatus {
		out, ok := p.apiCache.authOutput, p.apiCache.authOK
		p.cacheMu.Unlock()
		return out, ok
	}
	p.cacheMu.Unlock()

	authOut, authErr := runGH(ctx, ghBinary, "auth", "status")
	authOK := authErr == nil

	p.cacheMu.Lock()
	if p.apiCache == nil {
		p.apiCache = &copilotAPICache{}
	}
	p.apiCache.authOutput = authOut
	p.apiCache.authOK = authOK
	p.apiCache.authFetchedAt = time.Now()
	p.cacheMu.Unlock()

	return authOut, authOK
}

func resolveCopilotBinaries(configuredBinary string, acct core.AccountConfig) (string, string) {
	ghBinary := ""
	copilotBinary := ""

	if isGHCliBinary(configuredBinary) {
		ghBinary = resolveBinaryPath(configuredBinary)
	} else {
		copilotBinary = resolveBinaryPath(configuredBinary)
	}

	if ghBinary == "" {
		ghBinary = resolveBinaryPath("gh")
	}

	if copilotBinary == "" {
		copilotBinary = resolveBinaryPath(acct.Hint("copilot_binary", ""))
	}
	if copilotBinary == "" {
		copilotBinary = resolveBinaryPath("copilot")
	}

	return ghBinary, copilotBinary
}

func isGHCliBinary(binary string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(binary)))
	return base == "gh" || base == "gh.exe"
}

func resolveBinaryPath(binary string) string {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return ""
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return ""
	}
	return path
}

func detectCopilotVersion(ctx context.Context, ghBinary, copilotBinary string) (string, string, error) {
	if ghBinary != "" {
		if out, err := runGH(ctx, ghBinary, "copilot", "--version"); err == nil {
			return strings.TrimSpace(out), "gh copilot", nil
		}
	}

	if copilotBinary != "" {
		if out, err := runGH(ctx, copilotBinary, "--version"); err == nil {
			return strings.TrimSpace(out), "copilot", nil
		}
	}

	return "", "", fmt.Errorf("failed to resolve a working copilot version command")
}

func (p *Provider) fetchLocalData(acct core.AccountConfig, snap *core.UsageSnapshot) {
	if dir := strings.TrimSpace(acct.Hint("config_dir", "")); dir != "" {
		p.readConfig(dir, snap)
		logData := p.readLogs(dir, snap)
		p.readSessions(dir, snap, logData)
		return
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
