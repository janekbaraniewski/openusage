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
