package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) fetchUserInfo(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	userJSON, err := runGHAPI(ctx, binary, "/user")
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
	body, err := runGHAPI(ctx, binary, "/copilot_internal/user")
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

	p.applyUsageSnapshotMetrics(cu.UsageSnapshots, snap)

	for _, candidate := range []string{cu.UsageResetDateUTC, cu.UsageResetDate, cu.LimitedUserResetDate} {
		if t := parseCopilotTime(candidate); !t.IsZero() {
			snap.Resets["quota_reset"] = t
			break
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
			Limit:     core.Float64Ptr(*limit),
			Remaining: core.Float64Ptr(*remaining),
			Used:      core.Float64Ptr(used),
			Unit:      unit,
			Window:    "month",
		}
		return true
	case pct >= 0:
		limitPct := 100.0
		used := 100 - pct
		snap.Metrics[key] = core.Metric{
			Limit:     &limitPct,
			Remaining: core.Float64Ptr(pct),
			Used:      core.Float64Ptr(used),
			Unit:      "%",
			Window:    "month",
		}
		return true
	case remaining != nil:
		snap.Metrics[key] = core.Metric{
			Used:   core.Float64Ptr(*remaining),
			Unit:   unit,
			Window: "month",
		}
		return true
	default:
		return false
	}
}

func (p *Provider) fetchRateLimits(ctx context.Context, binary string, snap *core.UsageSnapshot) {
	body, err := runGHAPI(ctx, binary, "/rate_limit")
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
	body, err := runGHAPI(ctx, binary, fmt.Sprintf("/orgs/%s/copilot/billing", org))
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
		snap.Metrics[prefix+"seats"] = core.Metric{
			Limit:  &total,
			Used:   &active,
			Unit:   "seats",
			Window: "cycle",
		}
	}
}

func (p *Provider) fetchOrgMetrics(ctx context.Context, binary, org string, snap *core.UsageSnapshot) {
	body, err := runGHAPI(ctx, binary, fmt.Sprintf("/orgs/%s/copilot/metrics", org))
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
		snap.Metrics[prefix+"active_users"] = core.Metric{Used: core.Float64Ptr(lastActive), Unit: "users", Window: "day"}
	}
	if len(engagedUsers) > 0 {
		lastEngaged := engagedUsers[len(engagedUsers)-1].Value
		snap.Metrics[prefix+"engaged_users"] = core.Metric{Used: core.Float64Ptr(lastEngaged), Unit: "users", Window: "day"}
	}
	if aggSuggestions > 0 {
		snap.Metrics[prefix+"suggestions"] = core.Metric{Used: core.Float64Ptr(aggSuggestions), Unit: "suggestions", Window: "series"}
	}
	if aggAcceptances > 0 {
		snap.Metrics[prefix+"acceptances"] = core.Metric{Used: core.Float64Ptr(aggAcceptances), Unit: "acceptances", Window: "series"}
	}
	if aggChats > 0 {
		snap.Metrics[prefix+"chats"] = core.Metric{Used: core.Float64Ptr(aggChats), Unit: "chats", Window: "series"}
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

func runGHAPI(ctx context.Context, binary, endpoint string) (string, error) {
	return runGH(
		ctx,
		binary,
		"api",
		"-H", "Cache-Control: no-cache",
		"-H", "Pragma: no-cache",
		endpoint,
	)
}
