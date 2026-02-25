package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestParseSimpleYAML(t *testing.T) {
	input := `id: abc-123
cwd: /home/user/project
git_root: /home/user/project
repository: user/project
branch: main
summary_count: 0
created_at: 2026-02-20T01:07:33.336Z
updated_at: 2026-02-20T01:07:59.359Z
summary: hello world`

	got := parseSimpleYAML(input)

	tests := map[string]string{
		"id":         "abc-123",
		"cwd":        "/home/user/project",
		"repository": "user/project",
		"branch":     "main",
		"summary":    "hello world",
		"created_at": "2026-02-20T01:07:33.336Z",
		"updated_at": "2026-02-20T01:07:59.359Z",
	}
	for k, want := range tests {
		if got[k] != want {
			t.Errorf("parseSimpleYAML[%q] = %q, want %q", k, got[k], want)
		}
	}
}

func TestParseSimpleYAML_Empty(t *testing.T) {
	got := parseSimpleYAML("")
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestParseSimpleYAML_Comments(t *testing.T) {
	input := `# comment
key: value
# another comment
key2: value2`
	got := parseSimpleYAML(input)
	if got["key"] != "value" || got["key2"] != "value2" {
		t.Errorf("unexpected: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestMapToSeries(t *testing.T) {
	m := map[string]float64{
		"2026-02-20": 5,
		"2026-02-18": 3,
		"2026-02-19": 7,
	}
	series := mapToSeries(m)
	if len(series) != 3 {
		t.Fatalf("expected 3 points, got %d", len(series))
	}
	if series[0].Date != "2026-02-18" {
		t.Errorf("expected first date 2026-02-18, got %s", series[0].Date)
	}
	if series[2].Date != "2026-02-20" {
		t.Errorf("expected last date 2026-02-20, got %s", series[2].Date)
	}
	if series[1].Value != 7 {
		t.Errorf("expected middle value 7, got %f", series[1].Value)
	}
}

func TestMapToSeries_Empty(t *testing.T) {
	series := mapToSeries(map[string]float64{})
	if len(series) != 0 {
		t.Errorf("expected 0 points, got %d", len(series))
	}
}

func TestSKULabel(t *testing.T) {
	tests := []struct {
		sku  string
		want string
	}{
		{"free_limited_copilot", "Free"},
		{"copilot_pro", "Pro"},
		{"copilot_pro_plus", "Pro+"},
		{"copilot_business", "Business"},
		{"copilot_enterprise", "Enterprise"},
		{"unknown_sku", "unknown_sku"},
	}
	for _, tt := range tests {
		got := skuLabel(tt.sku)
		if got != tt.want {
			t.Errorf("skuLabel(%q) = %q, want %q", tt.sku, got, tt.want)
		}
	}
}

func TestProviderID(t *testing.T) {
	p := New()
	if p.ID() != "copilot" {
		t.Errorf("ID() = %q, want %q", p.ID(), "copilot")
	}
}

func TestProviderDescribe(t *testing.T) {
	p := New()
	info := p.Describe()
	if info.Name != "GitHub Copilot" {
		t.Errorf("Name = %q, want %q", info.Name, "GitHub Copilot")
	}
	if len(info.Capabilities) == 0 {
		t.Error("expected non-empty Capabilities")
	}
}

func TestCopilotInternalUserParsing(t *testing.T) {
	body := `{
		"login": "testuser",
		"access_type_sku": "free_limited_copilot",
		"copilot_plan": "individual",
		"assigned_date": "2025-03-17T12:42:49+01:00",
		"chat_enabled": true,
		"is_mcp_enabled": true,
		"copilotignore_enabled": false,
		"restricted_telemetry": true,
		"can_signup_for_limited": false,
		"limited_user_subscribed_day": 17,
		"limited_user_reset_date": "2026-03-17",
		"endpoints": {
			"api": "https://api.individual.githubcopilot.com",
			"proxy": "https://proxy.individual.githubcopilot.com"
		},
		"organization_login_list": ["myorg"],
		"organization_list": [
			{"login": "myorg", "is_enterprise": false, "copilot_plan": "business"}
		],
		"limited_user_quotas": {
			"chat": 470,
			"completions": 3900
		},
		"monthly_quotas": {
			"chat": 500,
			"completions": 4000
		}
	}`

	var cu copilotInternalUser
	if err := unmarshalJSON(body, &cu); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cu.Login != "testuser" {
		t.Errorf("Login = %q, want %q", cu.Login, "testuser")
	}
	if cu.AccessTypeSKU != "free_limited_copilot" {
		t.Errorf("AccessTypeSKU = %q", cu.AccessTypeSKU)
	}
	if cu.CopilotPlan != "individual" {
		t.Errorf("CopilotPlan = %q", cu.CopilotPlan)
	}
	if !cu.ChatEnabled {
		t.Error("ChatEnabled should be true")
	}
	if !cu.MCPEnabled {
		t.Error("MCPEnabled should be true")
	}
	if cu.CopilotIgnoreEnabled {
		t.Error("CopilotIgnoreEnabled should be false")
	}
	if cu.LimitedUserResetDate != "2026-03-17" {
		t.Errorf("LimitedUserResetDate = %q", cu.LimitedUserResetDate)
	}
	if cu.MonthlyUsage == nil || cu.MonthlyUsage.Chat == nil || *cu.MonthlyUsage.Chat != 500 {
		t.Error("MonthlyUsage.Chat should be 500")
	}
	if cu.MonthlyUsage.Completions == nil || *cu.MonthlyUsage.Completions != 4000 {
		t.Error("MonthlyUsage.Completions should be 4000")
	}
	if cu.LimitedUserUsage == nil || cu.LimitedUserUsage.Chat == nil || *cu.LimitedUserUsage.Chat != 470 {
		t.Error("LimitedUserUsage.Chat should be 470")
	}
	if cu.LimitedUserUsage.Completions == nil || *cu.LimitedUserUsage.Completions != 3900 {
		t.Error("LimitedUserUsage.Completions should be 3900")
	}
	if len(cu.OrganizationLoginList) != 1 || cu.OrganizationLoginList[0] != "myorg" {
		t.Errorf("OrganizationLoginList = %v", cu.OrganizationLoginList)
	}
	if len(cu.OrganizationList) != 1 || cu.OrganizationList[0].Login != "myorg" {
		t.Errorf("OrganizationList = %v", cu.OrganizationList)
	}
	if cu.Endpoints["api"] != "https://api.individual.githubcopilot.com" {
		t.Errorf("Endpoints[api] = %q", cu.Endpoints["api"])
	}
}

func TestCopilotInternalUserParsing_NoUsageLimits(t *testing.T) {
	body := `{
		"login": "prouser",
		"access_type_sku": "copilot_pro",
		"copilot_plan": "individual",
		"chat_enabled": true,
		"is_mcp_enabled": false,
		"endpoints": {},
		"organization_login_list": [],
		"organization_list": []
	}`

	var cu copilotInternalUser
	if err := unmarshalJSON(body, &cu); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cu.LimitedUserUsage != nil {
		t.Error("expected nil LimitedUserUsage for pro user")
	}
	if cu.MonthlyUsage != nil {
		t.Error("expected nil MonthlyUsage for pro user")
	}
}

func TestCopilotInternalUserParsing_UsageSnapshots(t *testing.T) {
	body := `{
		"login": "testuser",
		"access_type_sku": "free_limited_copilot",
		"copilot_plan": "individual",
		"quota_reset_date_utc": "2026-03-17T00:00:00Z",
		"quota_snapshots": {
			"chat": {
				"entitlement": 500,
				"quota_remaining": 470,
				"percent_remaining": 94,
				"quota_id": "chat",
				"timestamp_utc": "2026-02-21T00:00:00Z"
			},
			"completions": {
				"entitlement": 4000,
				"quota_remaining": 3900,
				"quota_id": "completions"
			},
			"premium_interactions": {
				"entitlement": 50,
				"remaining": 45,
				"quota_id": "premium"
			}
		}
	}`

	var cu copilotInternalUser
	if err := unmarshalJSON(body, &cu); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cu.UsageSnapshots == nil || cu.UsageSnapshots.Chat == nil {
		t.Fatal("expected chat quota snapshot")
	}
	if cu.UsageSnapshots.Chat.Entitlement == nil || *cu.UsageSnapshots.Chat.Entitlement != 500 {
		t.Fatalf("chat entitlement = %v, want 500", cu.UsageSnapshots.Chat.Entitlement)
	}
	if cu.UsageSnapshots.PremiumInteractions == nil || cu.UsageSnapshots.PremiumInteractions.Remaining == nil || *cu.UsageSnapshots.PremiumInteractions.Remaining != 45 {
		t.Fatalf("premium remaining = %v, want 45", cu.UsageSnapshots.PremiumInteractions)
	}
}

func TestApplyCopilotInternalUser_UsageSnapshotMetrics(t *testing.T) {
	p := New()
	cu := &copilotInternalUser{
		AccessTypeSKU:     "free_limited_copilot",
		CopilotPlan:       "individual",
		UsageResetDateUTC: "2026-03-17T00:00:00Z",
		UsageSnapshots: &copilotUsageSnapshots{
			Chat: &copilotUsageSnapshot{
				Entitlement:    float64Ptr(500),
				UsageRemaining: float64Ptr(470),
				UsageID:        "chat",
			},
			Completions: &copilotUsageSnapshot{
				Entitlement:    float64Ptr(4000),
				UsageRemaining: float64Ptr(3900),
				UsageID:        "completions",
			},
			PremiumInteractions: &copilotUsageSnapshot{
				Entitlement:      float64Ptr(50),
				Remaining:        float64Ptr(45),
				UsageID:          "premium",
				TimestampUTC:     "2026-02-21T00:00:00Z",
				OveragePermitted: boolPtr(true),
			},
		},
	}

	snap := &core.UsageSnapshot{
		Metrics: make(map[string]core.Metric),
		Resets:  make(map[string]time.Time),
		Raw:     make(map[string]string),
	}
	p.applyCopilotInternalUser(cu, snap)

	if m, ok := snap.Metrics["chat_quota"]; !ok || m.Used == nil || *m.Used != 30 {
		t.Fatalf("chat_quota used = %v, want 30", m.Used)
	}
	if m, ok := snap.Metrics["completions_quota"]; !ok || m.Remaining == nil || *m.Remaining != 3900 {
		t.Fatalf("completions_quota remaining = %v, want 3900", m.Remaining)
	}
	if m, ok := snap.Metrics["premium_interactions_quota"]; !ok || m.Limit == nil || *m.Limit != 50 {
		t.Fatalf("premium_interactions_quota limit = %v, want 50", m.Limit)
	}
	if _, ok := snap.Resets["quota_reset"]; !ok {
		t.Fatal("expected quota_reset")
	}
	if got := snap.Raw["premium_interactions_quota_overage_permitted"]; got != "true" {
		t.Fatalf("premium overage raw = %q, want true", got)
	}
}

func TestRateLimitParsing(t *testing.T) {
	body := `{
		"resources": {
			"core": {"limit": 5000, "remaining": 4990, "reset": 1740000000, "used": 10},
			"search": {"limit": 30, "remaining": 28, "reset": 1740000060, "used": 2},
			"graphql": {"limit": 5000, "remaining": 5000, "reset": 1740000000, "used": 0}
		}
	}`

	var rl ghRateLimit
	if err := unmarshalJSON(body, &rl); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	core := rl.Resources["core"]
	if core.Limit != 5000 {
		t.Errorf("core.Limit = %d, want 5000", core.Limit)
	}
	if core.Remaining != 4990 {
		t.Errorf("core.Remaining = %d, want 4990", core.Remaining)
	}
	if core.Used != 10 {
		t.Errorf("core.Used = %d, want 10", core.Used)
	}

	search := rl.Resources["search"]
	if search.Limit != 30 {
		t.Errorf("search.Limit = %d, want 30", search.Limit)
	}
}

func TestOrgBillingParsing(t *testing.T) {
	body := `{
		"seat_breakdown": {
			"total": 25,
			"added_this_cycle": 2,
			"pending_cancellation": 1,
			"pending_invitation": 3,
			"active_this_cycle": 20,
			"inactive_this_cycle": 5
		},
		"plan_type": "business",
		"seat_management_setting": "assign_selected",
		"public_code_suggestions": "block",
		"ide_chat": "enabled",
		"platform_chat": "enabled",
		"cli": "enabled"
	}`

	var billing orgBilling
	if err := unmarshalJSON(body, &billing); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if billing.PlanType != "business" {
		t.Errorf("PlanType = %q", billing.PlanType)
	}
	if billing.SeatBreakdown.Total != 25 {
		t.Errorf("Total seats = %d, want 25", billing.SeatBreakdown.Total)
	}
	if billing.SeatBreakdown.ActiveThisCycle != 20 {
		t.Errorf("ActiveThisCycle = %d, want 20", billing.SeatBreakdown.ActiveThisCycle)
	}
	if billing.IDEChat != "enabled" {
		t.Errorf("IDEChat = %q", billing.IDEChat)
	}
}

func TestOrgMetricsParsing(t *testing.T) {
	body := `[
		{
			"date": "2026-02-18",
			"total_active_users": 15,
			"total_engaged_users": 12,
			"copilot_ide_code_completions": {
				"total_engaged_users": 10,
				"editors": [
					{
						"name": "vscode",
						"models": [
							{
								"name": "default",
								"is_custom_model": false,
								"total_engaged_users": 10,
								"total_code_suggestions": 500,
								"total_code_acceptances": 200,
								"total_code_lines_suggested": 1000,
								"total_code_lines_accepted": 400
							}
						]
					}
				]
			},
			"copilot_ide_chat": {
				"total_engaged_users": 8,
				"editors": [
					{
						"name": "vscode",
						"models": [
							{
								"name": "gpt-4o",
								"is_custom_model": false,
								"total_engaged_users": 8,
								"total_chats": 45,
								"total_chat_copy_events": 10,
								"total_chat_insertion_events": 5
							}
						]
					}
				]
			}
		}
	]`

	var days []orgMetricsDay
	if err := unmarshalJSON(body, &days); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(days))
	}

	day := days[0]
	if day.Date != "2026-02-18" {
		t.Errorf("Date = %q", day.Date)
	}
	if day.TotalActiveUsers != 15 {
		t.Errorf("TotalActiveUsers = %d", day.TotalActiveUsers)
	}
	if day.Completions == nil {
		t.Fatal("Completions is nil")
	}
	if len(day.Completions.Editors) != 1 {
		t.Fatalf("expected 1 editor, got %d", len(day.Completions.Editors))
	}
	model := day.Completions.Editors[0].Models[0]
	if model.TotalSuggestions != 500 {
		t.Errorf("TotalSuggestions = %d", model.TotalSuggestions)
	}
	if model.TotalAcceptances != 200 {
		t.Errorf("TotalAcceptances = %d", model.TotalAcceptances)
	}

	if day.IDEChat == nil {
		t.Fatal("IDEChat is nil")
	}
	chatModel := day.IDEChat.Editors[0].Models[0]
	if chatModel.TotalChats != 45 {
		t.Errorf("TotalChats = %d", chatModel.TotalChats)
	}
}

func TestCopilotConfigParsing(t *testing.T) {
	body := `{
		"banner": "never",
		"model": "gpt-5-mini",
		"asked_setup_terminals": ["cursor"],
		"render_markdown": true,
		"reasoning_effort": "high",
		"experimental": true
	}`

	var cfg copilotConfig
	if err := unmarshalJSON(body, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cfg.Model != "gpt-5-mini" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q", cfg.ReasoningEffort)
	}
	if !cfg.Experimental {
		t.Error("Experimental should be true")
	}
	if !cfg.RenderMarkdown {
		t.Error("RenderMarkdown should be true")
	}
}

func TestSessionEventParsing(t *testing.T) {
	body := `{
		"type": "user.message",
		"id": "abc-123",
		"timestamp": "2026-02-20T01:07:59.350Z",
		"data": {"content": "hello"}
	}`

	var evt sessionEvent
	if err := unmarshalJSON(body, &evt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if evt.Type != "user.message" {
		t.Errorf("Type = %q", evt.Type)
	}
	if evt.ID != "abc-123" {
		t.Errorf("ID = %q", evt.ID)
	}
	if evt.Timestamp != "2026-02-20T01:07:59.350Z" {
		t.Errorf("Timestamp = %q", evt.Timestamp)
	}
}

func TestResetDateParsing(t *testing.T) {
	dateStr := "2026-03-17"
	t1, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if t1.Year() != 2026 || t1.Month() != 3 || t1.Day() != 17 {
		t.Errorf("parsed date = %v", t1)
	}
}

func TestUsageStatusMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]string
		want string
	}{
		{
			"with login and sku",
			map[string]string{"github_login": "user1", "access_type_sku": "free_limited_copilot"},
			"Copilot (user1) · Free",
		},
		{
			"with login and plan only",
			map[string]string{"github_login": "user1", "copilot_plan": "individual"},
			"Copilot (user1) · individual",
		},
		{
			"no login",
			map[string]string{"access_type_sku": "copilot_pro"},
			"Copilot · Pro",
		},
		{
			"minimal",
			map[string]string{},
			"Copilot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := &core.UsageSnapshot{Raw: tt.raw}
			got := usageStatusMessage(snap)
			if got != tt.want {
				t.Errorf("usageStatusMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelChangeDataParsing(t *testing.T) {
	body := `{"newModel": "gpt-5-mini"}`
	var mc modelChangeData
	if err := unmarshalJSON(body, &mc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if mc.NewModel != "gpt-5-mini" {
		t.Errorf("NewModel = %q, want %q", mc.NewModel, "gpt-5-mini")
	}
}

func TestModelChangeDataParsing_WithOld(t *testing.T) {
	body := `{"oldModel": "claude-sonnet-4.6", "newModel": "gpt-5-mini"}`
	var mc modelChangeData
	if err := unmarshalJSON(body, &mc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if mc.OldModel != "claude-sonnet-4.6" {
		t.Errorf("OldModel = %q", mc.OldModel)
	}
	if mc.NewModel != "gpt-5-mini" {
		t.Errorf("NewModel = %q", mc.NewModel)
	}
}

func TestSessionInfoDataParsing(t *testing.T) {
	body := `{"infoType": "model", "message": "Model changed to: gpt-5-mini (high)"}`
	var info sessionInfoData
	if err := unmarshalJSON(body, &info); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if info.InfoType != "model" {
		t.Errorf("InfoType = %q", info.InfoType)
	}
	if info.Message != "Model changed to: gpt-5-mini (high)" {
		t.Errorf("Message = %q", info.Message)
	}
}

func TestParseCompactionLine(t *testing.T) {
	tests := []struct {
		line      string
		wantUsed  int
		wantTotal int
	}{
		{
			"2026-02-20T01:06:10.578Z [INFO] CompactionProcessor: Utilization 13.3% (16987/128000 tokens) below threshold 80%",
			16987, 128000,
		},
		{
			"2026-02-20T01:07:59.803Z [INFO] CompactionProcessor: Utilization 13.4% (17160/128000 tokens) below threshold 80%",
			17160, 128000,
		},
		{
			"garbage line",
			0, 0,
		},
	}
	for _, tt := range tests {
		entry := parseCompactionLine(tt.line)
		if entry.Used != tt.wantUsed {
			t.Errorf("parseCompactionLine(%q).Used = %d, want %d", tt.line[:min(40, len(tt.line))], entry.Used, tt.wantUsed)
		}
		if entry.Total != tt.wantTotal {
			t.Errorf("parseCompactionLine(%q).Total = %d, want %d", tt.line[:min(40, len(tt.line))], entry.Total, tt.wantTotal)
		}
	}
}

func TestParseCompactionLine_Timestamp(t *testing.T) {
	line := "2026-02-20T01:07:59.803Z [INFO] CompactionProcessor: Utilization 13.4% (17160/128000 tokens) below threshold 80%"
	entry := parseCompactionLine(line)
	if entry.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if entry.Timestamp.Year() != 2026 || entry.Timestamp.Month() != 2 || entry.Timestamp.Day() != 20 {
		t.Errorf("timestamp = %v", entry.Timestamp)
	}
}

func TestParseDayFromTimestamp(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2026-02-20T01:07:59.350Z", "2026-02-20"},
		{"2026-02-20T01:07:59Z", "2026-02-20"},
		{"", ""},
		{"not-a-date", ""},
	}
	for _, tt := range tests {
		got := parseDayFromTimestamp(tt.input)
		if got != tt.want {
			t.Errorf("parseDayFromTimestamp(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractModelFromInfoMsg(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"Model changed to: gpt-5-mini (high)", "gpt-5-mini"},
		{"Model changed to: claude-haiku-4.5", "claude-haiku-4.5"},
		{"Model changed to: claude-sonnet-4.6 (medium)", "claude-sonnet-4.6"},
		{"something else", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractModelFromInfoMsg(tt.message)
		if got != tt.want {
			t.Errorf("extractModelFromInfoMsg(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestNormalizeCopilotClient(t *testing.T) {
	tests := []struct {
		name string
		repo string
		cwd  string
		want string
	}{
		{name: "repo preferred", repo: "owner/repo", cwd: "/tmp/project", want: "owner/repo"},
		{name: "cwd fallback", repo: "", cwd: "/tmp/project", want: "project"},
		{name: "default cli", repo: "", cwd: "", want: "cli"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeCopilotClient(tt.repo, tt.cwd); got != tt.want {
				t.Fatalf("normalizeCopilotClient(%q, %q) = %q, want %q", tt.repo, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestFlexParseTime(t *testing.T) {
	tests := []struct {
		input string
		year  int
	}{
		{"2026-02-20T01:07:59Z", 2026},
		{"2026-02-20T01:07:59.350Z", 2026},
		{"2026-02-20T01:07:59+01:00", 2026},
		{"", 0},
		{"garbage", 0},
	}
	for _, tt := range tests {
		got := flexParseTime(tt.input)
		if tt.year == 0 && !got.IsZero() {
			t.Errorf("flexParseTime(%q) should be zero", tt.input)
		}
		if tt.year != 0 && got.Year() != tt.year {
			t.Errorf("flexParseTime(%q).Year() = %d, want %d", tt.input, got.Year(), tt.year)
		}
	}
}

func TestFormatModelMap(t *testing.T) {
	got := formatModelMap(map[string]int{
		"gpt-5-mini":        3,
		"claude-sonnet-4.6": 5,
	}, "msgs")
	if !strings.Contains(got, "claude-sonnet-4.6: 5 msgs") {
		t.Errorf("missing claude entry in %q", got)
	}
	if !strings.Contains(got, "gpt-5-mini: 3 msgs") {
		t.Errorf("missing gpt entry in %q", got)
	}
}

func TestFormatModelMap_Empty(t *testing.T) {
	got := formatModelMap(map[string]int{}, "msgs")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFormatModelMapPlain(t *testing.T) {
	got := formatModelMapPlain(map[string]int{"gpt-5-mini": 2})
	if got != "gpt-5-mini: 2" {
		t.Errorf("got %q", got)
	}
}

func TestAssistantMsgDataParsing(t *testing.T) {
	body := `{
		"content": "Hello! How can I help you?",
		"reasoningText": "The user said hello, I should greet them back.",
		"toolRequests": [{"name": "read_file", "args": {"path": "foo.go"}}]
	}`
	var msg assistantMsgData
	if err := unmarshalJSON(body, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if msg.Content != "Hello! How can I help you?" {
		t.Errorf("Content = %q", msg.Content)
	}
	if msg.ReasoningTxt != "The user said hello, I should greet them back." {
		t.Errorf("ReasoningTxt = %q", msg.ReasoningTxt)
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(msg.ToolRequests, &tools); err != nil {
		t.Fatalf("tool parse failed: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool request, got %d", len(tools))
	}
}

func TestAssistantMsgDataParsing_EmptyTools(t *testing.T) {
	body := `{
		"content": "Sure!",
		"reasoningText": "",
		"toolRequests": []
	}`
	var msg assistantMsgData
	if err := unmarshalJSON(body, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(msg.ToolRequests, &tools); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tool requests, got %d", len(tools))
	}
}

func TestReadSessions_EmitsModelTokenMetrics(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-20T01:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-20T01:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-20T01:00:02.000Z [INFO] CompactionProcessor: Utilization 1.4% (1800/128000 tokens) below threshold 80%",
		"2026-02-20T02:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-20T02:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process-test.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, created, updated string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}
		events := strings.Join([]string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[{"name":"read_file"}]}}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	mkSession("s1", "gpt-5-mini", "2026-02-20T01:00:00Z", "2026-02-20T01:10:00Z")
	mkSession("s2", "claude-sonnet-4.6", "2026-02-20T02:00:00Z", "2026-02-20T02:10:00Z")

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	if m := snap.Metrics["model_gpt_5_mini_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("model_gpt_5_mini_input_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["model_claude_sonnet_4_6_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("model_claude_sonnet_4_6_input_tokens missing/zero: %+v", m)
	}
	if _, ok := snap.DailySeries["tokens_gpt_5_mini"]; !ok {
		t.Fatal("missing tokens_gpt_5_mini series")
	}
	if m := snap.Metrics["cli_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("cli_input_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["client_owner_repo_total_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("client_owner_repo_total_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["client_owner_repo_sessions"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("client_owner_repo_sessions = %+v, want 2", m)
	}
	if _, ok := snap.DailySeries["tokens_client_owner_repo"]; !ok {
		t.Fatal("missing tokens_client_owner_repo series")
	}
	if got := snap.Raw["client_usage"]; !strings.Contains(got, "owner/repo") {
		t.Fatalf("client_usage = %q, want owner/repo", got)
	}
	if m := snap.Metrics["messages_today"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("messages_today missing/zero: %+v", m)
	}
	if m := snap.Metrics["tool_read_file"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("tool_read_file = %+v, want 2 calls", m)
	}
	if _, ok := snap.Metrics["context_window"]; !ok {
		t.Fatal("missing context_window metric")
	}
}

func TestReadLogs_UsesNewestTokenEntryByTimestamp(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	newer := strings.Join([]string{
		"2026-02-21T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-21T10:00:01.000Z [INFO] CompactionProcessor: Utilization 3.9% (5000/128000 tokens) below threshold 80%",
	}, "\n")
	older := strings.Join([]string{
		"2026-02-20T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-20T10:00:01.000Z [INFO] CompactionProcessor: Utilization 0.8% (1000/128000 tokens) below threshold 80%",
	}, "\n")
	// Lexicographic order is intentionally opposite to timestamp order.
	if err := os.WriteFile(filepath.Join(logDir, "a-new.log"), []byte(newer), 0o644); err != nil {
		t.Fatalf("write newer log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "z-old.log"), []byte(older), 0o644); err != nil {
		t.Fatalf("write older log: %v", err)
	}

	snap := &core.UsageSnapshot{
		Metrics: make(map[string]core.Metric),
		Resets:  make(map[string]time.Time),
		Raw:     make(map[string]string),
	}
	logs := p.readLogs(copilotDir, snap)

	if got := snap.Raw["context_window_tokens"]; got != "5000/128000" {
		t.Fatalf("context_window_tokens = %q, want %q", got, "5000/128000")
	}
	if got := logs.SessionTokens["s1"].Used; got != 5000 {
		t.Fatalf("session s1 used = %d, want 5000", got)
	}
}

func TestReadSessions_UsesLatestEventTimestampForRecency(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-21T13:05:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-21T13:05:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-21T15:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-21T15:00:01.000Z [INFO] CompactionProcessor: Utilization 1.7% (2200/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, wsCreated, wsUpdated, evtTs string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + wsCreated,
			"updated_at: " + wsUpdated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}
		events := strings.Join([]string{
			`{"type":"session.model_change","timestamp":"` + evtTs + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + evtTs + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + evtTs + `","data":{"turnId":"0"}}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	// Workspace metadata claims s1 is newer, but session events show s2 is latest.
	mkSession("s1", "model-s1", "2026-02-21T10:00:00Z", "2026-02-21T13:00:00Z", "2026-02-21T13:05:00Z")
	mkSession("s2", "model-s2", "2026-02-21T10:00:00Z", "2026-02-21T12:00:00Z", "2026-02-21T15:00:00Z")

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}
	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	if got := snap.Raw["last_session_model"]; got != "model-s2" {
		t.Fatalf("last_session_model = %q, want model-s2", got)
	}
	if got := snap.Raw["last_session_tokens"]; got != "2200/128000" {
		t.Fatalf("last_session_tokens = %q, want 2200/128000", got)
	}
	if got := snap.Raw["last_session_time"]; got != "2026-02-21T15:00:01Z" {
		t.Fatalf("last_session_time = %q, want 2026-02-21T15:00:01Z", got)
	}
}

func TestSessionShutdownDataParsing(t *testing.T) {
	body := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 12,
		"totalApiDurationMs": 45000,
		"sessionStartTime": "2026-02-24T10:00:00Z",
		"codeChanges": {"linesAdded": 150, "linesRemoved": 30, "filesModified": 5},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 10, "cost": 0.35},
				"usage": {"inputTokens": 52000, "outputTokens": 18000, "cacheReadTokens": 30000, "cacheWriteTokens": 5000}
			},
			"gpt-5-mini": {
				"requests": {"count": 2, "cost": 0.05},
				"usage": {"inputTokens": 3000, "outputTokens": 1000, "cacheReadTokens": 0, "cacheWriteTokens": 0}
			}
		}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "user_exit" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "user_exit")
	}
	if shutdown.TotalPremiumRequests != 12 {
		t.Errorf("TotalPremiumRequests = %d, want 12", shutdown.TotalPremiumRequests)
	}
	if shutdown.TotalAPIDurationMs != 45000 {
		t.Errorf("TotalAPIDurationMs = %d, want 45000", shutdown.TotalAPIDurationMs)
	}
	if shutdown.SessionStartTime != "2026-02-24T10:00:00Z" {
		t.Errorf("SessionStartTime = %q", shutdown.SessionStartTime)
	}
	if shutdown.CodeChanges.LinesAdded != 150 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 150", shutdown.CodeChanges.LinesAdded)
	}
	if shutdown.CodeChanges.LinesRemoved != 30 {
		t.Errorf("CodeChanges.LinesRemoved = %d, want 30", shutdown.CodeChanges.LinesRemoved)
	}
	if shutdown.CodeChanges.FilesModified != 5 {
		t.Errorf("CodeChanges.FilesModified = %d, want 5", shutdown.CodeChanges.FilesModified)
	}
	if len(shutdown.ModelMetrics) != 2 {
		t.Fatalf("expected 2 model metrics, got %d", len(shutdown.ModelMetrics))
	}

	claude := shutdown.ModelMetrics["claude-sonnet-4.5"]
	if claude.Requests.Count != 10 {
		t.Errorf("claude requests count = %d, want 10", claude.Requests.Count)
	}
	if claude.Requests.Cost != 0.35 {
		t.Errorf("claude requests cost = %f, want 0.35", claude.Requests.Cost)
	}
	if claude.Usage.InputTokens != 52000 {
		t.Errorf("claude input tokens = %f, want 52000", claude.Usage.InputTokens)
	}
	if claude.Usage.OutputTokens != 18000 {
		t.Errorf("claude output tokens = %f, want 18000", claude.Usage.OutputTokens)
	}
	if claude.Usage.CacheReadTokens != 30000 {
		t.Errorf("claude cache read tokens = %f, want 30000", claude.Usage.CacheReadTokens)
	}
	if claude.Usage.CacheWriteTokens != 5000 {
		t.Errorf("claude cache write tokens = %f, want 5000", claude.Usage.CacheWriteTokens)
	}

	gpt := shutdown.ModelMetrics["gpt-5-mini"]
	if gpt.Requests.Count != 2 {
		t.Errorf("gpt requests count = %d, want 2", gpt.Requests.Count)
	}
	if gpt.Requests.Cost != 0.05 {
		t.Errorf("gpt requests cost = %f, want 0.05", gpt.Requests.Cost)
	}
}

func TestSessionShutdownDataParsing_Empty(t *testing.T) {
	body := `{
		"shutdownType": "timeout",
		"totalPremiumRequests": 0,
		"totalApiDurationMs": 0,
		"codeChanges": {},
		"modelMetrics": {}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "timeout" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "timeout")
	}
	if shutdown.TotalPremiumRequests != 0 {
		t.Errorf("TotalPremiumRequests = %d, want 0", shutdown.TotalPremiumRequests)
	}
	if shutdown.CodeChanges.LinesAdded != 0 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 0", shutdown.CodeChanges.LinesAdded)
	}
	if len(shutdown.ModelMetrics) != 0 {
		t.Errorf("expected 0 model metrics, got %d", len(shutdown.ModelMetrics))
	}
}

func TestReadSessions_AccumulatesShutdownEvents(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-24T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-24T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-24T12:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-24T12:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSessionWithShutdown := func(id, created, updated string, shutdownJSON string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}
		events := strings.Join([]string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"claude-sonnet-4.5"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[]}}`,
			`{"type":"session.shutdown","timestamp":"` + updated + `","data":` + shutdownJSON + `}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	shutdown1 := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 8,
		"totalApiDurationMs": 30000,
		"sessionStartTime": "2026-02-24T10:00:00Z",
		"codeChanges": {"linesAdded": 100, "linesRemoved": 20, "filesModified": 3},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 6, "cost": 0.25},
				"usage": {"inputTokens": 40000, "outputTokens": 12000, "cacheReadTokens": 20000, "cacheWriteTokens": 3000}
			},
			"gpt-5-mini": {
				"requests": {"count": 2, "cost": 0.04},
				"usage": {"inputTokens": 2000, "outputTokens": 800, "cacheReadTokens": 0, "cacheWriteTokens": 0}
			}
		}
	}`

	shutdown2 := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 4,
		"totalApiDurationMs": 15000,
		"sessionStartTime": "2026-02-24T12:00:00Z",
		"codeChanges": {"linesAdded": 50, "linesRemoved": 10, "filesModified": 2},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 4, "cost": 0.10},
				"usage": {"inputTokens": 12000, "outputTokens": 6000, "cacheReadTokens": 10000, "cacheWriteTokens": 2000}
			}
		}
	}`

	mkSessionWithShutdown("s1", "2026-02-24T10:00:00Z", "2026-02-24T11:00:00Z", shutdown1)
	mkSessionWithShutdown("s2", "2026-02-24T12:00:00Z", "2026-02-24T13:00:00Z", shutdown2)

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify that the session data is still correctly parsed (existing behavior).
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("cli_messages = %+v, want 2", m)
	}

	// Verify total_sessions raw value accounts for both sessions.
	if got := snap.Raw["total_sessions"]; got != "2" {
		t.Fatalf("total_sessions = %q, want 2", got)
	}
}

func TestSessionShutdownDataParsing_NoModelMetrics(t *testing.T) {
	body := `{
		"shutdownType": "crash",
		"totalPremiumRequests": 3,
		"totalApiDurationMs": 5000,
		"codeChanges": {"linesAdded": 10, "linesRemoved": 2, "filesModified": 1}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "crash" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "crash")
	}
	if shutdown.TotalPremiumRequests != 3 {
		t.Errorf("TotalPremiumRequests = %d, want 3", shutdown.TotalPremiumRequests)
	}
	if shutdown.CodeChanges.LinesAdded != 10 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 10", shutdown.CodeChanges.LinesAdded)
	}
	if shutdown.ModelMetrics != nil {
		t.Errorf("expected nil ModelMetrics, got %v", shutdown.ModelMetrics)
	}
}

func TestAssistantUsageDataParsing(t *testing.T) {
	body := `{
		"model": "claude-sonnet-4.5",
		"inputTokens": 5200,
		"outputTokens": 1800,
		"cacheReadTokens": 3000,
		"cacheWriteTokens": 500,
		"cost": 0.042,
		"duration": 2500,
		"quotaSnapshots": {
			"premium_interactions": {
				"entitlementRequests": 300,
				"usedRequests": 158,
				"remainingPercentage": 47.3,
				"resetDate": "2026-03-01T00:00:00Z"
			}
		}
	}`

	var usage assistantUsageData
	if err := unmarshalJSON(body, &usage); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if usage.Model != "claude-sonnet-4.5" {
		t.Errorf("Model = %q, want %q", usage.Model, "claude-sonnet-4.5")
	}
	if usage.InputTokens != 5200 {
		t.Errorf("InputTokens = %f, want 5200", usage.InputTokens)
	}
	if usage.OutputTokens != 1800 {
		t.Errorf("OutputTokens = %f, want 1800", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 3000 {
		t.Errorf("CacheReadTokens = %f, want 3000", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 500 {
		t.Errorf("CacheWriteTokens = %f, want 500", usage.CacheWriteTokens)
	}
	if usage.Cost != 0.042 {
		t.Errorf("Cost = %f, want 0.042", usage.Cost)
	}
	if usage.Duration != 2500 {
		t.Errorf("Duration = %d, want 2500", usage.Duration)
	}
	if len(usage.QuotaSnapshots) != 1 {
		t.Fatalf("expected 1 quota snapshot, got %d", len(usage.QuotaSnapshots))
	}

	premium := usage.QuotaSnapshots["premium_interactions"]
	if premium.EntitlementRequests != 300 {
		t.Errorf("EntitlementRequests = %d, want 300", premium.EntitlementRequests)
	}
	if premium.UsedRequests != 158 {
		t.Errorf("UsedRequests = %d, want 158", premium.UsedRequests)
	}
	if premium.RemainingPercentage != 47.3 {
		t.Errorf("RemainingPercentage = %f, want 47.3", premium.RemainingPercentage)
	}
	if premium.ResetDate != "2026-03-01T00:00:00Z" {
		t.Errorf("ResetDate = %q, want %q", premium.ResetDate, "2026-03-01T00:00:00Z")
	}
}

func TestAssistantUsageDataParsing_NoQuota(t *testing.T) {
	body := `{
		"model": "gpt-5-mini",
		"inputTokens": 1000,
		"outputTokens": 500,
		"cacheReadTokens": 0,
		"cacheWriteTokens": 0,
		"cost": 0.01,
		"duration": 800
	}`

	var usage assistantUsageData
	if err := unmarshalJSON(body, &usage); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if usage.Model != "gpt-5-mini" {
		t.Errorf("Model = %q", usage.Model)
	}
	if usage.InputTokens != 1000 {
		t.Errorf("InputTokens = %f, want 1000", usage.InputTokens)
	}
	if usage.OutputTokens != 500 {
		t.Errorf("OutputTokens = %f, want 500", usage.OutputTokens)
	}
	if len(usage.QuotaSnapshots) != 0 {
		t.Errorf("expected 0 quota snapshots, got %d", len(usage.QuotaSnapshots))
	}
}

func TestReadSessions_AccumulatesUsageEvents(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-25T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-25T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSessionWithUsage := func(id, created, updated string, usageEvents []string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}

		baseEvents := []string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"claude-sonnet-4.5"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[]}}`,
		}
		allEvents := append(baseEvents, usageEvents...)
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(allEvents, "\n")), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	usageEvent1 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:05:00Z","data":{` +
		`"model":"claude-sonnet-4.5","inputTokens":5200,"outputTokens":1800,` +
		`"cacheReadTokens":3000,"cacheWriteTokens":500,"cost":0.042,"duration":2500,` +
		`"quotaSnapshots":{"premium_interactions":{"entitlementRequests":300,"usedRequests":150,"remainingPercentage":50.0,"resetDate":"2026-03-01T00:00:00Z"}}}}`

	usageEvent2 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:10:00Z","data":{` +
		`"model":"claude-sonnet-4.5","inputTokens":3000,"outputTokens":1200,` +
		`"cacheReadTokens":2000,"cacheWriteTokens":300,"cost":0.028,"duration":1800,` +
		`"quotaSnapshots":{"premium_interactions":{"entitlementRequests":300,"usedRequests":152,"remainingPercentage":49.3,"resetDate":"2026-03-01T00:00:00Z"}}}}`

	usageEvent3 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:15:00Z","data":{` +
		`"model":"gpt-5-mini","inputTokens":1000,"outputTokens":500,` +
		`"cacheReadTokens":0,"cacheWriteTokens":0,"cost":0.01,"duration":800}}`

	mkSessionWithUsage("s1", "2026-02-25T10:00:00Z", "2026-02-25T10:20:00Z",
		[]string{usageEvent1, usageEvent2, usageEvent3})

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify that existing session behavior still works.
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 1 {
		t.Fatalf("cli_messages = %+v, want 1", m)
	}
	if got := snap.Raw["total_sessions"]; got != "1" {
		t.Fatalf("total_sessions = %q, want 1", got)
	}

	// The usage data is accumulated internally but not yet emitted as metrics
	// (that is Task 5). This test verifies the parsing does not break existing
	// behavior and that the events are parsed without errors.
	// We verify by checking the session still has correct model and timestamps.
	if got := snap.Raw["last_session_model"]; got != "claude-sonnet-4.5" {
		t.Fatalf("last_session_model = %q, want claude-sonnet-4.5", got)
	}
}

func TestReadSessions_UsageEventsMultipleSessions(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-25T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-25T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-25T14:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-25T14:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, created, updated string, usageEvents []string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}

		baseEvents := []string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"reply","reasoningText":"","toolRequests":[]}}`,
		}
		allEvents := append(baseEvents, usageEvents...)
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(allEvents, "\n")), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	s1Usage := []string{
		`{"type":"assistant.usage","timestamp":"2026-02-25T10:05:00Z","data":{"model":"claude-sonnet-4.5","inputTokens":5200,"outputTokens":1800,"cacheReadTokens":3000,"cacheWriteTokens":500,"cost":0.042,"duration":2500}}`,
		`{"type":"assistant.usage","timestamp":"2026-02-25T10:10:00Z","data":{"model":"claude-sonnet-4.5","inputTokens":3000,"outputTokens":1200,"cacheReadTokens":2000,"cacheWriteTokens":300,"cost":0.028,"duration":1800}}`,
	}

	s2Usage := []string{
		`{"type":"assistant.usage","timestamp":"2026-02-25T14:05:00Z","data":{"model":"gpt-5-mini","inputTokens":1000,"outputTokens":500,"cacheReadTokens":0,"cacheWriteTokens":0,"cost":0.01,"duration":800}}`,
	}

	mkSession("s1", "claude-sonnet-4.5", "2026-02-25T10:00:00Z", "2026-02-25T10:20:00Z", s1Usage)
	mkSession("s2", "gpt-5-mini", "2026-02-25T14:00:00Z", "2026-02-25T14:10:00Z", s2Usage)

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify existing behavior is preserved.
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("cli_messages = %+v, want 2", m)
	}
	if got := snap.Raw["total_sessions"]; got != "2" {
		t.Fatalf("total_sessions = %q, want 2", got)
	}

	// The latest session (s2 at 14:10) should be shown as last.
	if got := snap.Raw["last_session_model"]; got != "gpt-5-mini" {
		t.Fatalf("last_session_model = %q, want gpt-5-mini", got)
	}
}

func unmarshalJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

func boolPtr(v bool) *bool { return &v }
