package copilot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
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
	if cu.MonthlyQuotas == nil || cu.MonthlyQuotas.Chat == nil || *cu.MonthlyQuotas.Chat != 500 {
		t.Error("MonthlyQuotas.Chat should be 500")
	}
	if cu.MonthlyQuotas.Completions == nil || *cu.MonthlyQuotas.Completions != 4000 {
		t.Error("MonthlyQuotas.Completions should be 4000")
	}
	if cu.LimitedUserQuotas == nil || cu.LimitedUserQuotas.Chat == nil || *cu.LimitedUserQuotas.Chat != 470 {
		t.Error("LimitedUserQuotas.Chat should be 470")
	}
	if cu.LimitedUserQuotas.Completions == nil || *cu.LimitedUserQuotas.Completions != 3900 {
		t.Error("LimitedUserQuotas.Completions should be 3900")
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

func TestCopilotInternalUserParsing_NoQuotas(t *testing.T) {
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

	if cu.LimitedUserQuotas != nil {
		t.Error("expected nil LimitedUserQuotas for pro user")
	}
	if cu.MonthlyQuotas != nil {
		t.Error("expected nil MonthlyQuotas for pro user")
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

func TestQuotaStatusMessage(t *testing.T) {
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
			snap := &core.QuotaSnapshot{Raw: tt.raw}
			got := quotaStatusMessage(snap)
			if got != tt.want {
				t.Errorf("quotaStatusMessage() = %q, want %q", got, tt.want)
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

func unmarshalJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}
