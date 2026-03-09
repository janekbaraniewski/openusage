package core

import "testing"

func TestExtractLanguageUsage(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"lang_go":         {Used: Float64Ptr(4)},
			"lang_typescript": {Used: Float64Ptr(2)},
			"lang_go_extra":   {Used: nil},
			"requests":        {Used: Float64Ptr(10)},
		},
	}

	got, used := ExtractLanguageUsage(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "go" || got[0].Requests != 4 {
		t.Fatalf("got[0] = %#v, want go/4", got[0])
	}
	if got[1].Name != "typescript" || got[1].Requests != 2 {
		t.Fatalf("got[1] = %#v, want typescript/2", got[1])
	}
	if !used["lang_go"] || !used["lang_typescript"] {
		t.Fatalf("used keys missing expected language metrics: %#v", used)
	}
	if used["requests"] {
		t.Fatalf("unexpected non-language metric in used keys: %#v", used)
	}
}

func TestExtractMCPUsage(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"mcp_calls_total":              {Used: Float64Ptr(5)},
			"mcp_github_total":             {Used: Float64Ptr(3)},
			"mcp_github_list_issues":       {Used: Float64Ptr(2)},
			"mcp_github_create_issue":      {Used: Float64Ptr(1)},
			"mcp_slack_total":              {Used: Float64Ptr(2)},
			"mcp_slack_post_message":       {Used: Float64Ptr(2)},
			"mcp_slack_post_message_today": {Used: Float64Ptr(1)},
		},
	}

	got, used := ExtractMCPUsage(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].RawName != "github" || got[0].Calls != 3 {
		t.Fatalf("got[0] = %#v, want github/3", got[0])
	}
	if len(got[0].Functions) != 2 {
		t.Fatalf("len(got[0].Functions) = %d, want 2", len(got[0].Functions))
	}
	if got[0].Functions[0].RawName != "list_issues" || got[0].Functions[0].Calls != 2 {
		t.Fatalf("got[0].Functions[0] = %#v, want list_issues/2", got[0].Functions[0])
	}
	if got[1].RawName != "slack" || got[1].Calls != 2 {
		t.Fatalf("got[1] = %#v, want slack/2", got[1])
	}
	if !used["mcp_github_total"] || !used["mcp_slack_post_message"] {
		t.Fatalf("used keys missing expected MCP metrics: %#v", used)
	}
	if !used["mcp_calls_total"] {
		t.Fatalf("aggregate MCP key should still be marked used")
	}
}

func TestExtractProjectUsage(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"project_alpha_requests":       {Used: Float64Ptr(5)},
			"project_alpha_requests_today": {Used: Float64Ptr(2)},
			"project_beta_requests":        {Used: Float64Ptr(3)},
		},
		DailySeries: map[string][]TimePoint{
			"usage_project_alpha": {
				{Date: "2026-03-08", Value: 2},
				{Date: "2026-03-09", Value: 3},
			},
		},
	}

	got, used := ExtractProjectUsage(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[0].Requests != 5 || got[0].Requests1d != 2 {
		t.Fatalf("got[0] = %#v, want alpha/5/2", got[0])
	}
	if len(got[0].Series) != 2 {
		t.Fatalf("len(got[0].Series) = %d, want 2", len(got[0].Series))
	}
	if got[1].Name != "beta" || got[1].Requests != 3 {
		t.Fatalf("got[1] = %#v, want beta/3", got[1])
	}
	if !used["project_alpha_requests"] || !used["project_beta_requests"] {
		t.Fatalf("used keys missing project metrics: %#v", used)
	}
}

func TestExtractModelBreakdown(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"model_alpha_input_tokens":   {Used: Float64Ptr(10)},
			"model_alpha_output_tokens":  {Used: Float64Ptr(3)},
			"model_alpha_cost_usd":       {Used: Float64Ptr(1.25)},
			"model_alpha_requests":       {Used: Float64Ptr(4)},
			"model_alpha_requests_today": {Used: Float64Ptr(2)},
			"input_tokens_beta":          {Used: Float64Ptr(7)},
			"output_tokens_beta":         {Used: Float64Ptr(2)},
		},
		DailySeries: map[string][]TimePoint{
			"usage_model_beta": {
				{Date: "2026-03-08", Value: 4},
				{Date: "2026-03-09", Value: 5},
			},
		},
	}

	got, used := ExtractModelBreakdown(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[0].Input != 10 || got[0].Output != 3 || got[0].Cost != 1.25 || got[0].Requests1d != 2 {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[1].Name != "beta" || got[1].Requests != 9 || len(got[1].Series) != 2 {
		t.Fatalf("got[1] = %#v", got[1])
	}
	if !used["model_alpha_cost_usd"] || !used["input_tokens_beta"] {
		t.Fatalf("used keys missing expected model metrics: %#v", used)
	}
}

func TestExtractProviderBreakdown(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"provider_openai_byok_cost": {Used: Float64Ptr(0.8)},
			"provider_openai_requests":  {Used: Float64Ptr(6)},
		},
		Raw: map[string]string{
			"provider_openai_prompt_tokens":     "120",
			"provider_openai_completion_tokens": "20",
		},
	}

	got, used := ExtractProviderBreakdown(snap)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "openai" || got[0].Cost != 0.8 || got[0].Input != 120 || got[0].Output != 20 || got[0].Requests != 6 {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if !used["provider_openai_byok_cost"] || !used["provider_openai_requests"] {
		t.Fatalf("used keys missing expected provider metrics: %#v", used)
	}
}

func TestExtractClientBreakdown(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"source_composer_requests": {Used: Float64Ptr(80)},
			"client_ide_sessions":      {Used: Float64Ptr(3)},
		},
		DailySeries: map[string][]TimePoint{
			"usage_source_composer": {
				{Date: "2026-02-20", Value: 10},
				{Date: "2026-02-21", Value: 70},
			},
		},
	}

	got, used := ExtractClientBreakdown(snap)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Name != "ide" || got[0].Requests != 80 || got[0].Sessions != 3 || len(got[0].Series) != 0 {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if !used["source_composer_requests"] || !used["client_ide_sessions"] {
		t.Fatalf("used keys missing expected client metrics: %#v", used)
	}
}

func TestExtractInterfaceClientBreakdown(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"interface_cli": {Used: Float64Ptr(5)},
			"interface_tab": {Used: Float64Ptr(4)},
		},
		DailySeries: map[string][]TimePoint{
			"usage_source_cli": {
				{Date: "2026-03-08", Value: 2},
				{Date: "2026-03-09", Value: 3},
			},
		},
	}

	got, used := ExtractInterfaceClientBreakdown(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Name != "cli_agents" || got[0].Requests != 5 {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if !used["interface_cli"] || !used["interface_tab"] {
		t.Fatalf("used keys missing expected interface metrics: %#v", used)
	}
}

func TestExtractActualToolUsage(t *testing.T) {
	snap := UsageSnapshot{
		Metrics: map[string]Metric{
			"tool_bash":                         {Used: Float64Ptr(3)},
			"tool_read":                         {Used: Float64Ptr(5)},
			"tool_bash_today":                   {Used: Float64Ptr(1)},
			"tool_calls_total":                  {Used: Float64Ptr(9)},
			"tool_mcp_github_list_issues":       {Used: Float64Ptr(2)},
			"tool_github_mcp_server_get_commit": {Used: Float64Ptr(1)},
		},
	}

	got, used := ExtractActualToolUsage(snap)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].RawName != "read" || got[0].Calls != 5 {
		t.Fatalf("got[0] = %#v, want read/5", got[0])
	}
	if got[1].RawName != "bash" || got[1].Calls != 3 {
		t.Fatalf("got[1] = %#v, want bash/3", got[1])
	}
	if !used["tool_calls_total"] || !used["tool_bash_today"] {
		t.Fatalf("used keys missing expected tool metrics: %#v", used)
	}
	if !used["tool_mcp_github_list_issues"] || !used["tool_github_mcp_server_get_commit"] {
		t.Fatalf("mcp tool metrics should still be marked used: %#v", used)
	}
}
